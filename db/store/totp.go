package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"lampa-api/db/postgres"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SetTotpSecret saves a pending TOTP secret without enabling 2FA yet.
func SetTotpSecret(ctx context.Context, userID int64, secret string) {
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`UPDATE users SET totp_secret = $1, totp_enabled = false WHERE id = $2`, secret, userID)
}

// EnableTotp marks 2FA as active and stores backup code hashes.
func EnableTotp(ctx context.Context, userID int64, backupCodesJSON string) {
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`UPDATE users SET totp_enabled = true, backup_codes = $1 WHERE id = $2`, backupCodesJSON, userID)
}

// DisableTotp removes 2FA completely.
func DisableTotp(ctx context.Context, userID int64) {
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`UPDATE users SET totp_secret = NULL, totp_enabled = false, backup_codes = NULL WHERE id = $1`, userID)
}

// GenerateBackupCodes creates 8 random codes (format XXXX-XXXX) and returns
// both the plaintext codes and their SHA-256 hashes as a JSON string.
func GenerateBackupCodes() (plaintext []string, hashesJSON string) {
	const n = 8
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	var hashes []string
	for i := 0; i < n; i++ {
		b := make([]byte, 8)
		rand.Read(b) //nolint:errcheck
		var code strings.Builder
		for j, bb := range b {
			code.WriteByte(chars[int(bb)%len(chars)])
			if j == 3 {
				code.WriteByte('-')
			}
		}
		plain := code.String()
		plaintext = append(plaintext, plain)
		h := sha256.Sum256([]byte(plain))
		hashes = append(hashes, hex.EncodeToString(h[:]))
	}
	data, _ := json.Marshal(hashes)
	hashesJSON = string(data)
	return
}

// UseBackupCode checks if the given code matches any stored hash and removes it.
// Returns true if a code was found and consumed.
func UseBackupCode(ctx context.Context, userID int64, code string) bool {
	var raw *string
	err := postgres.Pool.QueryRow(ctx,
		`SELECT backup_codes FROM users WHERE id = $1`, userID).Scan(&raw)
	if err != nil || raw == nil {
		return false
	}
	var hashes []string
	if err := json.Unmarshal([]byte(*raw), &hashes); err != nil {
		return false
	}
	code = strings.ToUpper(strings.ReplaceAll(code, " ", ""))
	h := sha256.Sum256([]byte(code))
	target := hex.EncodeToString(h[:])
	remaining := make([]string, 0, len(hashes))
	found := false
	for _, stored := range hashes {
		if stored == target {
			found = true
		} else {
			remaining = append(remaining, stored)
		}
	}
	if !found {
		return false
	}
	data, _ := json.Marshal(remaining)
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`UPDATE users SET backup_codes = $1 WHERE id = $2`, string(data), userID)
	return true
}

// CreateTotpPendingToken creates a short-lived login-pending token for 2FA step.
func CreateTotpPendingToken(ctx context.Context, userID int64, ttlSec int) (string, error) {
	token := uuid.New().String()
	expires := time.Now().Add(time.Duration(ttlSec) * time.Second)
	_, err := postgres.Pool.Exec(ctx,
		`INSERT INTO totp_2fa_pending (user_id, token, expires_at) VALUES ($1, $2, $3)`,
		userID, token, expires)
	return token, err
}

// ConsumeTotpPendingToken validates and deletes a login-pending token.
// Returns userID on success, 0 if not found or expired.
func ConsumeTotpPendingToken(ctx context.Context, token string) int64 {
	var userID int64
	err := postgres.Pool.QueryRow(ctx,
		`DELETE FROM totp_2fa_pending WHERE token = $1 AND expires_at > now() RETURNING user_id`,
		token).Scan(&userID)
	if err != nil {
		return 0
	}
	return userID
}
