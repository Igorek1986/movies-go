package store

import (
	"context"
	"crypto/rand"
	"fmt"
	"movies-api/db/models"
	"movies-api/db/postgres"
	"movies-api/internal/auth"
	"time"
)

func GetUserByUsername(ctx context.Context, username string) *models.User {
	var u models.User
	var totpSecret, backupCodes, blockReason *string
	var premiumUntil, blockedAt *time.Time

	err := postgres.Pool.QueryRow(ctx, `
		SELECT id, username, password_hash, role, is_admin,
		       totp_secret, totp_enabled, backup_codes,
		       premium_until, blocked_at, block_reason, created_at, must_change_password
		FROM users WHERE username = $1`,
		username,
	).Scan(
		&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.IsAdmin,
		&totpSecret, &u.TotpEnabled, &backupCodes,
		&premiumUntil, &blockedAt, &blockReason, &u.CreatedAt, &u.MustChangePassword,
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
		       premium_until, blocked_at, block_reason, created_at, must_change_password
		FROM users WHERE id = $1`,
		id,
	).Scan(
		&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.IsAdmin,
		&totpSecret, &u.TotpEnabled, &backupCodes,
		&premiumUntil, &blockedAt, &blockReason, &u.CreatedAt, &u.MustChangePassword,
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

// CreateUser inserts a new account. mustChangePassword forces a password
// change on first login — set for admin-created accounts (see
// registration_disabled setting / GenerateUniqueUsername+GenerateRandomPassword),
// false for self-registration.
func CreateUser(ctx context.Context, username, passwordHash, role string, mustChangePassword bool) (*models.User, error) {
	var u models.User
	err := postgres.Pool.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, role, must_change_password)
		VALUES ($1, $2, $3, $4)
		RETURNING id, username, password_hash, role, is_admin, created_at, must_change_password`,
		username, passwordHash, role, mustChangePassword,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.IsAdmin, &u.CreatedAt, &u.MustChangePassword)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

const usernameAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// No 0/O/1/l/I — easy to read and type out loud when handing a temp password to someone.
const generatedPasswordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"

func randomAlnum(alphabet string, n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, v := range b {
		out[i] = alphabet[int(v)%len(alphabet)]
	}
	return string(out), nil
}

// GenerateUniqueUsername makes a random "userXXXXXX" login not already taken
// — used when an admin creates an account manually (self-registration
// disabled, see registration_disabled setting).
func GenerateUniqueUsername(ctx context.Context) (string, error) {
	for attempts := 0; attempts < 20; attempts++ {
		suffix, err := randomAlnum(usernameAlphabet, 6)
		if err != nil {
			return "", err
		}
		username := "user" + suffix
		if GetUserByUsername(ctx, username) == nil {
			return username, nil
		}
	}
	return "", fmt.Errorf("could not generate a unique username")
}

// GenerateRandomPassword makes a random plaintext password for an
// admin-created account — shown to the admin once, then hashed like any
// other password. The account is created with must_change_password = true.
// Rejection-sampled against auth.ValidatePasswordStrength (the same policy
// enforced on every user-chosen password) rather than duplicating its rules
// here — the mixed-case+digit alphabet satisfies it within a try or two
// almost always, so this never loops meaningfully in practice.
func GenerateRandomPassword(ctx context.Context) (string, error) {
	blocklist := PasswordBlocklist(ctx)
	for attempts := 0; attempts < 20; attempts++ {
		password, err := randomAlnum(generatedPasswordAlphabet, 12)
		if err != nil {
			return "", err
		}
		if auth.ValidatePasswordStrength(password, blocklist) == "" {
			return password, nil
		}
	}
	return "", fmt.Errorf("could not generate a password meeting the strength policy")
}

func UsersExist(ctx context.Context) bool {
	var n int
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n) //nolint:errcheck
	return n > 0
}

// UpdatePassword also clears must_change_password — any successful password
// change (self-service or forced) satisfies that requirement.
func UpdatePassword(ctx context.Context, id int64, hash string) error {
	_, err := postgres.Pool.Exec(ctx, `UPDATE users SET password_hash = $1, must_change_password = false WHERE id = $2`, hash, id)
	return err
}

func SetUserRole(ctx context.Context, id int64, role string) error {
	_, err := postgres.Pool.Exec(ctx, `UPDATE users SET role = $1 WHERE id = $2`, role, id)
	return err
}

// SetUserBottomNavConfig saves the user's mobile/tablet bottom-nav bar
// configuration: comma-separated option keys (see BOTTOM_NAV_OPTIONS on the
// frontend) and its screen position ("bottom" or "right").
func SetUserBottomNavConfig(ctx context.Context, id int64, keys, position string) error {
	_, err := postgres.Pool.Exec(ctx, `UPDATE users SET bottom_nav_keys = $1, bottom_nav_position = $2 WHERE id = $3`, keys, position, id)
	return err
}

// interfacePrefColumns maps the frontend's field names to the actual column
// — a fixed, hardcoded set (never built from the request), so the
// interpolation below can't be used for injection.
var interfacePrefColumns = map[string]string{
	"card_layout":     "card_layout",
	"browse_layout":   "browse_layout",
	"settings_layout": "settings_layout",
	"theme":           "theme",
}

// SetUserInterfacePref saves one per-account UI layout preference (card
// layout, browse layout, or the /profiles page's own layout) — see
// interfacePrefColumns for the allowed field names.
func SetUserInterfacePref(ctx context.Context, id int64, field, value string) error {
	col, ok := interfacePrefColumns[field]
	if !ok {
		return fmt.Errorf("unknown interface pref field: %s", field)
	}
	_, err := postgres.Pool.Exec(ctx, fmt.Sprintf(`UPDATE users SET %s = $1 WHERE id = $2`, col), value, id)
	return err
}

func DeleteUser(ctx context.Context, id int64) error {
	_, err := postgres.Pool.Exec(ctx, `DELETE FROM users WHERE id = $1 AND is_admin = false`, id)
	return err
}

// DeleteStalePendingPasswordUsers removes admin-created accounts (generated
// login+password, see handleAdminCreateUser) that never completed their
// first login — must_change_password still true — more than olderThanDays
// after creation. Returns the deleted usernames for logging.
func DeleteStalePendingPasswordUsers(ctx context.Context, olderThanDays int) ([]string, error) {
	rows, err := postgres.Pool.Query(ctx, `
		DELETE FROM users
		WHERE must_change_password = true
		  AND is_admin = false
		  AND created_at < now() - ($1 || ' days')::interval
		RETURNING username`, olderThanDays)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var usernames []string
	for rows.Next() {
		var u string
		if rows.Scan(&u) == nil {
			usernames = append(usernames, u)
		}
	}
	return usernames, rows.Err()
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
	Enabled     bool   `json:"enabled"`
	Timezone    string `json:"timezone"`
	NotifyStart int    `json:"notify_start"`
	NotifyEnd   int    `json:"notify_end"`
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
