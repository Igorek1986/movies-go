package store

import (
	"context"

	"movies-api/db/postgres"
)

// ExternalSourceKeys — известные внешние источники runtime-фолбэков (см.
// схему external_source_tokens и internal/tasks/fix_runtime.go).
var ExternalSourceKeys = []string{"tvmaze", "thetvdb", "poiskkino", "kinopoisk_unofficial"}

// ExternalSource — статус одного источника для админки. Token отдаётся в
// открытом виде — этот эндпоинт только для requireAnyAdmin, чтобы можно
// было сверить сохранённый ключ глазами, не гадая по плейсхолдеру.
type ExternalSource struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
	Token   string `json:"token"`
}

// ListExternalSources returns the status of every known external source,
// even ones with no row yet (defaults: disabled, no token).
func ListExternalSources(ctx context.Context) []ExternalSource {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT source_key, enabled, token FROM external_source_tokens`)
	current := map[string]ExternalSource{}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var key, token string
			var enabled bool
			if rows.Scan(&key, &enabled, &token) == nil {
				current[key] = ExternalSource{Key: key, Enabled: enabled, Token: token}
			}
		}
	}

	out := make([]ExternalSource, 0, len(ExternalSourceKeys))
	for _, key := range ExternalSourceKeys {
		if v, ok := current[key]; ok {
			out = append(out, v)
		} else {
			out = append(out, ExternalSource{Key: key})
		}
	}
	return out
}

// SetExternalSourceEnabled toggles a source on/off without touching its token.
func SetExternalSourceEnabled(ctx context.Context, key string, enabled bool) error {
	_, err := postgres.Pool.Exec(ctx, `
		INSERT INTO external_source_tokens (source_key, enabled) VALUES ($1, $2)
		ON CONFLICT (source_key) DO UPDATE SET enabled = $2, updated_at = now()`,
		key, enabled)
	return err
}

// SetExternalSourceToken sets/replaces a source's token without touching enabled.
func SetExternalSourceToken(ctx context.Context, key, token string) error {
	_, err := postgres.Pool.Exec(ctx, `
		INSERT INTO external_source_tokens (source_key, token) VALUES ($1, $2)
		ON CONFLICT (source_key) DO UPDATE SET token = $2, updated_at = now()`,
		key, token)
	return err
}

// GetExternalSource returns the token (may be empty — TVmaze's free tier
// needs none) and whether the source is enabled, for internal use by
// fallback code (e.g. fix_runtime). ok is false if the source was never
// configured or is disabled.
func GetExternalSource(ctx context.Context, key string) (token string, ok bool) {
	var t string
	var enabled bool
	err := postgres.Pool.QueryRow(ctx,
		`SELECT token, enabled FROM external_source_tokens WHERE source_key = $1`, key,
	).Scan(&t, &enabled)
	if err != nil || !enabled {
		return "", false
	}
	return t, true
}
