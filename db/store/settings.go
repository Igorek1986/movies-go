package store

import (
	"context"
	"movies-api/db/postgres"
	"strconv"
	"sync"
	"time"
)

// ─── Defaults (mirrors FastAPI settings_cache.py DEFAULTS) ───────────────────

var SettingDefaults = map[string]string{
	// Simple role
	"simple_device_limit":   "1",
	"simple_profile_limit":  "3",
	"simple_timecode_limit": "5000",
	"simple_favorite_limit": "200",
	"simple_import_daily":   "1",
	// Premium role
	"premium_device_limit":   "3",
	"premium_profile_limit":  "5",
	"premium_timecode_limit": "10000",
	"premium_favorite_limit": "500",
	"premium_import_daily":   "3",
	"premium_myshows_daily":  "1",
	"premium_duration_days":  "30",
	// Super role (0 = unlimited)
	"super_device_limit":   "0",
	"super_profile_limit":  "0",
	"super_timecode_limit": "0",
	"super_favorite_limit": "0",
	"super_import_daily":   "0",
	"super_myshows_daily":  "0",
	// Grace period after premium expiry
	"timecode_grace_days":     "3",
	"premium_warn_days":       "3",
	"premium_extend_all_days": "3",
	// Inactive user auto-deletion
	"inactive_delete_days": "180",
	"inactive_warn_days":   "7",
	// Episodes refresh
	"episodes_future_threshold": "5",
	"episodes_refresh_batch":    "10",
	"episodes_refresh_delay":    "2",
	// General
	"watched_threshold":         "90",
	"popular_period_days":       "30",
	"daily_task_hour":           "2",
	"default_timezone":          "Europe/Moscow",
	"session_ttl_days":          "30",
	"session_renew_days":        "15",
	"device_token_ttl_days":     "90",
	"device_code_ttl_minutes":   "10",
	"telegram_link_ttl_minutes": "10",
	"reset_code_ttl_minutes":    "15",
	"pending_2fa_ttl_sec":       "600",
	// Rate limits
	"rate_login_max":           "10",
	"rate_login_window_sec":    "900",
	"rate_register_max":        "5",
	"rate_register_window_sec": "3600",
	"rate_forgot_max":          "3",
	"rate_forgot_window_sec":   "3600",
	"rate_2fa_max":             "5",
	"rate_2fa_window_sec":      "900",
	"sync_cooldown_sec":        "300",
	// Analytics
	"yandex_metrika_enabled":   "0",
	"yandex_metrika_id":        "",
	"google_analytics_enabled": "0",
	"google_analytics_id":      "",
	// TMDB card refresh
	"tmdb_refresh_new_year_delta": "2",
	"tmdb_refresh_old_batch":      "10000",
	"tmdb_refresh_age_days":       "30",
	// Catalog
	"catalog_require_poster": "1",
	"images_via_server":      "0", // раздавать картинки TMDB через сервер (/imgproxy)
	"catalog_actor_count":    "2",
	"catalog_actor_ru_count": "1",
	"catalog_director_count": "3",
	"tracker_new_days":       "90",
	// Parser
	"parser_order":           "rutor,kinozal,nnmclub",
	"catalog_trackers":       "rutor,kinozal,nnmclub",
	"parser_kinozal_enabled": "1",
	"parser_nnmclub_enabled": "1",
	"parser_rutor_enabled":   "1",
	"parser_overlap_days":    "2",
	// Retry / backoff (listing fetches for Kinozal and NNMClub)
	"parser_retry_attempts":      "10",
	"parser_retry_base_wait_sec": "30",
	"parser_retry_max_wait_sec":  "120",
	"parser_retry_ratio":         "2.0",
	// Category parser settings (applied at startup, require restart)
	"movies_new_year_delta":  "2",
	"movies_4k_year_delta":   "4",
	"movies_new_min_quality": "200",
	// Run mode
	"app_mode": "parser",
	// Security
	"banned_patterns": "bylampa",
	// MyShows
	"myshows_api_url":  "https://myshows.me/v3/rpc/",
	"myshows_auth_url": "https://myshows.me/api/session",
	// Site
	"base_url":           "",
	"plugin_url":         "",
	"donate_url":         "",
	"popular_source_url": "",
	// Legal
	"site_name":              "Movies-API",
	"contact_email":          "",
	"privacy_policy_content": "",
	"consent_content":        "",
	// Telegram bot
	"telegram_bot_token":   "",
	"telegram_bot_name":    "",
	"telegram_admin_ids":   "",
	"telegram_use_polling": "0",
}

