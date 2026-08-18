package store

import (
	"context"
	"movies-api/db/postgres"
	"time"
)

type DevicePlugin struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

// GetDevicePlugins returns the merged, enabled plugin URL list for a
// device+profile: the device-level set, with per-profile overrides applied
// on top (an override always wins, whether it enables an extra URL or
// disables a globally-enabled one).
func GetDevicePlugins(ctx context.Context, deviceID int64, profileID string) []DevicePlugin {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT url, name, enabled FROM device_plugins WHERE device_id = $1 ORDER BY id`,
		deviceID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	type entry struct {
		name    string
		enabled bool
	}
	var order []string
	byURL := map[string]entry{}
	for rows.Next() {
		var url, name string
		var enabled bool
		if err := rows.Scan(&url, &name, &enabled); err != nil {
			continue
		}
		order = append(order, url)
		byURL[url] = entry{name: name, enabled: enabled}
	}

	if profileID != "" {
		orows, err := postgres.Pool.Query(ctx,
			`SELECT url, enabled FROM device_plugin_profile_overrides WHERE device_id = $1 AND profile_id = $2`,
			deviceID, profileID,
		)
		if err == nil {
			defer orows.Close()
			for orows.Next() {
				var url string
				var enabled bool
				if err := orows.Scan(&url, &enabled); err != nil {
					continue
				}
				e, exists := byURL[url]
				if !exists {
					order = append(order, url)
				}
				e.enabled = enabled
				byURL[url] = e
			}
		}
	}

	result := make([]DevicePlugin, 0, len(order))
	for _, url := range order {
		e := byURL[url]
		if e.enabled {
			result = append(result, DevicePlugin{URL: url, Name: e.name})
		}
	}
	return result
}

// SeedDefaultDevicePlugins copies the admin-configured default_device_plugins
// set onto a newly created device, so it isn't left running zero plugins
// (including np.js itself) between linking and the first visit to the
// "Плагины" panel. Empty by default on a fresh install — see the
// "Плагины по умолчанию" section on the admin settings page.
func SeedDefaultDevicePlugins(ctx context.Context, deviceID int64) {
	postgres.Pool.Exec(ctx, `
		INSERT INTO device_plugins (device_id, url, name, enabled)
		SELECT $1, url, name, enabled FROM default_device_plugins
		ON CONFLICT (device_id, url) DO NOTHING`,
		deviceID,
	) //nolint:errcheck
}

// ─── Admin-configured defaults ────────────────────────────────────────────

func ListDefaultDevicePlugins(ctx context.Context) []DevicePluginRow {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT id, url, name, enabled, created_at FROM default_device_plugins ORDER BY id`,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []DevicePluginRow
	for rows.Next() {
		var p DevicePluginRow
		if err := rows.Scan(&p.ID, &p.URL, &p.Name, &p.Enabled, &p.CreatedAt); err == nil {
			result = append(result, p)
		}
	}
	return result
}

func AddDefaultDevicePlugin(ctx context.Context, url, name string, enabled bool) (DevicePluginRow, error) {
	var p DevicePluginRow
	p.URL, p.Name, p.Enabled = url, name, enabled
	err := postgres.Pool.QueryRow(ctx, `
		INSERT INTO default_device_plugins (url, name, enabled)
		VALUES ($1, $2, $3)
		RETURNING id, created_at`,
		url, name, enabled,
	).Scan(&p.ID, &p.CreatedAt)
	if err != nil {
		return DevicePluginRow{}, err
	}
	return p, nil
}

func UpdateDefaultDevicePlugin(ctx context.Context, pluginID int64, name, url *string, enabled *bool) error {
	_, err := postgres.Pool.Exec(ctx, `
		UPDATE default_device_plugins SET
			name = COALESCE($1, name),
			url = COALESCE($2, url),
			enabled = COALESCE($3, enabled)
		WHERE id = $4`,
		name, url, enabled, pluginID,
	)
	return err
}

func DeleteDefaultDevicePlugin(ctx context.Context, pluginID int64) error {
	_, err := postgres.Pool.Exec(ctx, `DELETE FROM default_device_plugins WHERE id = $1`, pluginID)
	return err
}

// ─── Web (session-auth) management ───────────────────────────────────────────

type DevicePluginRow struct {
	ID        int64     `json:"id"`
	URL       string    `json:"url"`
	Name      string    `json:"name"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

func ListDevicePlugins(ctx context.Context, deviceID int64) []DevicePluginRow {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT id, url, name, enabled, created_at FROM device_plugins WHERE device_id = $1 ORDER BY id`,
		deviceID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []DevicePluginRow
	for rows.Next() {
		var p DevicePluginRow
		if err := rows.Scan(&p.ID, &p.URL, &p.Name, &p.Enabled, &p.CreatedAt); err == nil {
			result = append(result, p)
		}
	}
	return result
}

