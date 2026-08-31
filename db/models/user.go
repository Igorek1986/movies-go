package models

import "time"

type User struct {
	ID                int64      `db:"id"`
	Username          string     `db:"username"`
	PasswordHash      string     `db:"password_hash"`
	Role              string     `db:"role"` // simple / premium / super
	IsAdmin           bool       `db:"is_admin"`
	TotpSecret        *string    `db:"totp_secret"`
	TotpEnabled       bool       `db:"totp_enabled"`
	BackupCodes       *string    `db:"backup_codes"` // JSON list of SHA-256 hashes
	PremiumUntil      *time.Time `db:"premium_until"`
	BlockedAt         *time.Time `db:"blocked_at"`
	BlockReason       *string    `db:"block_reason"`
	CreatedAt         time.Time  `db:"created_at"`
	BottomNavKeys     *string    `db:"bottom_nav_keys"`     // comma-separated, NULL = default set
	BottomNavPosition *string    `db:"bottom_nav_position"` // "bottom" (default) or "right"
	CardLayout        *string    `db:"card_layout"`         // "hero" (default) or "classic"
	BrowseLayout      *string    `db:"browse_layout"`       // "hero" (default) or "classic"
	SettingsLayout    *string    `db:"settings_layout"`     // "remote" (default) or "classic"
}

type Session struct {
	ID        int64     `db:"id"`
	UserID    int64     `db:"user_id"`
	Key       string    `db:"key"`
	ExpiresAt time.Time `db:"expires_at"`
	CreatedAt time.Time `db:"created_at"`
	IP        *string   `db:"ip"`
	UserAgent *string   `db:"user_agent"`
}
