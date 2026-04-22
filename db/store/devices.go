package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"lampa-api/db/models"
	"lampa-api/db/postgres"
	"strings"
	"time"
)

// ─── Device tokens ────────────────────────────────────────────────────────────

func generateToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}

func GetDeviceByToken(ctx context.Context, token string) *models.Device {
	var d models.Device
	err := postgres.Pool.QueryRow(ctx,
		`SELECT id, user_id, name, token, created_at FROM devices WHERE token = $1`,
		token,
	).Scan(&d.ID, &d.UserID, &d.Name, &d.Token, &d.CreatedAt)
	if err != nil {
		return nil
	}
	return &d
}

func GetDevicesByUser(ctx context.Context, userID int64) []models.Device {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT id, user_id, name, token, created_at FROM devices WHERE user_id = $1 ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []models.Device
	for rows.Next() {
		var d models.Device
		if err := rows.Scan(&d.ID, &d.UserID, &d.Name, &d.Token, &d.CreatedAt); err == nil {
			result = append(result, d)
		}
	}
	return result
}

func CountUserDevices(ctx context.Context, userID int64) int {
	var n int
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices WHERE user_id = $1`, userID).Scan(&n) //nolint:errcheck
	return n
}

func CreateDevice(ctx context.Context, userID int64, name string) (*models.Device, error) {
	token, err := generateToken()
	if err != nil {
		return nil, err
	}
	var d models.Device
	err = postgres.Pool.QueryRow(ctx, `
		INSERT INTO devices (user_id, name, token)
		VALUES ($1, $2, $3)
		RETURNING id, user_id, name, token, created_at`,
		userID, name, token,
	).Scan(&d.ID, &d.UserID, &d.Name, &d.Token, &d.CreatedAt)
	return &d, err
}

func DeleteDevice(ctx context.Context, deviceID, userID int64) error {
	_, err := postgres.Pool.Exec(ctx,
		`DELETE FROM devices WHERE id = $1 AND user_id = $2`,
		deviceID, userID,
	)
	return err
}

func RenameDevice(ctx context.Context, deviceID, userID int64, name string) error {
	_, err := postgres.Pool.Exec(ctx,
		`UPDATE devices SET name = $1 WHERE id = $2 AND user_id = $3`,
		name, deviceID, userID,
	)
	return err
}

// ─── Device activation codes ──────────────────────────────────────────────────

const codeTTL = 10 * time.Minute

// generateActivationCode makes a 6-digit numeric string like "483921".
func generateActivationCode() string {
	b := make([]byte, 4)
	rand.Read(b) //nolint:errcheck
	n := (int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])) & 0x7FFFFFFF
	return fmt.Sprintf("%06d", n%1_000_000)
}

func CreateDeviceCode(ctx context.Context) (string, error) {
	// Clean up old codes first.
	postgres.Pool.Exec(ctx, `DELETE FROM device_codes WHERE expires_at < now()`) //nolint:errcheck

	for attempts := 0; attempts < 10; attempts++ {
		code := generateActivationCode()
		_, err := postgres.Pool.Exec(ctx, `
			INSERT INTO device_codes (code, expires_at) VALUES ($1, $2)`,
			code, time.Now().Add(codeTTL),
		)
		if err != nil && strings.Contains(err.Error(), "duplicate") {
			continue
		}
		return code, err
	}
	return "", fmt.Errorf("failed to generate unique code")
}

func GetDeviceCode(ctx context.Context, code string) *models.DeviceCode {
	var dc models.DeviceCode
	err := postgres.Pool.QueryRow(ctx, `
		SELECT id, code, user_id, device_id, expires_at, created_at
		FROM device_codes WHERE code = $1 AND expires_at > now()`,
		code,
	).Scan(&dc.ID, &dc.Code, &dc.UserID, &dc.DeviceID, &dc.ExpiresAt, &dc.CreatedAt)
	if err != nil {
		return nil
	}
	return &dc
}

// LinkDeviceCode links an activation code to a user's device.
// Creates the device if maxDevices allows; returns the device token on success.
func LinkDeviceCode(ctx context.Context, code string, userID int64, deviceName string, maxDevices int) (string, error) {
	dc := GetDeviceCode(ctx, code)
	if dc == nil {
		return "", fmt.Errorf("code not found or expired")
	}
	if dc.DeviceID != nil {
		return "", fmt.Errorf("code already used")
	}

	if maxDevices > 0 && CountUserDevices(ctx, userID) >= maxDevices {
		return "", fmt.Errorf("device limit reached")
	}

	dev, err := CreateDevice(ctx, userID, deviceName)
	if err != nil {
		return "", err
	}

	_, err = postgres.Pool.Exec(ctx, `
		UPDATE device_codes SET user_id = $1, device_id = $2 WHERE code = $3`,
		userID, dev.ID, code,
	)
	if err != nil {
		return "", err
	}
	return dev.Token, nil
}

// DeviceCodeStatus returns ("pending"|"linked"|"expired", token).
func DeviceCodeStatus(ctx context.Context, code string) (string, string) {
	var userID *int64
	var deviceID *int64
	var expiresAt time.Time
	var token *string

	err := postgres.Pool.QueryRow(ctx, `
		SELECT dc.user_id, dc.device_id, dc.expires_at, d.token
		FROM device_codes dc
		LEFT JOIN devices d ON d.id = dc.device_id
		WHERE dc.code = $1`,
		code,
	).Scan(&userID, &deviceID, &expiresAt, &token)
	if err != nil {
		return "expired", ""
	}
	if time.Now().After(expiresAt) {
		return "expired", ""
	}
	if deviceID != nil && token != nil {
		return "linked", *token
	}
	return "pending", ""
}
