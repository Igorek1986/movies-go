package api

import (
	"context"
	"encoding/json"
	"movies-api/db/models"
	"movies-api/db/store"
	"movies-api/internal/auth"
	"movies-api/internal/bot"
	"movies-api/internal/proxy"
	"net/http"
	"strings"
)

func countBackupCodes(codes *string) int {
	if codes == nil || *codes == "" {
		return 0
	}
	var hashes []string
	if json.Unmarshal([]byte(*codes), &hashes) != nil {
		return 0
	}
	return len(hashes)
}

// ─── Middleware ───────────────────────────────────────────────────────────────

type ctxKeyUser struct{}

// requireSession is a chi middleware: 401 if no valid session.
func requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Авторизованный ответ персонализирован — кэширующий прокси (nginx/CDN)
		// не должен его сохранять и отдавать другим. Покрывает и requireAdmin.
		w.Header().Set("Cache-Control", "no-store")
		key := auth.SessionFromRequest(r)
		user := auth.GetSessionUser(r.Context(), key)
		if user == nil {
			Error(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyUser{}, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAdmin requires is_admin = true.
//
// Принимает два способа авторизации:
//   - админская сессия (cookie session_key);
//   - админский API-ключ (заголовок X-API-Key или Authorization: Bearer …) —
//     для автоматизации (бэкап/восстановление/миграция без логина и 2FA).
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if adminAPIKeyValid(r) {
			w.Header().Set("Cache-Control", "no-store")
			// Кладём синтетического админа в контекст: часть хендлеров читает
			// userFromCtx и не должна получить nil.
			ctx := context.WithValue(r.Context(), ctxKeyUser{}, &models.User{
				Username: "api-key", Role: "super", IsAdmin: true,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		requireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := userFromCtx(r)
			if u == nil || !u.IsAdmin {
				Error(w, http.StatusForbidden, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})).ServeHTTP(w, r)
	})
}

// optionalSession resolves session but does not block unauthenticated requests.
func optionalSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := auth.SessionFromRequest(r)
		if key != "" {
			if user := auth.GetSessionUser(r.Context(), key); user != nil {
				// Персонализированный ответ — не кэшировать на прокси.
				w.Header().Set("Cache-Control", "no-store")
				ctx := context.WithValue(r.Context(), ctxKeyUser{}, user)
				r = r.WithContext(ctx)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func userFromCtx(r *http.Request) *models.User {
	if v := r.Context().Value(ctxKeyUser{}); v != nil {
		if u, ok := v.(*models.User); ok {
			return u
		}
	}
	return nil
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

func handleMe(w http.ResponseWriter, r *http.Request) {
	key := auth.SessionFromRequest(r)
	if key == "" {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	u := auth.GetSessionUser(r.Context(), key)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var bottomNavKeys []string
	if u.BottomNavKeys != nil && *u.BottomNavKeys != "" {
		bottomNavKeys = strings.Split(*u.BottomNavKeys, ",")
	}
	bottomNavPosition := "bottom"
	if u.BottomNavPosition != nil && *u.BottomNavPosition != "" {
		bottomNavPosition = *u.BottomNavPosition
	}
	cardLayout := "hero"
	if u.CardLayout != nil && *u.CardLayout != "" {
		cardLayout = *u.CardLayout
	}
	browseLayout := "hero"
	if u.BrowseLayout != nil && *u.BrowseLayout != "" {
		browseLayout = *u.BrowseLayout
	}
	// Default is "remote" (the keyboard/pult-friendly view) — see
	// resolveSettingsLayout on the frontend, which must stay in sync.
	settingsLayout := "remote"
	if u.SettingsLayout != nil && *u.SettingsLayout != "" {
		settingsLayout = *u.SettingsLayout
	}
	// Default is "classic" — see THEMES/DEFAULT_THEME on the frontend, which
	// must stay in sync.
	theme := "classic"
	if u.Theme != nil && *u.Theme != "" {
		theme = *u.Theme
	}
	JSON(w, http.StatusOK, map[string]any{
		"id":                  u.ID,
		"username":            u.Username,
		"role":                u.Role,
		"is_admin":            u.IsAdmin,
		"totp_enabled":        u.TotpEnabled,
		"backup_codes_count":  countBackupCodes(u.BackupCodes),
		"premium_until":       u.PremiumUntil,
		"blocked_at":          u.BlockedAt,
		"bottom_nav_keys":     bottomNavKeys, // null/absent = use the frontend default set
		"bottom_nav_position": bottomNavPosition,
		"card_layout":         cardLayout,
		"browse_layout":       browseLayout,
		"settings_layout":     settingsLayout,
		"theme":               theme,
	})
}

// TMDB doesn't come into this — this is just the fixed vocabulary of mobile
// bottom-nav bar buttons (see BOTTOM_NAV_OPTIONS in bottomNavConfig.ts,
// which must stay in sync with this list).
var validBottomNavKeys = map[string]bool{
	"back": true, "home": true, "search": true, "calendar": true,
	"mine": true, "history": true, "sessions": true, "settings": true,
}

var validBottomNavPositions = map[string]bool{"bottom": true, "right": true, "left": true}

// POST /api/me/bottom-nav — save the current user's bottom-nav bar config.
// Body: {"keys": ["back","home","mine","history"], "position": "bottom"}
func handleSaveBottomNav(w http.ResponseWriter, r *http.Request) {
	key := auth.SessionFromRequest(r)
	if key == "" {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	u := auth.GetSessionUser(r.Context(), key)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Keys     []string `json:"keys"`
		Position string   `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}
	if len(body.Keys) < 2 || len(body.Keys) > 5 {
		Error(w, http.StatusBadRequest, "keys must list between 2 and 5 buttons")
		return
	}
	seen := make(map[string]bool, len(body.Keys))
	for _, k := range body.Keys {
		if !validBottomNavKeys[k] || seen[k] {
			Error(w, http.StatusBadRequest, "invalid or duplicate key: "+k)
			return
		}
		seen[k] = true
	}
	if body.Position == "" {
		body.Position = "bottom"
	}
	if !validBottomNavPositions[body.Position] {
		Error(w, http.StatusBadRequest, "invalid position: "+body.Position)
		return
	}
	if err := store.SetUserBottomNavConfig(r.Context(), u.ID, strings.Join(body.Keys, ","), body.Position); err != nil {
		Error(w, http.StatusInternalServerError, "save failed")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Allowed values per field — must stay in sync with CARD_LAYOUTS/
// BROWSE_LAYOUTS/SETTINGS_LAYOUTS/THEMES on the frontend.
var validInterfacePrefValues = map[string]map[string]bool{
	"card_layout":     {"hero": true, "classic": true},
	"browse_layout":   {"hero": true, "classic": true},
	"settings_layout": {"classic": true, "remote": true},
	"theme":           {"classic": true, "glass": true},
}

// POST /api/me/interface — save one or more per-account UI layout
// preferences (card/browse/settings-page layout, site theme — see
// utils/*Layout.ts and utils/theme.ts on the frontend). Body:
// {"card_layout": "hero"} or any subset of these fields; fields not present
// are left unchanged.
func handleSaveInterfacePrefs(w http.ResponseWriter, r *http.Request) {
	key := auth.SessionFromRequest(r)
	if key == "" {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	u := auth.GetSessionUser(r.Context(), key)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}
	for field, value := range body {
		allowed, ok := validInterfacePrefValues[field]
		if !ok || !allowed[value] {
			Error(w, http.StatusBadRequest, "invalid field or value: "+field)
			return
		}
	}
	for field, value := range body {
		if err := store.SetUserInterfacePref(r.Context(), u.ID, field, value); err != nil {
			Error(w, http.StatusInternalServerError, "save failed")
			return
		}
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// GET /api/config — public non-sensitive config for the frontend
func handleAppConfig(w http.ResponseWriter, r *http.Request) {
	imgProxy := ""
	// Через сервер: если для картинок настроен прокси (socks5/vless/... —
	// обход блокировки, см. страницу «Прокси») либо включён дисковый кеш —
	// кешировать нечего, пока картинки не проходят через наш сервер, так что
	// кеш сам по себе включает маршрутизацию независимо от обхода блокировки.
	if store.GetSettingInt(r.Context(), "images_cache_enabled") == 1 ||
		proxy.Default.HasProxy(r.Context(), proxy.RouteImages) {
		imgProxy = "/imgproxy"
	}
	pluginURL, _ := store.GetSetting(r.Context(), "plugin_url")
	if pluginURL == "" {
		if baseURL, _ := store.GetSetting(r.Context(), "base_url"); baseURL != "" {
			pluginURL = strings.TrimRight(baseURL, "/") + "/np.js"
		}
	}
	botName, _ := store.GetSetting(r.Context(), "telegram_bot_name")
	JSON(w, http.StatusOK, map[string]any{
		"image_proxy_url":   imgProxy,
		"bot_name":          botName,
		"plugin_url":        pluginURL,
		"watched_threshold": store.WatchedThreshold(r.Context()),
	})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		Error(w, http.StatusBadRequest, "Введите логин и пароль")
		return
	}

	u := store.GetUserByUsername(r.Context(), req.Username)
	if u == nil || !auth.CheckPassword(u.PasswordHash, req.Password) {
		Error(w, http.StatusUnauthorized, "Неверный логин или пароль")
		return
	}
	if u.BlockedAt != nil {
		msg := "Аккаунт заблокирован"
		if u.BlockReason != nil {
			msg = *u.BlockReason
		}
		Error(w, http.StatusForbidden, msg)
		return
	}

	if u.TotpEnabled {
		ttl := store.GetSettingInt(r.Context(), "pending_2fa_ttl_sec")
		if ttl <= 0 {
			ttl = 600
		}
		pendingToken, err := store.CreateTotpPendingToken(r.Context(), u.ID, ttl)
		if err != nil {
			Error(w, http.StatusInternalServerError, "session error")
			return
		}
		JSON(w, http.StatusOK, map[string]any{
			"requires_2fa":  true,
			"pending_token": pendingToken,
		})
		return
	}

	sess, err := auth.CreateSession(r.Context(), u.ID,
		realIP(r), r.Header.Get("User-Agent"))
	if err != nil {
		Error(w, http.StatusInternalServerError, "session error")
		return
	}

	auth.SetSessionCookie(w, sess.Key, sess.ExpiresAt)
	if tg := store.GetTelegramLinkByUserID(r.Context(), u.ID); tg != nil {
		go bot.SendNewSessionNotification(tg.TelegramID, realIP(r), r.Header.Get("User-Agent"))
	}
	JSON(w, http.StatusOK, map[string]any{
		"id":       u.ID,
		"username": u.Username,
		"role":     u.Role,
		"is_admin": u.IsAdmin,
	})
}

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 {
		Error(w, http.StatusBadRequest, "Логин слишком короткий (минимум 3 символа)")
		return
	}
	if len(req.Password) < 6 {
		Error(w, http.StatusBadRequest, "Пароль слишком короткий (минимум 6 символов)")
		return
	}

	if store.GetUserByUsername(r.Context(), req.Username) != nil {
		Error(w, http.StatusConflict, "Такой логин уже занят")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		Error(w, http.StatusInternalServerError, "hash error")
		return
	}

	u, err := store.CreateUser(r.Context(), req.Username, hash, "simple")
	if err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}

	sess, err := auth.CreateSession(r.Context(), u.ID,
		realIP(r), r.Header.Get("User-Agent"))
	if err != nil {
		Error(w, http.StatusInternalServerError, "session error")
		return
	}

	auth.SetSessionCookie(w, sess.Key, sess.ExpiresAt)
	JSON(w, http.StatusCreated, map[string]any{
		"id":       u.ID,
		"username": u.Username,
		"role":     u.Role,
		"is_admin": u.IsAdmin,
	})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	key := auth.SessionFromRequest(r)
	if key != "" {
		auth.DeleteSession(r.Context(), key)
	}
	auth.ClearSessionCookie(w)
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /api/change-password
func handleChangePassword(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, req.CurrentPassword) {
		Error(w, http.StatusUnauthorized, "Неверный текущий пароль")
		return
	}
	if len(req.NewPassword) < 6 {
		Error(w, http.StatusBadRequest, "Новый пароль слишком короткий (минимум 6 символов)")
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		Error(w, http.StatusInternalServerError, "hash error")
		return
	}
	if err := store.UpdatePassword(r.Context(), u.ID, hash); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DELETE /api/account
func handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u.IsAdmin {
		Error(w, http.StatusForbidden, "Нельзя удалить аккаунт администратора")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, req.Password) {
		Error(w, http.StatusUnauthorized, "Неверный пароль")
		return
	}
	if err := store.DeleteUser(r.Context(), u.ID); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	key := auth.SessionFromRequest(r)
	if key != "" {
		auth.DeleteSession(r.Context(), key)
	}
	auth.ClearSessionCookie(w)
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
