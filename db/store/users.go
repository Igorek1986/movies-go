package store

import (
	"context"
	"lampa-api/db/models"
	"lampa-api/db/postgres"
	"time"
)

func GetUserByUsername(ctx context.Context, username string) *models.User {
	var u models.User
	var totpSecret, backupCodes, blockReason *string
	var premiumUntil, blockedAt *time.Time

	err := postgres.Pool.QueryRow(ctx, `
		SELECT id, username, password_hash, role, is_admin,
		       totp_secret, totp_enabled, backup_codes,
		       premium_until, blocked_at, block_reason, created_at
		FROM users WHERE username = $1`,
		username,
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
	return &u
}

func GetUserByID(ctx context.Context, id int64) *models.User {
	var u models.User
	var totpSecret, backupCodes, blockReason *string
	var premiumUntil, blockedAt *time.Time

	err := postgres.Pool.QueryRow(ctx, `
		SELECT id, username, password_hash, role, is_admin,
		       totp_secret, totp_enabled, backup_codes,
		       premium_until, blocked_at, block_reason, created_at
		FROM users WHERE id = $1`,
		id,
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
	return &u
}

func CreateUser(ctx context.Context, username, passwordHash, role string) (*models.User, error) {
	var u models.User
	err := postgres.Pool.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, role)
		VALUES ($1, $2, $3)
		RETURNING id, username, password_hash, role, is_admin, created_at`,
		username, passwordHash, role,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.IsAdmin, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func UsersExist(ctx context.Context) bool {
	var n int
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n) //nolint:errcheck
	return n > 0
}

// EnsureSuperuser creates the superuser if no users exist yet.
func EnsureSuperuser(ctx context.Context, username, passwordHash string) error {
	_, err := postgres.Pool.Exec(ctx, `
		INSERT INTO users (username, password_hash, role, is_admin)
		VALUES ($1, $2, 'super', true)
		ON CONFLICT (username) DO NOTHING`,
		username, passwordHash,
	)
	return err
}
