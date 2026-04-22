package api

import (
	"context"
	"encoding/json"
	"lampa-api/db/models"
	"lampa-api/db/store"
	"lampa-api/internal/auth"
	"net/http"
	"strings"
)

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
		"id":            u.ID,
		"username":      u.Username,
		"role":          u.Role,
		"is_admin":      u.IsAdmin,
		"totp_enabled":  u.TotpEnabled,
		"premium_until": u.PremiumUntil,
		"blocked_at":    u.BlockedAt,
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
