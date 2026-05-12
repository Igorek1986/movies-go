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

func UpdatePassword(ctx context.Context, id int64, hash string) error {
	_, err := postgres.Pool.Exec(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, hash, id)
	return err
}

func SetUserRole(ctx context.Context, id int64, role string) error {
	_, err := postgres.Pool.Exec(ctx, `UPDATE users SET role = $1 WHERE id = $2`, role, id)
	return err
}

func DeleteUser(ctx context.Context, id int64) error {
	_, err := postgres.Pool.Exec(ctx, `DELETE FROM users WHERE id = $1 AND is_admin = false`, id)
	return err
}

type RoleCount struct {
	Role  string
	Count int
}

func QueryUserRoleCounts(ctx context.Context) ([]RoleCount, error) {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT role, COUNT(*) FROM users GROUP BY role`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []RoleCount
	for rows.Next() {
		var rc RoleCount
		if rows.Scan(&rc.Role, &rc.Count) == nil {
			result = append(result, rc)
		}
	}
	return result, nil
}

// ─── Notification settings ────────────────────────────────────────────────────

type NotificationSettings struct {
	Enabled      bool   `json:"enabled"`
	Timezone     string `json:"timezone"`
	NotifyStart  int    `json:"notify_start"`
	NotifyEnd    int    `json:"notify_end"`
}

func GetNotificationSettings(ctx context.Context, userID int64) NotificationSettings {
	var s NotificationSettings
	postgres.Pool.QueryRow(ctx, //nolint:errcheck
		`SELECT notifications_enabled, COALESCE(timezone,'Europe/Moscow'), notify_start, notify_end
		 FROM users WHERE id=$1`, userID,
	).Scan(&s.Enabled, &s.Timezone, &s.NotifyStart, &s.NotifyEnd)
	return s
}

func SaveNotificationSettings(ctx context.Context, userID int64, s NotificationSettings) error {
	_, err := postgres.Pool.Exec(ctx,
		`UPDATE users SET notifications_enabled=$2, timezone=$3, notify_start=$4, notify_end=$5 WHERE id=$1`,
		userID, s.Enabled, s.Timezone, s.NotifyStart, s.NotifyEnd,
	)
	return err
}

// CleanupUserOverlimit deletes devices beyond MaxDevices for the role (keeps oldest).
// Returns count of deleted devices (cascade removes associated profiles and timecodes).
func CleanupUserOverlimit(ctx context.Context, userID int64, role string) int {
	lim := LimitsFor(role)
	if lim.MaxDevices == 0 {
		return 0
	}
	tag, err := postgres.Pool.Exec(ctx, `
		DELETE FROM devices WHERE user_id = $1 AND id NOT IN (
			SELECT id FROM devices WHERE user_id = $1 ORDER BY created_at ASC LIMIT $2
		)`, userID, lim.MaxDevices)
	if err != nil {
		return 0
	}
	return int(tag.RowsAffected())
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
