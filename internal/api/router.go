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

func NewRouter(mode string) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(corsMiddleware)
	r.Use(gzipMiddleware)
	r.Use(serveLampaPlugins)

	// ── Общие маршруты (parser + all) ───────────────────────────────────────
	r.Get("/health", handleHealth)
	r.Get("/imgproxy/*", handleImgProxy)

	// Lampa content API
	r.Get("/api/search", handleSearch)
	r.Get("/search", handleSearch)

	for route := range categoryRoutes {
		r.Get("/"+route, handleCategory)
		r.Get("/lampac_"+route, handleCategory) // lampac_ prefix alias
	}
	r.Get("/movies_id_{year:[0-9]+}", handleCategory)
	r.Get("/lampac_movies_id_{year:[0-9]+}", handleCategory)
	r.Get("/continues", handleCategory)
	r.Get("/continues_movie", handleCategory)
	r.Get("/continues_tv", handleCategory)
	r.Get("/continues_anime", handleCategory)
	r.Get("/np_popular", handleCategory)

	r.Get("/api/refresh-card-episodes", handleRefreshCardEpisodes)

	r.Route("/api", func(r chi.Router) {
		r.Get("/config", handleAppConfig)
		r.Get("/categories", handleAPICategories)
		r.Get("/media-card/{card_id}", handleMediaCard)
		r.Get("/media-card/{card_id}/credits", handleMediaCardCredits)
		r.Get("/media-card/{card_id}/similar", handleMediaCardSimilar)
		r.Get("/media-card/{card_id}/recommendations", handleMediaCardSimilar)

		if mode == "all" {
			r.Get("/episodes", handleEpisodes)
			r.Get("/profile-ids", handleAPIProfileIDs)
			r.Post("/login", handleLogin)
			r.Post("/register", handleRegister)
			r.Post("/logout", handleLogout)
			r.Get("/me", handleMe)
			r.With(requireSession).Post("/change-password", handleChangePassword)
			r.With(requireSession).Delete("/account", handleDeleteAccount)
			r.With(requireSession).Post("/device/link", handleDeviceLink)
			r.With(requireSession).Get("/devices", handleListDevices)
			r.With(requireSession).Post("/devices", handleCreateDevice)
			r.With(requireSession).Delete("/devices/{id}", handleDeleteDevice)
			r.With(requireSession).Patch("/devices/{id}", handleRenameDevice)
			r.With(requireSession).Post("/devices/{id}/regenerate-token", handleRegenerateToken)
			r.With(requireSession).Delete("/devices/{id}/timecodes", handleClearDeviceTimecodes)
			r.With(requireSession).Get("/devices/{id}/profiles", handleWebListProfiles)
			r.With(requireSession).Post("/devices/{id}/profiles", handleWebCreateProfile)
			r.With(requireSession).Delete("/devices/{id}/profiles/{profile_id}", handleWebDeleteProfile)
			r.With(requireSession).Delete("/devices/{id}/profiles/{profile_id}/timecodes", handleWebClearProfileTimecodes)
			r.With(requireSession).Patch("/devices/{id}/profiles/{profile_id}", handleWebUpdateProfile)
			r.With(requireSession).Get("/web/history", handleWebHistory)
			r.With(requireSession).Get("/web/card-timecodes", handleWebCardTimecodes)
			r.With(requireSession).Post("/web/set-timecode", handleWebSetTimecode)
			r.With(requireSession).Delete("/web/card-timecodes", handleWebDeleteCardTimecodes)
			r.With(requireSession).Get("/web/card-progress", handleWebCardProgress)
			r.With(requireSession).Post("/web/mark-special", handleWebMarkSpecial)
			r.With(requireSession).Post("/web/unmark-special", handleWebUnmarkSpecial)
			r.With(requireSession).Get("/history", handleWebHistory)
			r.With(requireSession).Get("/card-timecodes", handleWebCardTimecodes)
			r.With(requireSession).Delete("/card-timecodes", handleWebDeleteCardTimecodes)
			r.With(requireSession).Post("/set-timecode", handleWebSetTimecode)
			r.With(requireSession).Post("/mark-watched", handleWebMarkSpecial)
			r.With(requireSession).Post("/unmark-special", handleWebUnmarkSpecial)
			r.With(requireSession).Post("/profile-name", handleProfileName)
			r.With(requireSession).Get("/card-views", handleCardViews)
			r.With(requireSession).Delete("/episode-timecode", handleDeleteEpisodeTimecode)
			r.With(requireSession).Patch("/lampa-profile", handleLampaProfilePatch)
			r.With(requireSession).Delete("/lampa-profile", handleLampaProfileDelete)
			r.With(requireSession).Post("/lampa-profile/clear", handleLampaProfileClear)
			r.With(requireSession).Get("/lampa-profile/quota", handleLampaProfileQuota)
			r.With(requireSession).Post("/lampa-profile/create", handleLampaProfileCreate)
			r.With(requireSession).Get("/actor/{person_id}", handleActorAPI)
			r.With(requireAdmin).Get("/admin/stats", handleAdminStats)
			r.With(requireAdmin).Get("/admin/users", handleAdminListUsers)
			r.With(requireAdmin).Patch("/admin/users/{id}/role", handleAdminSetRole)
			r.With(requireAdmin).Delete("/admin/users/{id}", handleAdminDeleteUser)
		}
	})

	if mode != "all" {
		return r
	}

	// ── Только в режиме all ──────────────────────────────────────────────────
	staticFS, _ := fs.Sub(static.FS, "files")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	r.Get("/api/plugin-settings", handleGetPluginSettings)
	r.Patch("/api/plugin-settings", handlePatchPluginSettings)

	r.Post("/device/code", handleDeviceGetCode)
	r.Get("/device/code", handleDeviceGetCode)
	r.Get("/device/status", handleDeviceStatus)
	r.With(requireSession).Post("/device/link", handleDeviceLink)

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
	r.With(optionalSession).Get("/consent", handleConsentPage)
	r.With(optionalSession).Get("/privacy", handlePrivacyPage)
	r.With(optionalSession).Get("/tg-miniapp", handleTgMiniappPage)
	r.With(optionalSession).Post("/tg-miniapp", handleTgMiniappSubmit)
	r.With(requireSession).Get("/setup-2fa", handleSetup2FAPage)
	r.With(requireSession).Post("/setup-2fa", handleSetup2FAConfirm)
	r.With(requireSession).Post("/disable-2fa", handleDisable2FA)
	r.Get("/verify-2fa", handleVerify2FAPage)
	r.Post("/verify-2fa", handleVerify2FASubmit)
	r.With(optionalSession).Get("/stats", handleStatsLoginPage)
	r.With(optionalSession).Post("/stats", handleStatsLoginForm)
	r.With(optionalSession).Get("/stats/dashboard", handleStatsDashboard)
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
	r.With(requireSession).Get("/profiles", handleProfilesPage)
	r.With(requireSession).Post("/profiles/create", handleProfilesCreate)
	r.With(requireSession).Post("/profiles/{id}/regenerate", handleProfilesRegenerate)
	r.With(requireSession).Post("/profiles/{id}/clear-timecodes", handleProfilesClearTimecodes)
	r.With(requireSession).Post("/profiles/{id}/delete", handleProfilesDelete)
	r.With(requireSession).Post("/profiles/{id}/rename", handleProfilesRename)
	r.With(requireSession).Post("/profile/reset-password", handleFormChangePassword)
	r.With(requireSession).Post("/profile/delete", handleFormDeleteAccount)
	r.With(requireSession).Get("/sessions", handleSessionsPage)
	r.With(requireSession).Post("/sessions/revoke-all", handleSessionRevokeAll)
	r.With(requireSession).Post("/sessions/{id}/revoke", handleSessionRevoke)

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
