package api

import (
	"io/fs"
	"movies-api/internal/web"
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
	r.Use(bannedOriginsMiddleware)
	r.Use(gzipMiddleware)
	r.Use(servePlugins)

	// ── Общие маршруты (parser + all) ───────────────────────────────────────
	r.Get("/health", handleHealth)
	r.Get("/blocked.png", handleBlockedImage)
	r.Get("/imgproxy/*", handleImgProxy)

	// Content API
	r.Get("/api/search", handleSearch)
	r.Get("/search", handleSearch)

	cached := withCategoryCache(handleCategory)
	for route := range categoryRoutes {
		if strings.HasPrefix(route, "genre_") {
			r.Get("/"+route, handleCategory)
		} else {
			r.Get("/"+route, cached)
		}
	}
	r.Get("/movies_id_{year:[0-9]+}", cached)
	r.Get("/actor_{person_id:[0-9]+}", cached)
	r.Get("/director_{person_id:[0-9]+}", cached)
	r.Get("/continues", cached)
	r.Get("/continues_movie", cached)
	r.Get("/continues_tv", cached)
	r.Get("/continues_anime", cached)
	r.Get("/np_popular", cached)
	r.Get("/np_popular_daily", handlePopularDaily)

	r.Get("/api/refresh-card-episodes", handleRefreshCardEpisodes)
	r.Get("/api/check-ongoing", handleCheckOngoing)

	r.Route("/api", func(r chi.Router) {
		r.Get("/config", handleAppConfig)
		r.Get("/categories", handleAPICategories)
		r.Get("/media-card/{card_id}", handleMediaCard)
		r.Get("/media-card/{card_id}/credits", handleMediaCardCredits)
		r.Get("/media-card/{card_id}/similar", handleMediaCardSimilar)
		r.Get("/media-card/{card_id}/recommendations", handleMediaCardSimilar)
		r.Post("/view", handleView)

		if mode == "all" {
			r.With(optionalSession).Get("/episodes", handleEpisodes)
			r.With(optionalSession).Get("/profile-ids", handleAPIProfileIDs)
			r.Post("/login", rateLimitMiddleware(loginRL, "rate_login_max", "rate_login_window_sec", handleLogin))
			r.Post("/register", rateLimitMiddleware(registerRL, "rate_register_max", "rate_register_window_sec", handleRegister))
			r.Post("/logout", handleLogout)
			r.Get("/me", handleMe)
			r.With(requireSession).Post("/change-password", handleChangePassword)
			r.With(requireSession).Delete("/account", handleDeleteAccount)
			r.With(requireSession).Get("/sessions", handleAPISessions)
			r.With(requireSession).Delete("/sessions/{id}", handleAPISessionRevoke)
			r.With(requireSession).Delete("/sessions", handleAPISessionRevokeAll)
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
			r.With(requireSession).Post("/devices/{id}/migrate-default-timecodes", handleWebMigrateDefaultTimecodes)
			r.With(requireSession).Delete("/devices/{id}/default-timecodes", handleWebDeleteDefaultTimecodes)
			r.With(requireSession).Patch("/devices/{id}/profiles/{profile_id}", handleWebUpdateProfile)
			r.With(requireSession).Get("/web/history", handleWebHistory)
			r.With(requireSession).Get("/web/card-timecodes", handleWebCardTimecodes)
			r.With(requireSession).Post("/web/set-timecode", handleWebSetTimecode)
			r.With(requireSession).Delete("/web/card-timecodes", handleWebDeleteCardTimecodes)
			r.With(requireSession).Get("/web/card-progress", handleWebCardProgress)
			r.With(requireSession).Post("/web/mark-special", handleWebMarkSpecial)
			r.With(requireSession).Post("/web/unmark-special", handleWebUnmarkSpecial)
			r.With(requireSession).Get("/history", handleWebHistoryAll)
			r.With(requireSession).Get("/card-timecodes", handleWebCardTimecodes)
			r.With(requireSession).Delete("/card-timecodes", handleWebDeleteCardTimecodes)
			r.With(requireSession).Post("/set-timecode", handleWebSetTimecode)
			r.With(requireSession).Post("/mark-watched", handleWebMarkSpecial)
			r.With(requireSession).Post("/unmark-special", handleWebUnmarkSpecial)
			r.With(requireSession).Post("/profile-name", handleProfileName)
			r.With(requireSession).Get("/card-views", handleCardViews)
			r.With(requireSession).Delete("/episode-timecode", handleDeleteEpisodeTimecode)
			r.With(requireSession).Patch("/profile", handleProfilePatch)
			r.With(requireSession).Delete("/profile", handleProfileDelete)
			r.With(requireSession).Post("/profile/clear", handleProfileClear)
			r.With(requireSession).Get("/profile/quota", handleProfileQuota)
			r.With(requireSession).Post("/profile/create", handleProfileCreate)
			r.With(requireSession).Get("/actor/{person_id}", handleActorAPI)
			r.With(requireAdmin).Get("/admin/stats", handleAdminStats)
			r.With(requireAdmin).Get("/admin/popular", handleAPIAdminPopular)
			r.With(requireAdmin).Get("/admin/popular-source", handleAPIAdminPopularSource)
			r.With(requireAdmin).Get("/admin/users", handleAdminListUsers)
			r.With(requireAdmin).Patch("/admin/users/{id}/role", handleAdminSetRole)
			r.With(requireAdmin).Delete("/admin/users/{id}", handleAdminDeleteUser)
			r.With(requireAdmin).Patch("/admin/users/{id}/toggle-admin", handleAPIAdminToggleAdmin)
			r.With(requireAdmin).Post("/admin/users/{id}/block", handleAPIAdminBlock)
			r.With(requireAdmin).Post("/admin/users/{id}/unblock", handleAPIAdminUnblock)
			r.With(requireAdmin).Post("/admin/users/{id}/reset-sync", handleAPIAdminResetSync)
			r.With(requireAdmin).Post("/admin/users/{id}/cleanup-limits", handleAPIAdminCleanupLimits)
			r.With(requireAdmin).Post("/admin/run-expiry-check", handleAPIAdminRunExpiryCheck)
			r.With(requireAdmin).Post("/admin/extend-all-premium", handleAPIAdminExtendAllPremium)
			r.With(requireAdmin).Post("/admin/episodes-refresh", handleAPIAdminEpisodesRefresh)
			r.With(requireAdmin).Post("/admin/refresh-cards", handleAPIAdminRefreshCards)
			r.With(requireAdmin).Post("/admin/refresh-cards/stop", handleAPIAdminRefreshCardsStop)
			r.With(requireAdmin).Get("/admin/refresh-cards/status", handleAPIAdminRefreshCardsStatus)
			r.With(requireAdmin).Post("/admin/fix-runtime", handleAPIAdminFixRuntime)
			r.With(requireAdmin).Post("/admin/fix-runtime/stop", handleAPIAdminFixRuntimeStop)
			r.With(requireAdmin).Get("/admin/fix-runtime/status", handleAPIAdminFixRuntimeStatus)
			r.With(requireAdmin).Post("/admin/backfill-cast", handleAPIAdminBackfillCast)
			r.With(requireAdmin).Post("/admin/backfill-cast/stop", handleAPIAdminBackfillCastStop)
			r.With(requireAdmin).Get("/admin/backfill-cast/status", handleAPIAdminBackfillCastStatus)
			r.With(requireAdmin).Get("/admin/actors", handleAPIAdminActorList)
			r.With(requireAdmin).Get("/admin/directors", handleAPIAdminDirectorList)
			r.With(requireAdmin).Post("/admin/restart", handleAPIAdminRestart)
			r.With(requireAdmin).Get("/admin/bot/status", handleAPIAdminBotStatus)
			r.With(requireAdmin).Post("/admin/bot/restart", handleAPIAdminBotRestart)
			r.With(requireAdmin).Get("/admin/banned-patterns", handleAPIAdminBannedGet)
			r.With(requireAdmin).Post("/admin/banned-patterns", handleAPIAdminBannedAdd)
			r.With(requireAdmin).Delete("/admin/banned-patterns", handleAPIAdminBannedDelete)
			r.With(requireAdmin).Get("/admin/child-keywords", handleAPIAdminChildKeywordsGet)
			r.With(requireAdmin).Post("/admin/child-keywords", handleAPIAdminChildKeywordsAdd)
			r.With(requireAdmin).Delete("/admin/child-keywords", handleAPIAdminChildKeywordsDelete)
			r.With(requireAdmin).Post("/admin/child-keywords/reset", handleAPIAdminChildKeywordsReset)
			r.With(requireAdmin).Get("/admin/child-keywords/search", handleAPIAdminChildKeywordsSearch)
			r.With(requireAdmin).Get("/admin/child-keywords/resolve", handleAPIAdminChildKeywordsResolve)
			r.With(requireAdmin).Get("/admin/child-text-keywords", handleAPIAdminChildTextKwGet)
			r.With(requireAdmin).Post("/admin/child-text-keywords", handleAPIAdminChildTextKwAdd)
			r.With(requireAdmin).Delete("/admin/child-text-keywords", handleAPIAdminChildTextKwDelete)
			r.With(requireAdmin).Get("/admin/child-text-keyword-ages", handleAPIAdminChildTextAgesGet)
			r.With(requireAdmin).Post("/admin/child-text-keyword-ages", handleAPIAdminChildTextAgesSave)

			r.With(requireAdmin).Post("/admin/refresh-card/{card_id}", handleAPIAdminRefreshCard)
			r.With(requireAdmin).Get("/admin/logs", handleAPIAdminLogsStream)
			r.With(requireAdmin).Get("/admin/logs/day", handleAPIAdminLogsDay)
			r.With(requireSession).Post("/telegram/generate-link-code", handleGenerateLinkCode)
			r.With(requireSession).Get("/telegram/status", handleTelegramStatus)
			r.With(requireSession).Delete("/telegram/unlink", handleTelegramUnlink)
			r.With(requireSession).Get("/notification-settings", handleGetNotificationSettings)
			r.With(requireSession).Patch("/notification-settings", handlePatchNotificationSettings)
			r.With(requireSession).Post("/disable-2fa", handleAPIDisable2FA)
			r.With(requireSession).Get("/setup-2fa", handleAPISetup2FA)
			r.With(requireSession).Post("/setup-2fa", handleAPISetup2FAConfirm)
			r.Post("/verify-2fa", rateLimitMiddleware(twoFARL, "rate_2fa_max", "rate_2fa_window_sec", handleAPIVerify2FA))
		}
	})

	// ── Settings + Parser management (admin, both modes) ────────────────────────
	r.Route("/api/admin/settings", func(r chi.Router) {
		r.Use(requireAnyAdmin(mode))
		r.Get("/", handleAPIAdminSettingsGet)
		r.Post("/", handleAPIAdminSettingsSave)
	})
	r.Route("/api/admin/parsers", func(r chi.Router) {
		r.Use(requireAnyAdmin(mode))
		r.Get("/", handleAPIAdminParsersGet)
		r.Post("/settings", handleAPIAdminParsersSettings)
		r.Post("/run", handleAPIAdminParsersRun)
		r.Post("/stop", handleAPIAdminParsersStop)
		r.Post("/{name}/run", handleAPIAdminParserTrackerRun)
		r.Post("/{name}/reset", handleAPIAdminParserTrackerReset)
	})

	// ── TMDB missing cards (admin, both modes) ──────────────────────────────────
	r.Route("/api/admin/tmdb-missing", func(r chi.Router) {
		r.Use(requireAnyAdmin(mode))
		r.Get("/", handleAPIAdminTMDBMissing)
		r.Delete("/{cardID}", handleAPIAdminTMDBMissingDelete)
	})

	// ── Cards (admin, both modes) ─────────────────────────────────────────────────
	r.With(requireAnyAdmin(mode)).Get("/api/admin/cards-today", handleAPIAdminCardsToday)
	r.With(requireAnyAdmin(mode)).Get("/api/admin/all-cards", handleAPIAdminAllCards)
	r.With(requireAnyAdmin(mode)).Get("/api/admin/all-cards/meta", handleAPIAdminAllCardsMeta)
	r.With(requireAnyAdmin(mode)).Delete("/api/admin/cards", handleAPIAdminDeleteCards)
	r.With(requireAnyAdmin(mode)).Patch("/api/admin/cards/{card_id}/dates", handleAPIAdminPatchCardDates)

	// ── System stats (admin, both modes) ─────────────────────────────────────────
	r.With(requireAnyAdmin(mode)).Get("/api/admin/system-stats", handleAPIAdminSystemStats)

	// ── Proxy config (admin, both modes) ────────────────────────────────────────
	r.Route("/api/admin/proxies", func(r chi.Router) {
		r.Use(requireAnyAdmin(mode))
		r.Get("/", handleAPIProxiesList)
		r.Post("/", handleAPIProxiesCreate)
		r.Put("/{id}", handleAPIProxiesUpdate)
		r.Delete("/{id}", handleAPIProxiesDelete)
		r.Post("/{id}/test", handleAPIProxiesTest)
		r.Post("/routing", handleAPIProxyRoutingSave)
	})

	if mode != "all" {
		r.Route("/api/admin", func(r chi.Router) {
			r.Use(requireParserAdmin)
			r.Get("/banned-patterns", handleAPIAdminBannedGet)
			r.Post("/banned-patterns", handleAPIAdminBannedAdd)
			r.Delete("/banned-patterns", handleAPIAdminBannedDelete)
			r.Get("/child-keywords", handleAPIAdminChildKeywordsGet)
			r.Post("/child-keywords", handleAPIAdminChildKeywordsAdd)
			r.Delete("/child-keywords", handleAPIAdminChildKeywordsDelete)
			r.Post("/child-keywords/reset", handleAPIAdminChildKeywordsReset)
			r.Get("/child-keywords/search", handleAPIAdminChildKeywordsSearch)
			r.Get("/child-keywords/resolve", handleAPIAdminChildKeywordsResolve)
			r.Get("/child-text-keywords", handleAPIAdminChildTextKwGet)
			r.Post("/child-text-keywords", handleAPIAdminChildTextKwAdd)
			r.Delete("/child-text-keywords", handleAPIAdminChildTextKwDelete)
			r.Get("/child-text-keyword-ages", handleAPIAdminChildTextAgesGet)
			r.Post("/child-text-keyword-ages", handleAPIAdminChildTextAgesSave)
			r.Get("/logs", handleAPIAdminLogsStream)
			r.Get("/logs/day", handleAPIAdminLogsDay)
		})
		r.Get("/admin", handleParserModeAdmin)
		r.Get("/admin/", handleParserModeAdmin)
		r.Post("/admin/login", handleParserModeLogin)
		r.Post("/admin/mode", handleParserModeSwitch)
		return r
	}

	// ── Только в режиме all ──────────────────────────────────────────────────
	r.Get("/api/plugin-settings", handleGetPluginSettings)
	r.Patch("/api/plugin-settings", handlePatchPluginSettings)

	registerTgAppRoutes(r)

	r.Get("/device/ping", handleDevicePing)
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
		r.Get("/ws", handleTimecodeWS)
	})

	r.Get("/api/plugin-settings/ws", handlePluginSettingsWS)

	r.Get("/logout", handleLogoutPage)
	r.Post("/api/forgot-password", rateLimitMiddleware(forgotRL, "rate_forgot_max", "rate_forgot_window_sec", handleAPIForgotPassword))
	r.Post("/api/reset-password", handleAPIResetPassword)
	r.Get("/api/public/page", handlePublicPage)
	r.With(requireSession).Post("/myshows/sync", handleMyshowsSync)

	r.Get("/myshows/watching", handleMyshowsWatchingGet)
	r.Post("/myshows/watching", handleMyshowsWatchingPost)
	r.Get("/myshows/profile_shows", handleMyshowsProfileShowsGet)
	r.Post("/myshows/profile_shows", handleMyshowsProfileShowsPost)
	r.Get("/myshows/watchlist", handleMyshowsStatusGet("watchlist"))
	r.Post("/myshows/watchlist", handleMyshowsStatusPost("watchlist"))
	r.Get("/myshows/watched", handleMyshowsStatusGet("watched"))
	r.Post("/myshows/watched", handleMyshowsStatusPost("watched"))
	r.Get("/myshows/cancelled", handleMyshowsStatusGet("cancelled"))
	r.Post("/myshows/cancelled", handleMyshowsStatusPost("cancelled"))
	r.Post("/myshows/serial_status", handleMyshowsSerialStatus)
	r.Post("/myshows/movie_status", handleMyshowsMovieStatus)
	r.Post("/myshows/set_status", handleMyshowsSetStatus)
	r.Get("/myshows/status", handleMyshowsGetStatus)
	r.Post("/myshows/timetable", handleMyshowsTimetablePost)
	r.With(requireSession).Get("/api/export", handleExport)
	r.With(requireSession).Post("/api/import", handleImport)

	r.Post("/bot/webhook", handleTelegramWebhook)

	if mode == "all" {
		r.NotFound(serveSPA)
	} else {
		r.NotFound(serveParserStub)
	}

	return r
}

// servePlugins отдаёт файлы из ./plugins/ без рестарта бинарника.
func servePlugins(next http.Handler) http.Handler {
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

		fullPath := filepath.Join("plugins", rel)
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

// serveParserStub отдаёт заглушку в режиме парсера (веб-интерфейс недоступен).
func serveParserStub(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<!DOCTYPE html><html lang="ru"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Сервис работает</title><style>*{box-sizing:border-box;margin:0;padding:0}body{background:#0f1117;color:#e8eaf6;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;min-height:100vh;display:flex;align-items:center;justify-content:center;padding:16px}.card{background:#1a1d27;border:1px solid #2d3148;border-radius:10px;padding:32px;width:100%;max-width:400px;display:flex;flex-direction:column;gap:16px;text-align:center}h1{font-size:1.5rem;font-weight:600}p{color:#8a8fa8;font-size:14px;line-height:1.5}</style></head><body><div class="card"><h1>Сервис работает</h1><p>Веб-интерфейс недоступен в режиме парсера.<br>Для доступа к панели управления переключитесь в режим <strong>all</strong>.</p></div></body></html>`)) //nolint:errcheck
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