// ─── Generic key-value settings ───────────────────────────────────────────────

func GetSetting(ctx context.Context, key string) (string, bool) {
	var val string
	err := postgres.Pool.QueryRow(ctx,
		`SELECT value FROM app_settings WHERE key = $1`, key).Scan(&val)
	return val, err == nil
}

func SetSetting(ctx context.Context, key, value string) {
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`INSERT INTO app_settings (key, value, updated_at) VALUES ($1, $2, now())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		key, value)
}

// GetAllSettings returns all DB settings merged with defaults (DB overrides defaults).
func GetAllSettings(ctx context.Context) map[string]string {
	result := make(map[string]string, len(SettingDefaults))
	for k, v := range SettingDefaults {
		result[k] = v
	}
	rows, err := postgres.Pool.Query(ctx, `SELECT key, value FROM app_settings`)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			result[k] = v
		}
	}
	return result
}

func SetSettings(ctx context.Context, kv map[string]string) {
	for k, v := range kv {
		SetSetting(ctx, k, v)
	}
}

// GetSettingInt returns a numeric setting value with a fallback default.
func GetSettingInt(ctx context.Context, key string) int {
	if v, ok := GetSetting(ctx, key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	if def, ok := SettingDefaults[key]; ok {
		if n, err := strconv.Atoi(def); err == nil {
			return n
		}
	}
	return 0
}

// ─── Limits cache ─────────────────────────────────────────────────────────────

const limitsCacheTTL = 30 * time.Second

var (
	limitsCache   map[string]RoleLimits
	limitsCacheMu sync.RWMutex
	limitsCacheAt time.Time
)

// InvalidateLimitsCache forces the next LimitsFor call to reload from DB.
func InvalidateLimitsCache() {
	limitsCacheMu.Lock()
	limitsCache = nil
	limitsCacheMu.Unlock()
}

func intSetting(m map[string]string, key string, def int) int {
	if v, ok := m[key]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return def
}

func loadLimitsFromDB(ctx context.Context) map[string]RoleLimits {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT key, value FROM app_settings
		 WHERE key LIKE 'simple_%' OR key LIKE 'premium_%' OR key LIKE 'super_%'`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	s := make(map[string]string)
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			s[k] = v
		}
	}
	return map[string]RoleLimits{
		"simple": {
			MaxDevices:   intSetting(s, "simple_device_limit", 1),
			MaxProfiles:  intSetting(s, "simple_profile_limit", 3),
			MaxTimecodes: intSetting(s, "simple_timecode_limit", 5000),
			MaxFavorite:  intSetting(s, "simple_favorite_limit", 200),
		},
		"premium": {
			MaxDevices:   intSetting(s, "premium_device_limit", 3),
			MaxProfiles:  intSetting(s, "premium_profile_limit", 5),
			MaxTimecodes: intSetting(s, "premium_timecode_limit", 10000),
			MaxFavorite:  intSetting(s, "premium_favorite_limit", 500),
		},
		"super": {MaxDevices: 0, MaxProfiles: 0, MaxTimecodes: 0, MaxFavorite: 0},
	}
}

// LimitsForDefaults returns the hardcoded default limits (used as fallback).
func LimitsForDefaults(role string) RoleLimits {
	switch role {
	case "premium":
		return RoleLimits{MaxDevices: 3, MaxProfiles: 5, MaxTimecodes: 10000, MaxFavorite: 500}
	case "super":
		return RoleLimits{MaxDevices: 0, MaxProfiles: 0, MaxTimecodes: 0, MaxFavorite: 0}
	default:
		return RoleLimits{MaxDevices: 1, MaxProfiles: 3, MaxTimecodes: 5000, MaxFavorite: 200}
	}
}
