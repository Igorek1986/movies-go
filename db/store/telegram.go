package store

import (
	"context"
	"crypto/rand"
	"fmt"
	"movies-api/db/postgres"
	"math/big"
	"time"
)

// ─── Telegram users ────────────────────────────────────────────────────────────

type TelegramLink struct {
	UserID     int64
	TelegramID int64
	Username   string
}

func GetTelegramLinkByTelegramID(ctx context.Context, telegramID int64) *TelegramLink {
	var r TelegramLink
	var username *string
	err := postgres.Pool.QueryRow(ctx,
		`SELECT user_id, telegram_id, COALESCE(username,'') FROM telegram_users WHERE telegram_id=$1`,
		telegramID,
	).Scan(&r.UserID, &r.TelegramID, &username)
	if err != nil {
		return nil
	}
	if username != nil {
		r.Username = *username
	}
	return &r
}

func GetTelegramLinkByUserID(ctx context.Context, userID int64) *TelegramLink {
	var r TelegramLink
	var username *string
	err := postgres.Pool.QueryRow(ctx,
		`SELECT user_id, telegram_id, username FROM telegram_users WHERE user_id=$1`,
		userID,
	).Scan(&r.UserID, &r.TelegramID, &username)
	if err != nil {
		return nil
	}
	if username != nil {
		r.Username = *username
	}
	return &r
}

func UpsertTelegramLink(ctx context.Context, userID, telegramID int64, username string) error {
	var usernamePtr *string
	if username != "" {
		usernamePtr = &username
	}
	_, err := postgres.Pool.Exec(ctx,
		`INSERT INTO telegram_users (user_id, telegram_id, username)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id) DO UPDATE SET telegram_id=EXCLUDED.telegram_id, username=EXCLUDED.username`,
		userID, telegramID, usernamePtr,
	)
	return err
}

func DeleteTelegramLink(ctx context.Context, userID int64) {
	postgres.Pool.Exec(ctx, `DELETE FROM telegram_users WHERE user_id=$1`, userID) //nolint:errcheck
}

