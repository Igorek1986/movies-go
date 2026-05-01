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

type SessionInfo struct {
	ID        int64
	Browser   string
	IP        string
	CreatedAt time.Time
	IsCurrent bool
}

// ListSessions returns all active sessions for a user.
func ListSessions(ctx context.Context, userID int64, currentKey string) []SessionInfo {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT id, ip, user_agent, created_at
		FROM sessions
		WHERE user_id = $1 AND expires_at > now()
		ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []SessionInfo
	for rows.Next() {
		var id int64
		var ip, ua *string
		var createdAt time.Time
		if err := rows.Scan(&id, &ip, &ua, &createdAt); err != nil {
			continue
		}
		browser := parseUserAgent(ua)
		ipStr := "—"
		if ip != nil {
			ipStr = *ip
		}
		result = append(result, SessionInfo{
			ID:        id,
			Browser:   browser,
			IP:        ipStr,
			CreatedAt: createdAt,
			IsCurrent: false,
		})
	}
	// Mark current session
	if currentKey != "" {
		var currentID int64
		postgres.Pool.QueryRow(ctx, `SELECT id FROM sessions WHERE key=$1`, currentKey).Scan(&currentID) //nolint:errcheck
		for i := range result {
			if result[i].ID == currentID {
				result[i].IsCurrent = true
			}
		}
	}
	return result
}

// DeleteSessionByID deletes a session by ID, only if it belongs to userID.
// Returns the session key (for cookie clearing if it's the current session).
func DeleteSessionByID(ctx context.Context, sessionID, userID int64) string {
	var key string
	postgres.Pool.QueryRow(ctx,
		`DELETE FROM sessions WHERE id=$1 AND user_id=$2 RETURNING key`,
		sessionID, userID,
	).Scan(&key) //nolint:errcheck
	return key
}

// DeleteAllUserSessions deletes all sessions for a user.
func DeleteAllUserSessions(ctx context.Context, userID int64) {
	postgres.Pool.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, userID) //nolint:errcheck
}

func parseUserAgent(ua *string) string {
	if ua == nil || *ua == "" {
		return "Неизвестный браузер"
	}
	s := *ua
	switch {
	case containsAny(s, "Firefox/"):
		return "Firefox"
	case containsAny(s, "Edg/", "Edge/"):
		return "Edge"
	case containsAny(s, "OPR/", "Opera/"):
		return "Opera"
	case containsAny(s, "Chrome/") && containsAny(s, "Safari/"):
		return "Chrome"
	case containsAny(s, "Safari/") && !containsAny(s, "Chrome/"):
		return "Safari"
	case containsAny(s, "curl/"):
		return "curl"
	default:
		if len(s) > 40 {
			return s[:40] + "…"
		}
		return s
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
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
