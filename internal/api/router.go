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

	// Device activation (public — Lampa polls these)
	r.Get("/device/code", handleDeviceGetCode)
	r.Get("/device/status", handleDeviceStatus)

	// Timecodes — authenticated via ?token= (Lampa device token)
	r.Route("/timecode", func(r chi.Router) {
		r.Post("", handleSaveTimecode)
		r.Delete("", handleDeleteTimecode)
		r.Post("/batch", handleBatchTimecodes)
		r.Get("/export", handleExportTimecodes)
		r.Post("/import/lampac", handleImportLampac)
		r.Get("/history", handleHistory)
		r.Get("/profiles", handleListProfiles)
		r.Post("/profiles", handleCreateProfile)
		r.Patch("/profiles/{profile_id}", handleUpdateProfile)
		r.Delete("/profiles/{profile_id}", handleDeleteProfile)
		r.Get("/favorite", handleGetFavorite)
		r.Put("/favorite", handlePutFavorite)
	})

	// Plugin settings (device token auth)
	r.Get("/api/plugin-settings", handleGetPluginSettings)
	r.Patch("/api/plugin-settings", handlePatchPluginSettings)

	// Search
	r.Get("/search", handleSearch)

	// Content categories (device token optional for hide-watched)
	for route := range categoryRoutes {
		r.Get("/"+route, handleCategory)
	}
	r.Get("/continues", handleCategory)
	r.Get("/continues_movie", handleCategory)
	r.Get("/continues_tv", handleCategory)
	r.Get("/continues_anime", handleCategory)
	r.Get("/np_popular", handleCategory)

	// API — authenticated
	r.Route("/api", func(r chi.Router) {
		// Auth (public)
		r.Post("/login", handleLogin)
		r.Post("/register", handleRegister)
		r.Post("/logout", handleLogout)
		r.Get("/me", handleMe)

		// Device management (requires web session)
		r.With(requireSession).Post("/device/link", handleDeviceLink)
		r.With(requireSession).Get("/devices", handleListDevices)
		r.With(requireSession).Delete("/devices/{id}", handleDeleteDevice)
		r.With(requireSession).Patch("/devices/{id}", handleRenameDevice)
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