func GetAllTelegramIDs(ctx context.Context) []int64 {
	rows, err := postgres.Pool.Query(ctx, `SELECT telegram_id FROM telegram_users`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// ─── Telegram link codes ───────────────────────────────────────────────────────

func randDigits(n int) string {
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
	v, _ := rand.Int(rand.Reader, max)
	return fmt.Sprintf("%0*d", n, v.Int64())
}

func CreateTelegramLinkCode(ctx context.Context, userID int64, ttlMinutes int) (string, error) {
	if ttlMinutes <= 0 {
		ttlMinutes = 10
	}
	code := randDigits(6)
	postgres.Pool.Exec(ctx, `DELETE FROM telegram_link_codes WHERE user_id=$1`, userID) //nolint:errcheck
	_, err := postgres.Pool.Exec(ctx,
		`INSERT INTO telegram_link_codes (user_id, code, expires_at)
		 VALUES ($1, $2, now() + ($3 * INTERVAL '1 minute'))`,
		userID, code, ttlMinutes,
	)
	return code, err
}

func ConsumeTelegramLinkCode(ctx context.Context, code string) (int64, error) {
	var userID int64
	err := postgres.Pool.QueryRow(ctx,
		`DELETE FROM telegram_link_codes WHERE code=$1 AND expires_at > now() RETURNING user_id`,
		code,
	).Scan(&userID)
	return userID, err
}

// ─── Support messages ─────────────────────────────────────────────────────────

func SaveSupportMessage(ctx context.Context, userTelegramID int64, userUsername, direction, text string, adminTelegramID *int64, adminMsgID *int) {
	var usernamePtr *string
	if userUsername != "" {
		usernamePtr = &userUsername
	}
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`INSERT INTO support_messages (user_telegram_id, user_username, direction, text, admin_telegram_id, admin_msg_id)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		userTelegramID, usernamePtr, direction, text, adminTelegramID, adminMsgID,
	)
}

func GetUserTelegramIDByAdminMsgID(ctx context.Context, adminMsgID int) int64 {
	var telegramID int64
	postgres.Pool.QueryRow(ctx,
		`SELECT user_telegram_id FROM support_messages
		 WHERE admin_msg_id=$1 ORDER BY created_at DESC LIMIT 1`,
		adminMsgID,
	).Scan(&telegramID) //nolint:errcheck
	return telegramID
}

// ─── Password reset tokens ────────────────────────────────────────────────────

func CreatePasswordResetToken(ctx context.Context, userID int64, ttlMinutes int) (string, error) {
	if ttlMinutes <= 0 {
		ttlMinutes = 15
	}
	token := randDigits(6)
	postgres.Pool.Exec(ctx, `DELETE FROM password_reset_tokens WHERE user_id=$1`, userID) //nolint:errcheck
	_, err := postgres.Pool.Exec(ctx,
		`INSERT INTO password_reset_tokens (user_id, token, expires_at)
		 VALUES ($1, $2, now() + ($3 * INTERVAL '1 minute'))`,
		userID, token, ttlMinutes,
	)
	return token, err
}

func ConsumePasswordResetToken(ctx context.Context, token string) (int64, error) {
	var userID int64
	err := postgres.Pool.QueryRow(ctx,
		`DELETE FROM password_reset_tokens WHERE token=$1 AND expires_at > now() RETURNING user_id`,
		token,
	).Scan(&userID)
	return userID, err
}

// ─── Notifications ─────────────────────────────────────────────────────────────

type PendingNotification struct {
	UserID     int64
	TelegramID int64
	NotifyType string // "warning" / "expired" / "inactive_warn" / "inactive_delete"
	Username   string
	Timezone   string
	NotifyStart int
	NotifyEnd   int
}

// GetPendingNotifications returns users with pending notifications due now.
func GetPendingNotifications(ctx context.Context) []PendingNotification {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT u.id, tu.telegram_id, u.notify_type, u.username,
		       COALESCE(u.timezone, 'Europe/Moscow'),
		       u.notify_start, u.notify_end
		FROM users u
		JOIN telegram_users tu ON tu.user_id = u.id
		WHERE u.notifications_enabled = true
		  AND u.notify_premium_after IS NOT NULL
		  AND u.notify_premium_after <= now()`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []PendingNotification
	for rows.Next() {
		var n PendingNotification
		if rows.Scan(&n.UserID, &n.TelegramID, &n.NotifyType, &n.Username, &n.Timezone, &n.NotifyStart, &n.NotifyEnd) == nil {
			result = append(result, n)
		}
	}
	return result
}

// ClearPremiumNotify clears the notification fields after sending.
func ClearPremiumNotify(ctx context.Context, userID int64) {
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`UPDATE users SET notify_premium_after=NULL, notify_type=NULL WHERE id=$1`, userID)
}

type InactiveNotification struct {
	UserID     int64
	TelegramID *int64
	Username   string
	NotifyType string // "warn" or "delete"
}

// GetInactiveNotifications returns users whose inactive notification is due.
func GetInactiveNotifications(ctx context.Context) []InactiveNotification {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT u.id, tu.telegram_id, u.username, 'warn' AS notify_type
		FROM users u
		LEFT JOIN telegram_users tu ON tu.user_id = u.id
		WHERE u.role = 'simple'
		  AND u.notify_inactive_after IS NOT NULL
		  AND u.notify_inactive_after <= now()
		  AND u.inactive_warned = false`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []InactiveNotification
	for rows.Next() {
		var n InactiveNotification
		if rows.Scan(&n.UserID, &n.TelegramID, &n.Username, &n.NotifyType) == nil {
			result = append(result, n)
		}
	}
	return result
}

// ─── Premium expiry ────────────────────────────────────────────────────────────

type ExpiredPremiumUser struct {
	ID           int64
	Username     string
	PremiumUntil time.Time
	Timezone     string
	NotifyStart  int
	NotifyEnd    int
}

// GetExpiredPremiumUsers returns premium users whose subscription has expired.
func GetExpiredPremiumUsers(ctx context.Context) []ExpiredPremiumUser {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT id, username, premium_until,
		       COALESCE(timezone, 'Europe/Moscow'), notify_start, notify_end
		FROM users
		WHERE role = 'premium' AND premium_until IS NOT NULL AND premium_until < now()`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []ExpiredPremiumUser
	for rows.Next() {
		var u ExpiredPremiumUser
		if rows.Scan(&u.ID, &u.Username, &u.PremiumUntil, &u.Timezone, &u.NotifyStart, &u.NotifyEnd) == nil {
			result = append(result, u)
		}
	}
	return result
}

// GetExpiringPremiumUsers returns premium users expiring within warnDays and not yet warned.
func GetExpiringPremiumUsers(ctx context.Context, warnDays int) []ExpiredPremiumUser {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT id, username, premium_until,
		       COALESCE(timezone, 'Europe/Moscow'), notify_start, notify_end
		FROM users
		WHERE role = 'premium'
		  AND premium_until IS NOT NULL
		  AND premium_until > now()
		  AND premium_until < now() + ($1 * INTERVAL '1 day')
		  AND premium_warned = false`, warnDays)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []ExpiredPremiumUser
	for rows.Next() {
		var u ExpiredPremiumUser
		if rows.Scan(&u.ID, &u.Username, &u.PremiumUntil, &u.Timezone, &u.NotifyStart, &u.NotifyEnd) == nil {
			result = append(result, u)
		}
	}
	return result
}

// SetPremiumExpiredNotify demotes user to simple and schedules an expiry notification.
func SetPremiumExpiredNotify(ctx context.Context, userID int64, notifyAt time.Time) {
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`UPDATE users SET role='simple', notify_type='expired', notify_premium_after=$2,
		 premium_warned=false, timecode_grace_until=now() + (
		     SELECT (COALESCE(value,'3'))::int * INTERVAL '1 day' FROM app_settings WHERE key='timecode_grace_days'
		     UNION ALL SELECT INTERVAL '3 days' LIMIT 1
		 ) WHERE id=$1`, userID, notifyAt)
}

// SetPremiumWarningNotify schedules a warning notification for expiring premium.
func SetPremiumWarningNotify(ctx context.Context, userID int64, notifyAt time.Time) {
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`UPDATE users SET notify_type='warning', notify_premium_after=$2, premium_warned=true WHERE id=$1`,
		userID, notifyAt)
}

// InactiveUser represents a simple user overdue for deletion.
type InactiveUser struct {
	ID         int64
	Username   string
	LastActive *time.Time
	WarnedAt   *time.Time
	InactWarn  bool
	TelegramID *int64
}

// GetInactiveSimpleUsers returns simple users who haven't been active for deleteDays.
func GetInactiveSimpleUsers(ctx context.Context, deleteDays int) []InactiveUser {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT u.id, u.username, u.last_active_at::timestamptz, u.notify_inactive_after,
		       u.inactive_warned, tu.telegram_id
		FROM users u
		LEFT JOIN telegram_users tu ON tu.user_id = u.id
		WHERE u.role = 'simple'
		  AND u.is_admin = false
		  AND (u.last_active_at IS NULL OR u.last_active_at < CURRENT_DATE - ($1 * INTERVAL '1 day')::interval)`,
		deleteDays)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []InactiveUser
	for rows.Next() {
		var r InactiveUser
		if rows.Scan(&r.ID, &r.Username, &r.LastActive, &r.WarnedAt, &r.InactWarn, &r.TelegramID) == nil {
			result = append(result, r)
		}
	}
	return result
}
