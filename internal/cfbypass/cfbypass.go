// Package cfbypass fetches pages from hosts behind Cloudflare's JS challenge.
//
// FlareSolverr (a headless-Chromium wrapper) solves the challenge once per
// host and hands back a cf_clearance cookie + the browser's User-Agent.
// Plain net/http with that same cookie still gets 403 — Cloudflare also
// checks the TLS/JA3 fingerprint, which stdlib doesn't reproduce. So
// subsequent requests go through cffetch (a small curl_cffi sidecar that
// impersonates Chrome's TLS fingerprint) reusing the cached cookie — only
// when that fast path reports the clearance no longer works do we fall
// back to solving again via FlareSolverr.
//
// Disabled by default: both FLARESOLVERR_URL and CFFETCH_URL must be set.
// See dev/kinozal.md / dev/rutracker.md for the operational background.
package cfbypass

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	flareURL   = strings.TrimSpace(os.Getenv("FLARESOLVERR_URL"))
	cffetchURL = strings.TrimSpace(os.Getenv("CFFETCH_URL"))

	// 340s: buffer above maxTimeoutMs below, so the Go client doesn't give up
	// on the HTTP round-trip before FlareSolverr's own deadline would.
	httpClient = &http.Client{Timeout: 340 * time.Second}
)

const (
	sessionPrefix = "movies-go-"
	// 300000ms (5min): kinozal usually solves in 10-45s regardless, but
	// rutracker.org's heavier challenge has taken up to ~270s in testing
	// (2026-09-16) — this is a ceiling, not a fixed wait.
	maxTimeoutMs      = 300000
	clearanceTTL      = 30 * time.Minute
	fastPathBlockTTL  = 30 * time.Minute
	mitigationsToDrop = 3
	mitigationWindow  = 60 * time.Second
	cffetchConcurrent = 8
)

// Enabled reports whether both sidecars are configured. Callers should fall
// back to a plain HTTP client when this is false.
func Enabled() bool { return flareURL != "" && cffetchURL != "" }

// ─── per-host state ─────────────────────────────────────────────────────────

type clearance struct {
	cookies   string
	userAgent string
	at        time.Time
}

type browserSession struct {
	name  string
	mu    sync.Mutex
	alive bool
}

type mitigationRun struct {
	since time.Time
	count int
}

var (
	stateMu     sync.Mutex
	sessions    = map[string]*browserSession{}
	clearances  = map[string]*clearance{}
	blockedTill = map[string]time.Time{}
	mitigations = map[string]*mitigationRun{}

	cffetchGate = make(chan struct{}, cffetchConcurrent)
)

func sessionFor(host string) *browserSession {
	stateMu.Lock()
	defer stateMu.Unlock()
	s, ok := sessions[host]
	if !ok {
		s = &browserSession{name: sessionName(host)}
		sessions[host] = s
	}
	return s
}

