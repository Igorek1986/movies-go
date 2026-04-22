package api

import (
	"io/fs"
	"lampa-api/internal/web"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(corsMiddleware)
	r.Use(gzipMiddleware)
	r.Use(serveLampaPlugins)

	// Health
	r.Get("/health", handleHealth)

	// API
	r.Route("/api", func(r chi.Router) {
		r.Post("/login", handleLogin)
		r.Post("/register", handleRegister)
		r.Post("/logout", handleLogout)
		r.Get("/me", handleMe)
	})

	// SPA fallback
	r.NotFound(serveSPA)

	return r
}

// serveLampaPlugins отдаёт файлы из ./lampa-plugins/ без рестарта бинарника.
func serveLampaPlugins(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}

		rel := strings.TrimPrefix(r.URL.Path, "/")
		if rel == "" {
			next.ServeHTTP(w, r)
			return
		}

		fullPath := filepath.Join("lampa-plugins", rel)
		info, err := os.Stat(fullPath)
		if err != nil || info.IsDir() {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, fullPath)
	})
}

// serveSPA отдаёт React-приложение; неизвестные пути → index.html.
func serveSPA(w http.ResponseWriter, r *http.Request) {
	sub, err := fs.Sub(web.FS, "dist")
	if err != nil {
		http.Error(w, "frontend not built", http.StatusInternalServerError)
		return
	}

	fsrv := http.FileServer(http.FS(sub))
	path := strings.TrimPrefix(r.URL.Path, "/")

	if _, err := fs.Stat(sub, path); err == nil {
		fsrv.ServeHTTP(w, r)
		return
	}

	// SPA fallback → index.html
	r.URL.Path = "/"
	fsrv.ServeHTTP(w, r)
}
