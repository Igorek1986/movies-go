package api

import (
	"encoding/base64"
	"lampa-api/config"
	"lampa-api/db/store"
	"lampa-api/internal/auth"
	"lampa-api/internal/render"
	"net/http"

	"github.com/pquerna/otp/totp"
	qrcode "github.com/skip2/go-qrcode"
)

type setup2faData struct {
	QRDataURL string
	Secret    string
	Error     string
}

type verify2faData struct {
	Token string
	Error string
}

type backupCodesData struct {
	Codes []string
}

func buildQR(otpauthURL string) string {
	png, err := qrcode.Encode(otpauthURL, qrcode.Medium, 200)
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

func siteNameFromConfig() string {
	if n := config.Get().SiteName; n != "" {
		return n
	}
	return "NUMParser"
}

// GET /setup-2fa
func handleSetup2FAPage(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	// If user already has a pending (not-yet-enabled) secret, reuse it.
	// Otherwise generate a new one.
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
			http.Error(w, "error generating 2FA key", http.StatusInternalServerError)
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
		http.Error(w, "error generating QR", http.StatusInternalServerError)
		return
	}

	render.Page(w, r, "setup_2fa", u, setup2faData{
		QRDataURL: buildQR(key.URL()),
		Secret:    secret,
		Error:     r.URL.Query().Get("error"),
	})
}

// POST /setup-2fa — confirm TOTP code, enable 2FA, show backup codes
func handleSetup2FAConfirm(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/setup-2fa", http.StatusFound)
		return
	}

	code := r.FormValue("code")
	fresh := store.GetUserByID(r.Context(), u.ID)
	if fresh == nil || fresh.TotpSecret == nil {
		http.Redirect(w, r, "/setup-2fa", http.StatusFound)
		return
	}

	if !totp.Validate(code, *fresh.TotpSecret) {
		http.Redirect(w, r, "/setup-2fa?error=invalid_code", http.StatusFound)
		return
	}

	plainCodes, hashesJSON := store.GenerateBackupCodes()
	store.EnableTotp(r.Context(), u.ID, hashesJSON)

	render.Page(w, r, "backup_codes", u, backupCodesData{Codes: plainCodes})
}

// POST /disable-2fa
func handleDisable2FA(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/profiles", http.StatusFound)
		return
	}
	if !auth.CheckPassword(u.PasswordHash, r.FormValue("password")) {
		http.Redirect(w, r, "/profiles?error=wrong_password", http.StatusFound)
		return
	}
	store.DisableTotp(r.Context(), u.ID)
	http.Redirect(w, r, "/profiles?success=2fa_disabled", http.StatusFound)
}

// GET /verify-2fa?t=TOKEN
func handleVerify2FAPage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("t")
	if token == "" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	errMsg := r.URL.Query().Get("error")
	if errMsg == "invalid_code" {
		errMsg = "Неверный код. Попробуйте ещё раз."
	}
	render.Page(w, r, "verify_2fa", nil, verify2faData{
		Token: token,
		Error: errMsg,
	})
}

// POST /verify-2fa
func handleVerify2FASubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	pendingToken := r.FormValue("token")
	code := r.FormValue("code")

	ctx := r.Context()
	userID := store.ConsumeTotpPendingToken(ctx, pendingToken)
	if userID == 0 {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	u := store.GetUserByID(ctx, userID)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	valid := false
	if u.TotpSecret != nil {
		valid = totp.Validate(code, *u.TotpSecret)
	}
	if !valid {
		valid = store.UseBackupCode(ctx, userID, code)
	}

	if !valid {
		ttl := store.GetSettingInt(ctx, "pending_2fa_ttl_sec")
		if ttl <= 0 {
			ttl = 600
		}
		newToken, err := store.CreateTotpPendingToken(ctx, userID, ttl)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/verify-2fa?t="+newToken+"&error=invalid_code", http.StatusFound)
		return
	}

	sess, err := auth.CreateSession(ctx, userID, r.RemoteAddr, r.Header.Get("User-Agent"))
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	auth.SetSessionCookie(w, sess.Key, sess.ExpiresAt)
	http.Redirect(w, r, "/", http.StatusFound)
}
