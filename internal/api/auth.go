package api

import (
	"context"
	"encoding/json"
	"movies-api/config"
	"movies-api/db/models"
	"movies-api/db/store"
	"movies-api/internal/auth"
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
func requireAdmin(next http.Handler) http.Handler {
	return requireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := userFromCtx(r)
		if u == nil || !u.IsAdmin {
			Error(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// optionalSession resolves session but does not block unauthenticated requests.
func optionalSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := auth.SessionFromRequest(r)
		if key != "" {
			if user := auth.GetSessionUser(r.Context(), key); user != nil {
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
	JSON(w, http.StatusOK, map[string]any{
		"id":                 u.ID,
		"username":           u.Username,
		"role":               u.Role,
		"is_admin":           u.IsAdmin,
		"totp_enabled":       u.TotpEnabled,
		"backup_codes_count": countBackupCodes(u.BackupCodes),
		"premium_until":      u.PremiumUntil,
		"blocked_at":         u.BlockedAt,
	})
}

// GET /api/config — public non-sensitive config for the frontend
func handleAppConfig(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	imgProxy := ""
	if proxy.Default.HasProxy(r.Context(), proxy.RouteImages) {
		imgProxy = "/imgproxy"
	}
	pluginURL, _ := store.GetSetting(r.Context(), "plugin_url")
	if pluginURL == "" {
		if baseURL, _ := store.GetSetting(r.Context(), "base_url"); baseURL != "" {
			pluginURL = strings.TrimRight(baseURL, "/") + "/np.js"
		}
	}
	JSON(w, http.StatusOK, map[string]any{
		"image_proxy_url": imgProxy,
		"bot_name":        cfg.TelegramBotName,
		"plugin_url":      pluginURL,
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
		Error(w, http.StatusBadRequest, "username and password required")
		return
	}

	u := store.GetUserByUsername(r.Context(), req.Username)
	if u == nil || !auth.CheckPassword(u.PasswordHash, req.Password) {
		Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if u.BlockedAt != nil {
		msg := "account blocked"
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
		r.RemoteAddr, r.Header.Get("User-Agent"))
	if err != nil {
		Error(w, http.StatusInternalServerError, "session error")
		return
	}

	auth.SetSessionCookie(w, sess.Key, sess.ExpiresAt)
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
		Error(w, http.StatusBadRequest, "username too short")
		return
	}
	if len(req.Password) < 6 {
		Error(w, http.StatusBadRequest, "password too short")
		return
	}

	if store.GetUserByUsername(r.Context(), req.Username) != nil {
		Error(w, http.StatusConflict, "username taken")
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
		r.RemoteAddr, r.Header.Get("User-Agent"))
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
		Error(w, http.StatusUnauthorized, "wrong password")
		return
	}
	if len(req.NewPassword) < 6 {
		Error(w, http.StatusBadRequest, "password too short")
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
		Error(w, http.StatusForbidden, "cannot delete admin account")
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
		Error(w, http.StatusUnauthorized, "wrong password")
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
