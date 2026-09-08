package store

import (
	"context"
	"movies-api/db/postgres"
	"time"
)

// WebExtensionRow is one entry in a user's web-SPA extension list (/profiles
// → «Расширения»). Scope is per-user, not per-device/profile like
// device_plugins — see the schema.sql comment on web_extensions.
type WebExtensionRow struct {
	ID              int64      `json:"id"`
	URL             string     `json:"url"`
	Name            string     `json:"name"`
	Enabled         bool       `json:"enabled"`
	Status          string     `json:"status"`
	StatusCode      *int       `json:"status_code"`
	StatusCheckedAt *time.Time `json:"status_checked_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

func ListWebExtensions(ctx context.Context, userID int64) []WebExtensionRow {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT id, url, name, enabled, status, status_code, status_checked_at, created_at
		FROM web_extensions WHERE user_id = $1 ORDER BY sort_order, id`,
		userID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []WebExtensionRow
	for rows.Next() {
		var e WebExtensionRow
		if err := rows.Scan(&e.ID, &e.URL, &e.Name, &e.Enabled, &e.Status, &e.StatusCode, &e.StatusCheckedAt, &e.CreatedAt); err == nil {
			result = append(result, e)
		}
	}
	return result
}

// AddWebExtension inserts a new extension for a user. Returns the new row, or
// an error (e.g. "uq_web_extensions_user_url" on duplicate URL).
func AddWebExtension(ctx context.Context, userID int64, url, name string, enabled bool, status string, statusCode *int) (WebExtensionRow, error) {
	e := WebExtensionRow{URL: url, Name: name, Enabled: enabled, Status: status, StatusCode: statusCode}
	now := time.Now()
	err := postgres.Pool.QueryRow(ctx, `
		INSERT INTO web_extensions (user_id, url, name, enabled, status, status_code, status_checked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`,
		userID, url, name, enabled, status, statusCode, now,
	).Scan(&e.ID, &e.CreatedAt)
	if err != nil {
		return WebExtensionRow{}, err
	}
	e.StatusCheckedAt = &now
	return e, nil
}

func UpdateWebExtension(ctx context.Context, id, userID int64, name *string, enabled *bool) error {
	_, err := postgres.Pool.Exec(ctx, `
		UPDATE web_extensions SET
			name = COALESCE($1, name),
			enabled = COALESCE($2, enabled)
		WHERE id = $3 AND user_id = $4`,
		name, enabled, id, userID,
	)
	return err
}

func SetWebExtensionStatus(ctx context.Context, id, userID int64, status string, statusCode *int) error {
	_, err := postgres.Pool.Exec(ctx, `
		UPDATE web_extensions SET status = $1, status_code = $2, status_checked_at = now()
		WHERE id = $3 AND user_id = $4`,
		status, statusCode, id, userID,
	)
	return err
}

func DeleteWebExtension(ctx context.Context, id, userID int64) error {
	_, err := postgres.Pool.Exec(ctx, `DELETE FROM web_extensions WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

// GetWebExtensionURL returns the stored URL for ownership-scoped lookups
// (e.g. re-checking status), or "" if not found/not owned.
func GetWebExtensionURL(ctx context.Context, id, userID int64) string {
	var url string
	err := postgres.Pool.QueryRow(ctx,
		`SELECT url FROM web_extensions WHERE id = $1 AND user_id = $2`, id, userID,
	).Scan(&url)
	if err != nil {
		return ""
	}
	return url
}
