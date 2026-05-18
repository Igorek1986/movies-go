package store

import (
	"context"
	"encoding/json"
	"movies-api/db/postgres"
)

func GetPluginSettings(ctx context.Context, userID int64, profileID, plugin string) map[string]any {
	var raw *string
	err := postgres.Pool.QueryRow(ctx,
		`SELECT settings FROM plugin_settings
		 WHERE user_id=$1 AND lampa_profile_id=$2 AND plugin=$3`,
		userID, profileID, plugin,
	).Scan(&raw)
	if err != nil || raw == nil {
		return map[string]any{}
	}
	var m map[string]any
	json.Unmarshal([]byte(*raw), &m) //nolint:errcheck
	if m == nil {
		return map[string]any{}
	}
	return m
}

func PatchPluginSetting(ctx context.Context, userID int64, profileID, plugin, key string, value any) error {
	data := GetPluginSettings(ctx, userID, profileID, plugin)
	data[key] = value
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = postgres.Pool.Exec(ctx, `
		INSERT INTO plugin_settings (user_id, lampa_profile_id, plugin, settings)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, lampa_profile_id, plugin)
		DO UPDATE SET settings = EXCLUDED.settings, updated_at = now()`,
		userID, profileID, plugin, string(b),
	)
	return err
}
