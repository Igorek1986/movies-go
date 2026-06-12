package store

import (
	"context"
	"crypto/rand"
	"fmt"
	"movies-api/db/models"
	"movies-api/db/postgres"
	"strings"
	"time"
)

// ─── Device tokens ────────────────────────────────────────────────────────────

const tokenAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no I,O,0,1 — easy to type

func generateToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	parts := make([]byte, 0, 19)
	for i, v := range b {
		if i > 0 && i%4 == 0 {
			parts = append(parts, '-')
		}
		parts = append(parts, tokenAlphabet[int(v)%len(tokenAlphabet)])
	}
	return string(parts), nil
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

type DeviceWithStats struct {
	models.Device
	TimecodesCount int `json:"timecodes_count"`
}

func GetDevicesWithStats(ctx context.Context, userID int64) []DeviceWithStats {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT d.id, d.user_id, d.name, d.token, d.created_at,
		       COUNT(t.id) AS timecodes_count
		FROM devices d
		LEFT JOIN timecodes t ON t.device_id = d.id
		WHERE d.user_id = $1
		GROUP BY d.id
		ORDER BY d.created_at`,
		userID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []DeviceWithStats
	for rows.Next() {
		var d DeviceWithStats
		if err := rows.Scan(&d.ID, &d.UserID, &d.Name, &d.Token, &d.CreatedAt, &d.TimecodesCount); err == nil {
			result = append(result, d)
		}
	}
	return result
}

func GetDeviceByID(ctx context.Context, id int64) *models.Device {
	var d models.Device
	err := postgres.Pool.QueryRow(ctx,
		`SELECT id, user_id, name, token, created_at FROM devices WHERE id = $1`, id,
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
	for suffix := 0; suffix <= 10; suffix++ {
		candidateName := name
		if suffix > 0 {
			candidateName = fmt.Sprintf("%s %d", name, suffix+1)
		}
		var d models.Device
		err = postgres.Pool.QueryRow(ctx, `
			INSERT INTO devices (user_id, name, token)
			VALUES ($1, $2, $3)
			RETURNING id, user_id, name, token, created_at`,
			userID, candidateName, token,
		).Scan(&d.ID, &d.UserID, &d.Name, &d.Token, &d.CreatedAt)
		if err == nil {
			return &d, nil
		}
		if !strings.Contains(err.Error(), "uq_devices_user_name") && !strings.Contains(err.Error(), "duplicate key") {
			return nil, err
		}
	}
	return nil, fmt.Errorf("device name conflict")
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

func RegenerateToken(ctx context.Context, deviceID, userID int64) (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", err
	}
	_, err = postgres.Pool.Exec(ctx,
		`UPDATE devices SET token = $1 WHERE id = $2 AND user_id = $3`,
		token, deviceID, userID,
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

func ClearDeviceTimecodes(ctx context.Context, deviceID, userID int64) error {
	_, err := postgres.Pool.Exec(ctx, `
		DELETE FROM timecodes WHERE device_id = (
			SELECT id FROM devices WHERE id = $1 AND user_id = $2
		)`, deviceID, userID,
	)
	return err
}

// ─── Device activation codes ──────────────────────────────────────────────────

const codeTTL = 10 * time.Minute

// DeviceCodeTTLSeconds returns the activation-code lifetime in seconds (for the
// client-side countdown on the device-pairing screen).
func DeviceCodeTTLSeconds() int {
	return int(codeTTL.Seconds())
}

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
// If existingDeviceID is non-nil, links to that device (must belong to userID).
// Otherwise creates a new device (with maxDevices limit check); returns the token on success.
func LinkDeviceCode(ctx context.Context, code string, userID int64, deviceName string, maxDevices int, existingDeviceID *int64) (string, error) {
	dc := GetDeviceCode(ctx, code)
	if dc == nil {
		return "", fmt.Errorf("code not found or expired")
	}
	if dc.DeviceID != nil {
		return "", fmt.Errorf("code already used")
	}

	var devID int64
	var token string

	if existingDeviceID != nil {
		// Link to existing device — verify ownership.
		err := postgres.Pool.QueryRow(ctx,
			`SELECT id, token FROM devices WHERE id = $1 AND user_id = $2`,
			*existingDeviceID, userID,
		).Scan(&devID, &token)
		if err != nil {
			return "", fmt.Errorf("device not found")
		}
	} else {
		if maxDevices > 0 && CountUserDevices(ctx, userID) >= maxDevices {
			return "", fmt.Errorf("device limit reached")
		}
		dev, err := CreateDevice(ctx, userID, deviceName)
		if err != nil {
			return "", err
		}
		devID = dev.ID
		token = dev.Token
	}

	_, err := postgres.Pool.Exec(ctx, `
		UPDATE device_codes SET user_id = $1, device_id = $2 WHERE code = $3`,
		userID, devID, code,
	)
	if err != nil {
		return "", err
	}
	return token, nil
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
