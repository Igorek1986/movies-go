package api

import (
	"io/fs"
	"lampa-api/internal/static"
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

	// Static assets embedded in binary
	staticFS, _ := fs.Sub(static.FS, "files")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// ── Web pages (server-side HTML) ────────────────────────────────────────
	// Index / catalog
	r.With(optionalSession).Get("/", handleIndexPage)
	r.With(optionalSession).Get("/catalog/{category}", handleCatalogCategoryPage)

	r.Get("/login", handleLoginPage)
	r.Post("/login", handleLoginForm)
	r.Get("/logout", handleLogoutPage)
	r.Get("/register", handleRegisterPage)
	r.Post("/register", handleRegisterForm)
	r.Get("/register-success", handleRegisterSuccess)
	r.Get("/forgot-password", handleForgotPasswordPage)
	r.Post("/forgot-password", handleForgotPasswordForm)
	r.Get("/reset-password", handleResetPasswordPage)
	r.Post("/reset-password", handleResetPasswordForm)
	r.With(requireSession).Get("/history", handleHistoryPage)
	r.With(optionalSession).Get("/card/{card_id}", handleCardDetailPage)
	r.With(requireSession).Get("/actor/{person_id}", handleActorPage)

	// Legal / static pages
	r.With(optionalSession).Get("/consent", handleConsentPage)
	r.With(optionalSession).Get("/privacy", handlePrivacyPage)

	// Telegram miniapp (device linking)
	r.With(optionalSession).Get("/tg-miniapp", handleTgMiniappPage)
	r.With(optionalSession).Post("/tg-miniapp", handleTgMiniappSubmit)

	// 2FA setup (requires session) and login verify (no session needed)
	r.With(requireSession).Get("/setup-2fa", handleSetup2FAPage)
	r.With(requireSession).Post("/setup-2fa", handleSetup2FAConfirm)
	r.With(requireSession).Post("/disable-2fa", handleDisable2FA)
	r.Get("/verify-2fa", handleVerify2FAPage)
	r.Post("/verify-2fa", handleVerify2FASubmit)

	// Stats dashboard (password-cookie auth)
	r.With(optionalSession).Get("/stats", handleStatsLoginPage)
	r.With(optionalSession).Post("/stats", handleStatsLoginForm)
	r.With(optionalSession).Get("/stats/dashboard", handleStatsDashboard)

	// Admin dashboard (admin session auth)
	r.With(optionalSession).Get("/admin", handleAdminPage)
	r.With(optionalSession).Get("/admin/settings", handleAdminSettings)
	r.With(optionalSession).Post("/admin/settings", handleAdminSettingsSave)
	r.With(optionalSession).Post("/admin/user/{id}/role", handleAdminUserSetRole)
	r.With(optionalSession).Post("/admin/user/{id}/toggle-admin", handleAdminUserToggleAdmin)
	r.With(optionalSession).Post("/admin/user/{id}/block", handleAdminUserBlock)
	r.With(optionalSession).Post("/admin/user/{id}/unblock", handleAdminUserUnblock)
	r.With(optionalSession).Post("/admin/user/{id}/delete", handleAdminUserDelete)
	r.With(optionalSession).Post("/admin/user/{id}/reset-sync", handleAdminUserResetSync)
	r.With(optionalSession).Post("/admin/user/{id}/cleanup-limits", handleAdminUserCleanupLimits)
	r.With(optionalSession).Post("/admin/run-expiry-check", handleAdminRunExpiryCheck)
	r.With(optionalSession).Post("/admin/extend-all-premium", handleAdminExtendAllPremium)
	r.With(optionalSession).Post("/admin/episodes-refresh", handleAdminEpisodesRefresh)
	r.With(optionalSession).Post("/admin/episodes-find-ids", handleAdminEpisodesFindIDs)
	r.With(optionalSession).Post("/admin/parser-reset-date", handleAdminParserResetDate)

	// Profiles / devices management
	r.With(requireSession).Get("/profiles", handleProfilesPage)
	r.With(requireSession).Post("/profiles/create", handleProfilesCreate)
	r.With(requireSession).Post("/profiles/{id}/regenerate", handleProfilesRegenerate)
	r.With(requireSession).Post("/profiles/{id}/clear-timecodes", handleProfilesClearTimecodes)
	r.With(requireSession).Post("/profiles/{id}/delete", handleProfilesDelete)
	r.With(requireSession).Post("/profiles/{id}/rename", handleProfilesRename)

	// Account actions (form-based)
	r.With(requireSession).Post("/profile/reset-password", handleFormChangePassword)
	r.With(requireSession).Post("/profile/delete", handleFormDeleteAccount)

	// Sessions management
	r.With(requireSession).Get("/sessions", handleSessionsPage)
	r.With(requireSession).Post("/sessions/revoke-all", handleSessionRevokeAll)
	r.With(requireSession).Post("/sessions/{id}/revoke", handleSessionRevoke)

	// Health
	r.Get("/health", handleHealth)

	// TMDB image proxy (server-side SOCKS5 tunnel)
	r.Get("/imgproxy/*", handleImgProxy)

	// Device activation (public — Lampa polls these)
	r.Post("/device/code", handleDeviceGetCode) // plugin sends POST
	r.Get("/device/code", handleDeviceGetCode)  // web UI fallback
	r.Get("/device/status", handleDeviceStatus)
	r.With(requireSession).Post("/device/link", handleDeviceLink) // profiles.js alias

	// Episode refresh — Lampa plugin calls this (device-token auth)
	r.Get("/api/refresh-card-episodes", handleRefreshCardEpisodes)

	// Timecodes — authenticated via ?token= (Lampa device token)
	r.Route("/timecode", func(r chi.Router) {
		r.Post("/", handleSaveTimecode)
		r.Delete("/", handleDeleteTimecode)
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

	// Search — lm.js calls /api/search?q=
	r.Get("/api/search", handleSearch)
	r.Get("/search", handleSearch) // backward compat

	// Content categories (device token optional for hide-watched)
	for route := range categoryRoutes {
		r.Get("/"+route, handleCategory)
	}
	r.Get("/movies_id_{year:[0-9]+}", handleCategory)
	// Continues and popular
	r.Get("/continues", handleCategory)
	r.Get("/continues_movie", handleCategory)
	r.Get("/continues_tv", handleCategory)
	r.Get("/continues_anime", handleCategory)
	r.Get("/np_popular", handleCategory)

	// API — authenticated
	r.Route("/api", func(r chi.Router) {
		// Public config
		r.Get("/config", handleAppConfig)
		r.Get("/categories", handleAPICategories)
		r.With(requireSession).Get("/profile-ids", handleAPIProfileIDs)

		// Auth (public)
		r.Post("/login", handleLogin)
		r.Post("/register", handleRegister)
		r.Post("/logout", handleLogout)
		r.Get("/me", handleMe)

		// Account management
		r.With(requireSession).Post("/change-password", handleChangePassword)
		r.With(requireSession).Delete("/account", handleDeleteAccount)

		// Device management (requires web session)
		r.With(requireSession).Post("/device/link", handleDeviceLink)
		r.With(requireSession).Get("/devices", handleListDevices)
		r.With(requireSession).Post("/devices", handleCreateDevice)
		r.With(requireSession).Delete("/devices/{id}", handleDeleteDevice)
		r.With(requireSession).Patch("/devices/{id}", handleRenameDevice)
		r.With(requireSession).Post("/devices/{id}/regenerate-token", handleRegenerateToken)
		r.With(requireSession).Delete("/devices/{id}/timecodes", handleClearDeviceTimecodes)
		// Lampa profiles per device (session-auth)
		r.With(requireSession).Get("/devices/{id}/profiles", handleWebListProfiles)
		r.With(requireSession).Post("/devices/{id}/profiles", handleWebCreateProfile)
		r.With(requireSession).Delete("/devices/{id}/profiles/{profile_id}", handleWebDeleteProfile)
		r.With(requireSession).Delete("/devices/{id}/profiles/{profile_id}/timecodes", handleWebClearProfileTimecodes)
		r.With(requireSession).Patch("/devices/{id}/profiles/{profile_id}", handleWebUpdateProfile)

		// Episodes (requires session for watched state, device for refresh)
		r.With(requireSession).Get("/episodes", handleEpisodes)

		// Web history + card timecodes (requires session)
		r.With(requireSession).Get("/web/history", handleWebHistory)
		r.With(requireSession).Get("/web/card-timecodes", handleWebCardTimecodes)
		r.With(requireSession).Post("/web/set-timecode", handleWebSetTimecode)
		r.With(requireSession).Delete("/web/card-timecodes", handleWebDeleteCardTimecodes)
		r.With(requireSession).Get("/web/card-progress", handleWebCardProgress)
		r.With(requireSession).Post("/web/mark-special", handleWebMarkSpecial)
		r.With(requireSession).Post("/web/unmark-special", handleWebUnmarkSpecial)

		// Aliases for static JS files (FastAPI-compatible URLs)
		r.With(requireSession).Get("/history", handleWebHistory)
		r.With(requireSession).Get("/card-timecodes", handleWebCardTimecodes)
		r.With(requireSession).Delete("/card-timecodes", handleWebDeleteCardTimecodes)
		r.With(requireSession).Post("/set-timecode", handleWebSetTimecode)
		r.With(requireSession).Post("/mark-watched", handleWebMarkSpecial)
		r.With(requireSession).Post("/unmark-special", handleWebUnmarkSpecial)
		r.With(requireSession).Post("/profile-name", handleProfileName)
		r.With(requireSession).Get("/card-views", handleCardViews)
		r.With(requireSession).Delete("/episode-timecode", handleDeleteEpisodeTimecode)
		// Lampa profile aliases (used by profiles.js)
		r.With(requireSession).Patch("/lampa-profile", handleLampaProfilePatch)
		r.With(requireSession).Delete("/lampa-profile", handleLampaProfileDelete)
		r.With(requireSession).Post("/lampa-profile/clear", handleLampaProfileClear)
		r.With(requireSession).Get("/lampa-profile/quota", handleLampaProfileQuota)
		r.With(requireSession).Post("/lampa-profile/create", handleLampaProfileCreate)

		// Media card detail (public — no auth needed)
		r.Get("/media-card/{card_id}", handleMediaCard)
		r.Get("/media-card/{card_id}/credits", handleMediaCardCredits)
		r.Get("/media-card/{card_id}/similar", handleMediaCardSimilar)
		r.Get("/media-card/{card_id}/recommendations", handleMediaCardSimilar)

		// Actor/person
		r.With(requireSession).Get("/actor/{person_id}", handleActorAPI)

		// Admin (requires admin session)
		r.With(requireAdmin).Get("/admin/stats", handleAdminStats)
		r.With(requireAdmin).Get("/admin/users", handleAdminListUsers)
		r.With(requireAdmin).Patch("/admin/users/{id}/role", handleAdminSetRole)
		r.With(requireAdmin).Delete("/admin/users/{id}", handleAdminDeleteUser)
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
