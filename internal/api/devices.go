package api

import (
	"encoding/json"
	"lampa-api/db/store"
	"lampa-api/internal/auth"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// deviceLimits returns max devices per role.
func deviceLimit(role string) int {
	switch role {
	case "premium":
		return 8
	case "super":
		return 0 // unlimited
	default:
		return 3
	}
}

// GET /device/code
// Lampa calls this to get a pairing code, then shows it to the user.
func handleDeviceGetCode(w http.ResponseWriter, r *http.Request) {
	code, err := store.CreateDeviceCode(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "cannot generate code")
		return
	}
	JSON(w, http.StatusOK, map[string]string{"code": code})
}

// GET /device/status?code=XXXXXX
// Lampa polls this until status == "linked", then stores the token.
func handleDeviceStatus(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		Error(w, http.StatusBadRequest, "code required")
		return
	}
	status, token := store.DeviceCodeStatus(r.Context(), code)
	resp := map[string]string{"status": status}
	if token != "" {
		resp["token"] = token
	}
	JSON(w, http.StatusOK, resp)
}

// POST /device/link
// Web UI calls this after user logs in and enters the code from Lampa.
func handleDeviceLink(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Code == "" {
		Error(w, http.StatusBadRequest, "code required")
		return
	}
	if req.Name == "" {
		req.Name = "Lampa"
	}

	maxDev := deviceLimit(u.Role)
	token, err := store.LinkDeviceCode(r.Context(), req.Code, u.ID, req.Name, maxDev)
	if err != nil {
		switch err.Error() {
		case "code not found or expired":
			Error(w, http.StatusNotFound, err.Error())
		case "code already used":
			Error(w, http.StatusConflict, err.Error())
		case "device limit reached":
			Error(w, http.StatusForbidden, err.Error())
		default:
			Error(w, http.StatusInternalServerError, "link error")
		}
		return
	}
	JSON(w, http.StatusOK, map[string]string{"token": token})
}

// GET /api/devices
func handleListDevices(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	devices := store.GetDevicesByUser(r.Context(), u.ID)
	if devices == nil {
		devices = nil
	}
	type deviceView struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		CreatedAt string `json:"created_at"`
	}
	result := make([]deviceView, len(devices))
	for i, d := range devices {
		result[i] = deviceView{
			ID:        d.ID,
			Name:      d.Name,
			CreatedAt: d.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}
	JSON(w, http.StatusOK, result)
}

// DELETE /api/devices/{id}
func handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := store.DeleteDevice(r.Context(), id, u.ID); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// PATCH /api/devices/{id}
func handleRenameDevice(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		Error(w, http.StatusBadRequest, "name required")
		return
	}
	if err := store.RenameDevice(r.Context(), id, u.ID, req.Name); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ─── Token-based Lampa auth helper ───────────────────────────────────────────

// deviceFromRequest reads ?token= or Authorization: Bearer <token> header.
func deviceFromRequest(r *http.Request) *deviceCtx {
	token := r.URL.Query().Get("token")
	if token == "" {
		if hdr := r.Header.Get("Authorization"); len(hdr) > 7 {
			token = hdr[7:] // "Bearer "
		}
	}
	if token == "" {
		return nil
	}
	d := store.GetDeviceByToken(r.Context(), token)
	if d == nil {
		return nil
	}
	return &deviceCtx{ID: d.ID, UserID: d.UserID, Token: d.Token}
}

type deviceCtx struct {
	ID     int64
	UserID int64
	Token  string
}

// requireToken middleware: for Lampa API endpoints that use device tokens.
func requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d := deviceFromRequest(r)
		if d == nil {
			// Fall back to session auth for web UI calls.
			key := auth.SessionFromRequest(r)
			user := auth.GetSessionUser(r.Context(), key)
			if user == nil {
				Error(w, http.StatusUnauthorized, "unauthorized")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
