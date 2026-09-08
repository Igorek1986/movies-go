package api

// Shared URL validation/reachability check for user-supplied plugin/extension
// script URLs — used by both device_plugins (Lampa plugins) and web_extensions
// (web SPA extensions, see extensions.go/extensions_rpc.go). Kept in one place
// per the project's DRY rule instead of duplicating the same GET-and-sniff
// logic for each feature.

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var scriptCheckClient = &http.Client{Timeout: 6 * time.Second}

// resolveScriptURL turns a URL as stored (absolute http(s), or a same-origin
// "/path" for first-party scripts served from ./plugins/ — kept relative so
// it survives the app being mirrored under different domains, see the DPI
// front-proxy setup) into an absolute URL usable for a server-side GET.
func resolveScriptURL(r *http.Request, rawURL string) string {
	if strings.HasPrefix(rawURL, "/") {
		scheme := "https"
		if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
			scheme = "http"
		}
		return scheme + "://" + r.Host + rawURL
	}
	return rawURL
}

// validScriptURLShape reports whether rawURL is an acceptable stored form: an
// absolute http(s) URL, or a same-origin "/path" for first-party scripts.
func validScriptURLShape(rawURL string) bool {
	return strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") || strings.HasPrefix(rawURL, "/")
}

// checkScriptURL does a best-effort GET to catch typos/dead links at save
// time. Returns a normalized status ("ok"/"unreachable"/"invalid"), the HTTP
// status code when one was received, and a user-facing reason (empty on ok).
func checkScriptURL(absoluteURL string) (status string, code *int, reason string) {
	// Lampac-style template URLs (e.g. "http://{localhost}/my_plugins/actors.js")
	// have their host substituted by Lampac itself at request time — there's
	// nothing reachable from here to check, so skip the round-trip.
	if strings.Contains(absoluteURL, "{") {
		return "ok", nil, ""
	}
	req, err := http.NewRequest(http.MethodGet, absoluteURL, nil)
	if err != nil {
		return "invalid", nil, "некорректный URL"
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; movies-go-plugin-check/1.0)")
	resp, err := scriptCheckClient.Do(req)
	if err != nil {
		return "unreachable", nil, "не удалось загрузить URL: " + err.Error()
	}
	defer resp.Body.Close()
	sc := resp.StatusCode
	if sc != http.StatusOK {
		return "unreachable", &sc, fmt.Sprintf("сервер вернул статус %d", sc)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	head := strings.TrimSpace(strings.ToLower(string(body)))
	if strings.HasPrefix(head, "<!doctype") || strings.HasPrefix(head, "<html") {
		return "invalid", &sc, "по ссылке HTML-страница, а не JS-файл"
	}
	return "ok", &sc, ""
}