// AddDevicePlugin inserts a plugin URL for a device the user owns. Returns
// the new row, or an error (e.g. "uq_device_plugins_url" on duplicate URL).
func AddDevicePlugin(ctx context.Context, deviceID, userID int64, url, name string, enabled bool) (DevicePluginRow, error) {
	var p DevicePluginRow
	p.URL, p.Name, p.Enabled = url, name, enabled
	err := postgres.Pool.QueryRow(ctx, `
		INSERT INTO device_plugins (device_id, url, name, enabled)
		SELECT $1, $2, $3, $4 WHERE EXISTS (SELECT 1 FROM devices WHERE id = $1 AND user_id = $5)
		RETURNING id, created_at`,
		deviceID, url, name, enabled, userID,
	).Scan(&p.ID, &p.CreatedAt)
	if err != nil {
		return DevicePluginRow{}, err
	}
	return p, nil
}

func UpdateDevicePlugin(ctx context.Context, pluginID, deviceID, userID int64, name, url *string, enabled *bool) error {
	_, err := postgres.Pool.Exec(ctx, `
		UPDATE device_plugins SET
			name = COALESCE($1, name),
			url = COALESCE($2, url),
			enabled = COALESCE($3, enabled)
		WHERE id = $4 AND device_id = $5
		  AND device_id IN (SELECT id FROM devices WHERE user_id = $6)`,
		name, url, enabled, pluginID, deviceID, userID,
	)
	return err
}

func DeleteDevicePlugin(ctx context.Context, pluginID, deviceID, userID int64) error {
	_, err := postgres.Pool.Exec(ctx, `
		DELETE FROM device_plugins
		WHERE id = $1 AND device_id = $2
		  AND device_id IN (SELECT id FROM devices WHERE user_id = $3)`,
		pluginID, deviceID, userID,
	)
	return err
}

// ProfilePluginRow is a merged view for the profile override editor: the
// device-level plugin (if any) plus its override state for one profile.
type ProfilePluginRow struct {
	URL           string `json:"url"`
	Name          string `json:"name"`
	InDeviceList  bool   `json:"in_device_list"`
	DeviceEnabled bool   `json:"device_enabled"`
	Override      *bool  `json:"override"` // nil = inherit device_enabled
}

func ListProfilePlugins(ctx context.Context, deviceID int64, profileID string) []ProfilePluginRow {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT id, url, name, enabled FROM device_plugins WHERE device_id = $1 ORDER BY id`,
		deviceID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var order []string
	byURL := map[string]*ProfilePluginRow{}
	for rows.Next() {
		var id int64
		var url, name string
		var enabled bool
		if err := rows.Scan(&id, &url, &name, &enabled); err != nil {
			continue
		}
		order = append(order, url)
		byURL[url] = &ProfilePluginRow{URL: url, Name: name, InDeviceList: true, DeviceEnabled: enabled}
	}

	orows, err := postgres.Pool.Query(ctx,
		`SELECT url, enabled, name FROM device_plugin_profile_overrides WHERE device_id = $1 AND profile_id = $2`,
		deviceID, profileID,
	)
	if err == nil {
		defer orows.Close()
		for orows.Next() {
			var url, name string
			var enabled bool
			if err := orows.Scan(&url, &enabled, &name); err != nil {
				continue
			}
			p, exists := byURL[url]
			if !exists {
				p = &ProfilePluginRow{URL: url, Name: name}
				byURL[url] = p
				order = append(order, url)
			}
			e := enabled
			p.Override = &e
		}
	}

	result := make([]ProfilePluginRow, 0, len(order))
	for _, url := range order {
		result = append(result, *byURL[url])
	}
	return result
}

// SetProfilePluginOverride upserts an explicit enabled/disabled override for
// one URL on one profile. name is only meaningful for profile-only URLs (not
// in the device's list, so they have nothing else to display) — pass nil to
// leave the stored name untouched (e.g. when just toggling enabled).
// Ownership is checked via the device_id subquery.
func SetProfilePluginOverride(ctx context.Context, deviceID, userID int64, profileID, url string, enabled bool, name *string) error {
	_, err := postgres.Pool.Exec(ctx, `
		INSERT INTO device_plugin_profile_overrides (device_id, profile_id, url, enabled, name)
		SELECT $1, $2, $3, $4, COALESCE($6, '') WHERE EXISTS (SELECT 1 FROM devices WHERE id = $1 AND user_id = $5)
		ON CONFLICT (device_id, profile_id, url) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			name = COALESCE($6, device_plugin_profile_overrides.name)`,
		deviceID, profileID, url, enabled, userID, name,
	)
	return err
}

// ClearProfilePluginOverride removes an override, reverting the profile to
// inherit the device-level enabled flag for that URL.
func ClearProfilePluginOverride(ctx context.Context, deviceID, userID int64, profileID, url string) error {
	_, err := postgres.Pool.Exec(ctx, `
		DELETE FROM device_plugin_profile_overrides
		WHERE device_id = $1 AND profile_id = $2 AND url = $3
		  AND device_id IN (SELECT id FROM devices WHERE user_id = $4)`,
		deviceID, profileID, url, userID,
	)
	return err
}
