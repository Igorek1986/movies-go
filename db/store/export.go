package store

import (
	"context"
	"encoding/json"
	"movies-api/db/postgres"
	"time"
)

// ─── Export types ─────────────────────────────────────────────────────────────

type ExportData struct {
	Version        int                  `json:"version"`
	ExportedAt     time.Time            `json:"exported_at"`
	Devices        []ExportDevice       `json:"devices"`
	PluginSettings []ExportPluginSetting `json:"plugin_settings"`
}

type ExportDevice struct {
	Name      string          `json:"name"`
	Token     string          `json:"token"`
	CreatedAt time.Time       `json:"created_at"`
	Profiles  []ExportProfile  `json:"profiles"`
	Timecodes []ExportTimecode `json:"timecodes"`
}

type ExportProfile struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Icon     *string         `json:"icon,omitempty"`
	Child    bool            `json:"child"`
	Params   json.RawMessage `json:"params"`
	Favorite *string         `json:"favorite,omitempty"`
}

type ExportTimecode struct {
	ProfileID  string  `json:"profile_id"`
	CardID     string  `json:"card_id"`
	Item       string  `json:"item"`
	Data       string  `json:"data"`
	ViewCount  int     `json:"view_count,omitempty"`
	CountedAt  *string `json:"counted_at,omitempty"`
}

type ExportPluginSetting struct {
	ProfileID string `json:"profile_id"`
	Plugin    string `json:"plugin"`
	Settings  string `json:"settings"`
}

// ─── Export ───────────────────────────────────────────────────────────────────

func ExportUserData(ctx context.Context, userID int64) (*ExportData, error) {
	out := &ExportData{
		Version:    1,
		ExportedAt: time.Now().UTC(),
	}

	// Devices
	rows, err := postgres.Pool.Query(ctx,
		`SELECT id, name, token, created_at FROM devices WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deviceIDs []int64
	for rows.Next() {
		var id int64
		var dev ExportDevice
		if err := rows.Scan(&id, &dev.Name, &dev.Token, &dev.CreatedAt); err != nil {
			continue
		}
		deviceIDs = append(deviceIDs, id)
		out.Devices = append(out.Devices, dev)
	}
	rows.Close()

	// Profiles + timecodes per device
	for i, devID := range deviceIDs {
		// Profiles
		prows, err := postgres.Pool.Query(ctx,
			`SELECT lampa_profile_id, name, icon, child, params, favorite
			 FROM lampa_profiles WHERE device_id = $1 ORDER BY id`, devID)
		if err == nil {
			for prows.Next() {
				var p ExportProfile
				var params []byte
				if err := prows.Scan(&p.ID, &p.Name, &p.Icon, &p.Child, &params, &p.Favorite); err != nil {
					continue
				}
				if len(params) > 0 {
					p.Params = json.RawMessage(params)
				} else {
					p.Params = json.RawMessage("{}")
				}
				out.Devices[i].Profiles = append(out.Devices[i].Profiles, p)
			}
			prows.Close()
		}

		// Timecodes
		trows, err := postgres.Pool.Query(ctx,
			`SELECT lampa_profile_id, card_id, item, data, view_count, TO_CHAR(counted_at, 'YYYY-MM-DD')
			 FROM timecodes WHERE device_id = $1 ORDER BY id`, devID)
		if err == nil {
			for trows.Next() {
				var t ExportTimecode
				if err := trows.Scan(&t.ProfileID, &t.CardID, &t.Item, &t.Data, &t.ViewCount, &t.CountedAt); err != nil {
					continue
				}
				out.Devices[i].Timecodes = append(out.Devices[i].Timecodes, t)
			}
			trows.Close()
		}
	}

	// Plugin settings
	psrows, err := postgres.Pool.Query(ctx,
		`SELECT lampa_profile_id, plugin, settings FROM plugin_settings WHERE user_id = $1`, userID)
	if err == nil {
		defer psrows.Close()
		for psrows.Next() {
			var ps ExportPluginSetting
			if psrows.Scan(&ps.ProfileID, &ps.Plugin, &ps.Settings) == nil {
				out.PluginSettings = append(out.PluginSettings, ps)
			}
		}
	}

	return out, nil
}

// ─── Import ───────────────────────────────────────────────────────────────────

func ImportUserData(ctx context.Context, userID int64, data *ExportData) error {
	tx, err := postgres.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Full replace: delete all existing data (CASCADE handles profiles + timecodes)
	if _, err := tx.Exec(ctx, `DELETE FROM devices WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM plugin_settings WHERE user_id = $1`, userID); err != nil {
		return err
	}

	for _, dev := range data.Devices {
		// Insert device; if token conflicts with another user, generate new one
		var devID int64
		err := tx.QueryRow(ctx,
			`INSERT INTO devices (user_id, name, token, created_at) VALUES ($1, $2, $3, $4)
			 ON CONFLICT (token) DO UPDATE SET name = EXCLUDED.name
			 RETURNING id`,
			userID, dev.Name, dev.Token, dev.CreatedAt,
		).Scan(&devID)
		if err != nil {
			return err
		}

		for _, p := range dev.Profiles {
			params := p.Params
			if len(params) == 0 {
				params = json.RawMessage("{}")
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO lampa_profiles (device_id, lampa_profile_id, name, icon, child, params, favorite)
				 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				devID, p.ID, p.Name, p.Icon, p.Child, []byte(params), p.Favorite,
			); err != nil {
				return err
			}
		}

		for _, t := range dev.Timecodes {
			if _, err := tx.Exec(ctx,
				`INSERT INTO timecodes (device_id, lampa_profile_id, card_id, item, data)
				 VALUES ($1, $2, $3, $4, $5)
				 ON CONFLICT (device_id, lampa_profile_id, card_id, item) DO UPDATE SET data = EXCLUDED.data`,
				devID, t.ProfileID, t.CardID, t.Item, t.Data,
			); err != nil {
				return err
			}
		}
	}

	for _, ps := range data.PluginSettings {
		if _, err := tx.Exec(ctx,
			`INSERT INTO plugin_settings (user_id, lampa_profile_id, plugin, settings)
			 VALUES ($1, $2, $3, $4)`,
			userID, ps.ProfileID, ps.Plugin, ps.Settings,
		); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}
