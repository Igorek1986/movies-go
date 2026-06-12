package api

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log"
	_ "embed"
	"movies-api/db/store"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

//go:embed blocked.png
var blockedPNG []byte

func handleBlockedImage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(blockedPNG) //nolint:errcheck
}

// ── Banned origins ────────────────────────────────────────────────────────────

func buildBlockedResponse() map[string]any {
	baseURL, _ := store.GetSetting(context.Background(), "base_url")
	imgURL := strings.TrimRight(baseURL, "/") + "/blocked.png"
	return map[string]any{
		"page":     1,
		"language": "ru",
		"results": []map[string]any{{
			"id":             "blocked",
			"title":          "🚫 Uncensored Lampa is not supported",
			"original_title": "This version is blocked",
			"description":    "This unofficial version is blocked. Please use official Lampa. (lampa.mx)",
			"img":            imgURL,
		}},
		"total_pages":   1,
		"total_results": 1,
		"blocked":       true,
	}
}

var bannedCache struct {
	sync.Mutex
	patterns []string
	loadedAt time.Time
}

func invalidateBannedCache() {
	bannedCache.Lock()
	bannedCache.patterns = nil
	bannedCache.loadedAt = time.Time{}
	bannedCache.Unlock()
}

func loadBannedPatterns() []string {
	bannedCache.Lock()
	defer bannedCache.Unlock()
	if time.Since(bannedCache.loadedAt) < 60*time.Second {
		return bannedCache.patterns
	}
	val, _ := store.GetSetting(context.Background(), "banned_patterns")
	var patterns []string
	if val != "" {
		for _, line := range strings.Split(val, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				patterns = append(patterns, strings.ToLower(line))
			}
		}
	}
	bannedCache.patterns = patterns
	bannedCache.loadedAt = time.Now()
	return patterns
}

func isBanned(header string, patterns []string) bool {
	if header == "" || header == "null" {
		return false
	}
	h := strings.ToLower(header)
	for _, p := range patterns {
		if strings.Contains(h, p) {
			return true
		}
	}
	return false
}

func bannedOriginsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		patterns := loadBannedPatterns()
		isExcluded := r.URL.Path == "/blocked.png" ||
			strings.HasPrefix(r.URL.Path, "/myshows/")
		origin := r.Header.Get("Origin")
		referer := r.Header.Get("Referer")
		if len(patterns) > 0 && !isExcluded {
			if isBanned(origin, patterns) || isBanned(referer, patterns) {
				blocked := origin
				if blocked == "" {
					blocked = referer
				}
				log.Printf("banned origin blocked: %s", blocked)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
				if origin != "" {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					if corsOriginAllowed(origin) {
						w.Header().Set("Access-Control-Allow-Credentials", "true")
					}
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(buildBlockedResponse()) //nolint:errcheck
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ── CORS credentialed-origin allowlist ─────────────────────────────────────────
//
// Lampa-плагины обращаются к контент-API по токену в query/заголовке (без
// cookie) — им достаточно отражённого Origin без credentials. Куки (session_key)
// нужны только same-origin веб-приложению. Поэтому Access-Control-Allow-Credentials
// выдаём ТОЛЬКО для origin'ов из allowlist (base_url + cors_allowed_origins),
// иначе любой сторонний сайт мог бы читать ответы с куками жертвы.

var corsCache struct {
	sync.Mutex
	allowed  map[string]struct{}
	loadedAt time.Time
}

// originOf нормализует строку до "scheme://host[:port]" (без пути/слеша).
func originOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func loadCORSAllowed() map[string]struct{} {
	corsCache.Lock()
	defer corsCache.Unlock()
	if corsCache.allowed != nil && time.Since(corsCache.loadedAt) < 60*time.Second {
		return corsCache.allowed
	}
	allowed := make(map[string]struct{})
	if base, _ := store.GetSetting(context.Background(), "base_url"); base != "" {
		if o := originOf(base); o != "" {
			allowed[o] = struct{}{}
		}
	}
	if extra, _ := store.GetSetting(context.Background(), "cors_allowed_origins"); extra != "" {
		for _, part := range strings.FieldsFunc(extra, func(r rune) bool { return r == ',' || r == '\n' }) {
			if o := originOf(part); o != "" {
				allowed[o] = struct{}{}
			}
		}
	}
	corsCache.allowed = allowed
	corsCache.loadedAt = time.Now()
	return allowed
}

func corsOriginAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	_, ok := loadCORSAllowed()[origin]
	return ok
}

// corsMiddleware разрешает кросс-доменные запросы. Кредённые (с cookie) — только
// для origin'ов из allowlist; для остальных Origin отражается без credentials,
// чтобы публичное контент-API оставалось доступным Lampa-плагинам.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		switch {
		case origin == "":
			w.Header().Set("Access-Control-Allow-Origin", "*")
		case corsOriginAllowed(origin):
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		default:
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Telegram-Init-Data")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// gzipMiddleware сжимает ответы больше 1KB.
var gzipPool = sync.Pool{
	New: func() any {
		gz, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return gz
	},
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	return g.gz.Write(b)
}

func (g *gzipResponseWriter) Flush() {
	g.gz.Flush() //nolint:errcheck
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// WebSocket upgrades must not be wrapped — hijacking requires the original ResponseWriter.
		if r.Header.Get("Upgrade") == "websocket" {
			next.ServeHTTP(w, r)
			return
		}
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		gz := gzipPool.Get().(*gzip.Writer)
		defer gzipPool.Put(gz)

		gz.Reset(w)
		defer gz.Close()

		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length")

		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, gz: gz}, r)
	})
}