func sessionName(host string) string {
	var b strings.Builder
	b.WriteString(sessionPrefix)
	for _, c := range strings.ToLower(host) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func getClearance(host string) *clearance {
	stateMu.Lock()
	defer stateMu.Unlock()
	c, ok := clearances[host]
	if !ok {
		return nil
	}
	if time.Since(c.at) > clearanceTTL {
		return nil
	}
	if t, blocked := blockedTill[host]; blocked && time.Now().Before(t) {
		return nil
	}
	return c
}

func rememberClearance(host, cookies, userAgent string) {
	if cookies == "" {
		return
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	clearances[host] = &clearance{cookies: cookies, userAgent: userAgent, at: time.Now()}
	delete(blockedTill, host)
}

// forgetClearance drops the cached cookie and blocks the fast path for a
// while so we don't immediately re-learn a cookie that cffetch can't use
// (TLS-bound clearance, IP changed, etc.) — every call would just fail again.
func forgetClearance(host string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	delete(clearances, host)
	blockedTill[host] = time.Now().Add(fastPathBlockTTL)
	delete(mitigations, host)
}

// shouldDropClearance tolerates isolated failures (one bad response) before
// giving up on an otherwise-good cookie — three challenge responses within a
// minute before we conclude the clearance is actually dead.
func shouldDropClearance(host string) bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	run, ok := mitigations[host]
	now := time.Now()
	if !ok || now.Sub(run.since) > mitigationWindow {
		run = &mitigationRun{since: now}
		mitigations[host] = run
	}
	run.count++
	if run.count < mitigationsToDrop {
		return false
	}
	delete(mitigations, host)
	return true
}

// ─── public API ─────────────────────────────────────────────────────────────

// Get fetches url, solving Cloudflare's challenge via FlareSolverr the first
// time for a host and reusing the resulting clearance via cffetch after
// that. Matches the parser package's fetchFunc signature.
func Get(rawURL string) ([]byte, error) {
	if !Enabled() {
		return nil, fmt.Errorf("cfbypass: not configured")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	host := u.Host

	if body, ok := tryFastPath(host, rawURL); ok {
		return body, nil
	}
	return solveViaBrowser(host, rawURL)
}

func tryFastPath(host, rawURL string) ([]byte, bool) {
	c := getClearance(host)
	if c == nil {
		return nil, false
	}

	cffetchGate <- struct{}{}
	defer func() { <-cffetchGate }()

	status, body, mitigated, err := cffetchFetch(rawURL, c.cookies, c.userAgent)
	if err != nil {
		log.Printf("cfbypass: cffetch unreachable, falling back to browser: %v", err)
		return nil, false
	}
	if mitigated || isChallengeBody(body) {
		if shouldDropClearance(host) {
			log.Printf("cfbypass: %s: clearance no longer works, re-solving", host)
			forgetClearance(host)
		}
		return nil, false
	}
	if status != 200 || body == "" {
		return nil, false
	}
	return []byte(body), true
}

func solveViaBrowser(host, rawURL string) ([]byte, error) {
	sess := sessionFor(host)
	sess.mu.Lock()
	defer sess.mu.Unlock()

	// Another goroutine may have solved this host's challenge while we were
	// waiting for the session lock (common on startup: several categories
	// hit the same host at once) — check for a fresh clearance before
	// spending another browser round-trip.
	if body, ok := tryFastPath(host, rawURL); ok {
		return body, nil
	}

	if !sess.alive {
		if err := createSession(sess); err != nil {
			return nil, err
		}
	}

	body, cookies, ua, err := flareRequestGet(sess, rawURL)
	if err != nil {
		log.Printf("cfbypass: %s: session recycle after error: %v", host, err)
		destroySession(sess)
		if err2 := createSession(sess); err2 != nil {
			return nil, fmt.Errorf("recreate session: %w", err2)
		}
		body, cookies, ua, err = flareRequestGet(sess, rawURL)
		if err != nil {
			return nil, err
		}
	}

	rememberClearance(host, cookies, ua)
	return []byte(body), nil
}

// ─── FlareSolverr ───────────────────────────────────────────────────────────

type flareCookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type flareSolution struct {
	Status    int           `json:"status"`
	Response  string        `json:"response"`
	Cookies   []flareCookie `json:"cookies"`
	UserAgent string        `json:"userAgent"`
}

type flareResponse struct {
	Status   string         `json:"status"`
	Message  string         `json:"message"`
	Solution *flareSolution `json:"solution"`
}

func flareCall(payload map[string]any) (*flareResponse, error) {
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, flareURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out flareResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode flaresolverr response: %w", err)
	}
	return &out, nil
}

func createSession(sess *browserSession) error {
	resp, err := flareCall(map[string]any{"cmd": "sessions.create", "session": sess.name})
	if err != nil {
		sess.alive = false
		return fmt.Errorf("flaresolverr unreachable: %w", err)
	}
	ok := strings.EqualFold(resp.Status, "ok") || strings.Contains(strings.ToLower(resp.Message), "already exists")
	sess.alive = ok
	if !ok {
		return fmt.Errorf("flaresolverr sessions.create failed: %s", resp.Message)
	}
	log.Printf("cfbypass: session %s created", sess.name)
	return nil
}

func destroySession(sess *browserSession) {
	_, _ = flareCall(map[string]any{"cmd": "sessions.destroy", "session": sess.name})
	sess.alive = false
}

func flareRequestGet(sess *browserSession, rawURL string) (body, cookies, userAgent string, err error) {
	resp, err := flareCall(map[string]any{
		"cmd":        "request.get",
		"session":    sess.name,
		"url":        rawURL,
		"maxTimeout": maxTimeoutMs,
	})
	if err != nil {
		sess.alive = false
		return "", "", "", fmt.Errorf("flaresolverr unreachable: %w", err)
	}
	if !strings.EqualFold(resp.Status, "ok") {
		sess.alive = false
		return "", "", "", fmt.Errorf("flaresolverr request.get failed: %s", resp.Message)
	}
	sol := resp.Solution
	if sol == nil || sol.Status != 200 || sol.Response == "" {
		status := 0
		if sol != nil {
			status = sol.Status
		}
		return "", "", "", fmt.Errorf("flaresolverr: unexpected page status %d", status)
	}
	if isChallengeBody(sol.Response) {
		return "", "", "", fmt.Errorf("flaresolverr: challenge still present in solved page")
	}

	var jar strings.Builder
	for _, c := range sol.Cookies {
		if c.Name == "" {
			continue
		}
		if jar.Len() > 0 {
			jar.WriteString("; ")
		}
		jar.WriteString(c.Name)
		jar.WriteByte('=')
		jar.WriteString(c.Value)
	}

	return sol.Response, jar.String(), sol.UserAgent, nil
}

// ─── cffetch ────────────────────────────────────────────────────────────────

type cffetchResponse struct {
	Status      int    `json:"status"`
	CfMitigated bool   `json:"cfMitigated"`
	Body        string `json:"body"`
	Error       string `json:"error"`
}

func cffetchFetch(rawURL, cookies, userAgent string) (status int, body string, mitigated bool, err error) {
	payload := map[string]any{
		"url":       rawURL,
		"cookies":   cookies,
		"userAgent": userAgent,
	}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, cffetchURL, bytes.NewReader(raw))
	if err != nil {
		return 0, "", false, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 35 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", false, err
	}
	defer resp.Body.Close()
	raw, err = io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", false, err
	}
	var out cffetchResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, "", false, fmt.Errorf("decode cffetch response: %w", err)
	}
	if out.Status == 0 && out.Error != "" {
		return 0, "", false, fmt.Errorf("cffetch: %s", out.Error)
	}
	return out.Status, out.Body, out.CfMitigated, nil
}

// ─── challenge detection ────────────────────────────────────────────────────

// isChallengeBody recognizes Cloudflare's interstitial page — used when a
// response doesn't otherwise carry a clear "still challenged" signal (e.g.
// FlareSolverr's own solution.status can read 200 for the interstitial).
func isChallengeBody(body string) bool {
	if body == "" || len(body) > 200_000 {
		return false
	}
	lower := strings.ToLower(body)
	for _, marker := range []string{
		"cf-browser-verification",
		"cf_chl_opt",
		"just a moment",
		"один момент",
		"orchestrate/chl_page",
		"challenge-platform/h/",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
