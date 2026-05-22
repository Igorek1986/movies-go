package api

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log"
	_ "embed"
	"movies-api/config"
	"movies-api/db/store"
	"net/http"
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
	imgURL := strings.TrimRight(config.Get().BaseURL, "/") + "/blocked.png"
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
	log.Printf("banned_patterns reloaded from DB: %v", patterns)
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
				log.Printf("banned_block: patterns=%v path=%s origin=%q referer=%q → BLOCKED", patterns, r.URL.Path, origin, referer)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(buildBlockedResponse()) //nolint:errcheck
				_ = blocked
				return
			}
		} else if origin != "" || referer != "" {
			log.Printf("banned_check: patterns=%v path=%s origin=%q referer=%q → pass", patterns, r.URL.Path, origin, referer)
		}
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware разрешает запросы с любого Origin (для Lampa плагинов).
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Telegram-Init-Data")
		w.Header().Set("Vary", "Origin")

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
