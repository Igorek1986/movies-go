package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"lampa-api/db/models"
	"lampa-api/db/postgres"
	"net/http"
	"time"
)

const (
	sessionCookieName = "session_key"
	sessionTTL        = 30 * 24 * time.Hour
)

func GenerateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func CreateSession(ctx context.Context, userID int64, ip, userAgent string) (*models.Session, error) {
	key, err := GenerateKey()
	if err != nil {
		return nil, err
	}
	expiresAt := time.Now().Add(sessionTTL)

	var id int64
	err = postgres.Pool.QueryRow(ctx, `
		INSERT INTO sessions (user_id, key, expires_at, ip, user_agent)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		userID, key, expiresAt, nilStr(ip), nilStr(userAgent),
	).Scan(&id)
	if err != nil {
		return nil, err
	}
	return &models.Session{
		ID:        id,
		UserID:    userID,
		Key:       key,
		ExpiresAt: expiresAt,
	}, nil
}

// GetSessionUser validates the session key and returns the associated user.
// Returns nil if the session is missing or expired.
func GetSessionUser(ctx context.Context, key string) *models.User {
	if key == "" {
		return nil
	}

	var u models.User
	var totpSecret, backupCodes, blockReason *string
	var premiumUntil, blockedAt *time.Time

	err := postgres.Pool.QueryRow(ctx, `
		SELECT u.id, u.username, u.password_hash, u.role, u.is_admin,
		       u.totp_secret, u.totp_enabled, u.backup_codes,
		       u.premium_until, u.blocked_at, u.block_reason, u.created_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.key = $1 AND s.expires_at > now()`,
		key,
	).Scan(
		&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.IsAdmin,
		&totpSecret, &u.TotpEnabled, &backupCodes,
		&premiumUntil, &blockedAt, &blockReason, &u.CreatedAt,
	)
	if err != nil {
		return nil
	}
	u.TotpSecret = totpSecret
	u.BackupCodes = backupCodes
	u.PremiumUntil = premiumUntil
	u.BlockedAt = blockedAt
	u.BlockReason = blockReason

	// sliding window — extend session on use
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`UPDATE sessions SET expires_at = $1 WHERE key = $2`,
		time.Now().Add(sessionTTL), key,
	)
	return &u
}

func DeleteSession(ctx context.Context, key string) {
	postgres.Pool.Exec(ctx, `DELETE FROM sessions WHERE key = $1`, key) //nolint:errcheck
}

// SessionFromRequest extracts session key from cookie or query param ?session_key=
func SessionFromRequest(r *http.Request) string {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		return c.Value
	}
	return r.URL.Query().Get("session_key")
}

// SetSessionCookie writes the session cookie to the response.
func SetSessionCookie(w http.ResponseWriter, key string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    key,
		Expires:  expires,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie removes the session cookie.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:    sessionCookieName,
		Value:   "",
		MaxAge:  -1,
		Path:    "/",
		HttpOnly: true,
	})
}

func nilStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
