package models

import "time"

type Device struct {
	ID        int64     `db:"id"`
	UserID    int64     `db:"user_id"`
	Name      string    `db:"name"`
	Token     string    `db:"token"`
	CreatedAt time.Time `db:"created_at"`
}

type DeviceCode struct {
	ID        int64      `db:"id"`
	Code      string     `db:"code"`
	UserID    *int64     `db:"user_id"`
	DeviceID  *int64     `db:"device_id"`
	ExpiresAt time.Time  `db:"expires_at"`
	CreatedAt time.Time  `db:"created_at"`
}

type LampaProfile struct {
	ID             int64  `db:"id"`
	DeviceID       int64  `db:"device_id"`
	LampaProfileID string `db:"lampa_profile_id"`
	Name           string `db:"name"`
	Icon           string `db:"icon"`
	Favorite       string `db:"favorite"` // JSON
	Child          bool   `db:"child"`
	Params         string `db:"params"` // JSON
}
