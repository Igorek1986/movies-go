package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"movies-api/db/store"
	"movies-api/internal/auth"
	"net/http"

	"github.com/pquerna/otp/totp"
	qrcode "github.com/skip2/go-qrcode"
)

func buildQR(otpauthURL string) string {
	png, err := qrcode.Encode(otpauthURL, qrcode.Medium, 200)
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

func siteNameFromConfig() string {
	if n, ok := store.GetSetting(context.Background(), "site_name"); ok && n != "" {
		return n
	}
	return store.SettingDefaults["site_name"]
}

// ─── JSON API (React) ─────────────────────────────────────────────────────────

// GET /api/setup-2fa
func handleAPISetup2FA(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var secret string
	fresh := store.GetUserByID(r.Context(), u.ID)
	if fresh != nil && fresh.TotpSecret != nil && !fresh.TotpEnabled {
		secret = *fresh.TotpSecret
	} else {
		key, err := totp.Generate(totp.GenerateOpts{
			Issuer:      siteNameFromConfig(),
			AccountName: u.Username,
		})
		if err != nil {
			Error(w, http.StatusInternalServerError, "error generating 2FA key")
			return
		}
		secret = key.Secret()
		store.SetTotpSecret(r.Context(), u.ID, secret)
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      siteNameFromConfig(),
		AccountName: u.Username,
		Secret:      []byte(secret),
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, "error generating QR")
		return
	}

	JSON(w, http.StatusOK, map[string]string{
		"qr_data_url": string(buildQR(key.URL())),
		"secret":      secret,
	})
}

// POST /api/setup-2fa  body: {"code":"123456"}
func handleAPISetup2FAConfirm(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}

	fresh := store.GetUserByID(r.Context(), u.ID)
	if fresh == nil || fresh.TotpSecret == nil {
		Error(w, http.StatusBadRequest, "no pending 2fa setup")
		return
	}

	if !totp.Validate(req.Code, *fresh.TotpSecret) {
		Error(w, http.StatusUnauthorized, "invalid code")
		return
	}

	plainCodes, hashesJSON := store.GenerateBackupCodes()
	store.EnableTotp(r.Context(), u.ID, hashesJSON)

	JSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"backup_codes": plainCodes,
	})
}

// POST /api/verify-2fa  body: {"token":"...","code":"123456"}
func handleAPIVerify2FA(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Token == "" || req.Code == "" {
		Error(w, http.StatusBadRequest, "token and code required")
		return
	}

	ctx := r.Context()
	userID := store.ConsumeTotpPendingToken(ctx, req.Token)
	if userID == 0 {
		Error(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	u := store.GetUserByID(ctx, userID)
	if u == nil {
		Error(w, http.StatusUnauthorized, "user not found")
		return
	}

	valid := false
	if u.TotpSecret != nil {
		valid = totp.Validate(req.Code, *u.TotpSecret)
	}
	if !valid {
		valid = store.UseBackupCode(ctx, userID, req.Code)
	}

	if !valid {
		ttl := store.GetSettingInt(ctx, "pending_2fa_ttl_sec")
		if ttl <= 0 {
			ttl = 600
		}
		newToken, err := store.CreateTotpPendingToken(ctx, userID, ttl)
		if err != nil {
			Error(w, http.StatusInternalServerError, "server error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"error":     "invalid code",
			"new_token": newToken,
		})
		return
	}

	sess, err := auth.CreateSession(ctx, userID, r.RemoteAddr, r.Header.Get("User-Agent"))
	if err != nil {
		Error(w, http.StatusInternalServerError, "session error")
		return
	}
	auth.SetSessionCookie(w, sess.Key, sess.ExpiresAt)
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// POST /api/disable-2fa — JSON endpoint for React
func handleAPIDisable2FA(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	var req struct {
		Password string `json:"password"`
		TotpCode string `json:"totp_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, req.Password) {
		Error(w, http.StatusUnauthorized, "wrong password")
		return
	}
	if u.TotpSecret != nil && !totp.Validate(req.TotpCode, *u.TotpSecret) {
		Error(w, http.StatusUnauthorized, "invalid totp code")
		return
	}
	store.DisableTotp(r.Context(), u.ID)
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

