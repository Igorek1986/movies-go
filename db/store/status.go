package store

import (
	"context"
	"log"
	"movies-api/db/postgres"
	"strconv"
	"strings"
)

// Subjective status values. TV shows use Watching/Planned/Stopped/NotWatching;
// movies use Watched/Planned/Stopped/NotWatching. An empty string means "no
// explicit status" — callers should treat that as NotWatching unless
// EnsureImpliedStatus has since implied one from actual watch activity.
const (
	StatusWatching    = "watching"     // Смотрю (tv)
	StatusPlanned     = "planned"      // Буду смотреть (tv + movie)
	StatusStopped     = "stopped"      // Брошено (tv + movie)
	StatusWatched     = "watched"      // Просмотрел (movie)
	StatusNotWatching = "not_watching" // Не смотрю (tv + movie) — explicit opt-out
)

// SetSubjectiveStatus sets an explicit status, overwriting whatever was there
// before (implied or explicit) — an explicit user choice always wins.
func SetSubjectiveStatus(ctx context.Context, deviceID int64, profileID, cardID, status string) error {
	_, err := postgres.Pool.Exec(ctx, `
		INSERT INTO subjective_statuses (device_id, profile_id, card_id, status)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (device_id, profile_id, card_id) DO UPDATE
		SET status = EXCLUDED.status, updated_at = now()`,
		deviceID, profileID, cardID, status)
	if err != nil {
		return err
	}
	// A status change alone (no timecode write) can move a show in/out of
	// "Непросмотренные" (its "watching" gate) — e.g. "Брошено" must
	// drop it immediately, not just on the next timecode-driven invalidation.
	notifyWatchedChanged(deviceID, profileID)
	return nil
}

// ClearSubjectiveStatus removes the status row entirely, going back to "no
// explicit status" (so EnsureImpliedStatus is free to imply one again on the
// next timecode).
func ClearSubjectiveStatus(ctx context.Context, deviceID int64, profileID, cardID string) {
	if _, err := postgres.Pool.Exec(ctx,
		`DELETE FROM subjective_statuses WHERE device_id = $1 AND profile_id = $2 AND card_id = $3`,
		deviceID, profileID, cardID); err != nil {
		log.Printf("store: clear subjective status: %v", err)
		return
	}
	notifyWatchedChanged(deviceID, profileID)
}

// GetSubjectiveStatus returns the explicit status for one card, or "" if none is set.
func GetSubjectiveStatus(ctx context.Context, deviceID int64, profileID, cardID string) string {
	var status string
	err := postgres.Pool.QueryRow(ctx,
		`SELECT status FROM subjective_statuses WHERE device_id = $1 AND profile_id = $2 AND card_id = $3`,
		deviceID, profileID, cardID).Scan(&status)
	if err != nil {
		return ""
	}
	return status
}

// GetSubjectiveStatuses bulk-loads statuses for a set of cards — avoids N+1 when
// rendering a catalog page/row.
func GetSubjectiveStatuses(ctx context.Context, deviceID int64, profileID string, cardIDs []string) map[string]string {
	out := make(map[string]string, len(cardIDs))
	if len(cardIDs) == 0 {
		return out
	}
	rows, err := postgres.Pool.Query(ctx,
		`SELECT card_id, status FROM subjective_statuses
		 WHERE device_id = $1 AND profile_id = $2 AND card_id = ANY($3)`,
		deviceID, profileID, cardIDs)
	if err != nil {
		log.Printf("store: get subjective statuses: %v", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var cardID, status string
		if rows.Scan(&cardID, &status) == nil {
			out[cardID] = status
		}
	}
	return out
}

// ListCardIDsByStatus returns card_ids with the given explicit status — the backend
// for "Моё" tabs (Смотрю/Буду смотреть/Бросил). Does NOT include
// implied-but-unwritten statuses; EnsureImpliedStatus/BackfillImpliedStatuses keep
// the table caught up so this stays accurate.
func ListCardIDsByStatus(ctx context.Context, deviceID int64, profileID, status string) []string {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT card_id FROM subjective_statuses
		 WHERE device_id = $1 AND profile_id = $2 AND status = $3
		 ORDER BY updated_at DESC`,
		deviceID, profileID, status)
	if err != nil {
		log.Printf("store: list card ids by status: %v", err)
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var cardID string
		if rows.Scan(&cardID) == nil {
			out = append(out, cardID)
		}
	}
	return out
}

// watchingThresholdOverride looks up a per-profile override for watching_threshold
// from plugin_settings (plugin "np_unwatched", written client-side by
// np_unwatched.js via window.__NMSync — see its setProfileSetting/getProfileKey) —
// mirrors myshows.js's per-profile myshows_add_threshold: different profiles on
// the same server may want different sensitivity for "does watching an episode
// count as actually watching the show". ok is false if the profile never set one,
// so callers fall back to the deployment-wide watching_threshold setting.
//
// profile_id in plugin_settings is stored WITHOUT a leading "_" (np_profiles.js's
// __NMSync strips it before sending), while subjective_statuses/timecodes keep the
// raw Lampa profile id (which sometimes does start with "_") — the same profile
// otherwise looks like two different ones across these tables unless normalized here.
func watchingThresholdOverride(ctx context.Context, deviceID int64, profileID string) (int, bool) {
	var userID int64
	if err := postgres.Pool.QueryRow(ctx, `SELECT user_id FROM devices WHERE id = $1`, deviceID).Scan(&userID); err != nil {
		return 0, false
	}
	settings := GetPluginSettings(ctx, userID, strings.TrimPrefix(profileID, "_"), "np_unwatched")
	key := "np_unwatched_watching_threshold"
	if profileID != "" {
		key += "_profile_" + strings.TrimPrefix(profileID, "_")
	}
	v, ok := settings[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case string:
		n, err := strconv.Atoi(t)
		if err != nil {
			return 0, false
		}
		return n, true
	case float64:
		return int(t), true
	default:
		return 0, false
	}
}

// EnsureImpliedStatus re-derives status on every real watch event — "watching" for
// a TV show once its percent crosses watching_threshold (per-profile override if
// set, else the deployment-wide default — see watchingThresholdOverride; default 0
// means any timecode at all, matching the original behavior), "watched" for a
// movie once its percent crosses watched_threshold. Real watch activity always
// wins, INCLUDING over an explicit choice ("не смотрю"/"брошено") — a status set a
// while ago is weaker evidence than "just actually watched an episode". Called
// from live timecode writes only (not from BackfillImpliedStatuses, which stays ON
// CONFLICT DO NOTHING — it replays historical data in bulk on every startup and
// must not stomp on statuses the user set after that old activity happened).
func EnsureImpliedStatus(ctx context.Context, deviceID int64, profileID, cardID string, percent float64) {
	watchedThreshold := GetSettingInt(ctx, "watched_threshold")
	watchingThreshold := GetSettingInt(ctx, "watching_threshold")
	if override, ok := watchingThresholdOverride(ctx, deviceID, profileID); ok {
		watchingThreshold = override
	}
	// WHERE on the conflict update + RETURNING — the only way to tell whether
	// this call actually CHANGED the status (fresh insert, or a real
	// transition) versus just re-confirming a status already set (every
	// subsequent timecode save past the threshold for the same episode would
	// otherwise match too). Only a real change is worth notifying about —
	// physically watching something implies a status exactly like clicking
	// the status button does, but unlike that button click, this path had no
	// broadcast at all (see notifyStatusChanged below).
	var newStatus string
	err := postgres.Pool.QueryRow(ctx, `
		INSERT INTO subjective_statuses (device_id, profile_id, card_id, status)
		SELECT $1, $2, $3::varchar, CASE WHEN mc.media_type = 'movie' THEN 'watched' ELSE 'watching' END
		FROM media_cards mc
		WHERE mc.card_id = $3::varchar
		  AND (
		        (mc.media_type = 'movie' AND $4::float8 >= $5::int)
		     OR (mc.media_type != 'movie' AND $4::float8 >= $6::int)
		      )
		ON CONFLICT (device_id, profile_id, card_id) DO UPDATE
		SET status = EXCLUDED.status, updated_at = now()
		WHERE subjective_statuses.status IS DISTINCT FROM EXCLUDED.status
		RETURNING status`,
		deviceID, profileID, cardID, percent, watchedThreshold, watchingThreshold,
	).Scan(&newStatus)
	if err != nil {
		return // no row returned = status didn't actually change (the common case) — not an error
	}
	notifyStatusChanged(ctx, deviceID, profileID, cardID, newStatus)
}

// OnStatusChanged is called whenever EnsureImpliedStatus actually changes a
// card's status by real watch activity (crossing watching/watched_threshold) —
// wired in cmd/main.go to broadcast it over WS, the same way the explicit
// set-status HTTP handlers already do for a manual status-button click. Not
// used by SetSubjectiveStatus/ClearSubjectiveStatus — those already broadcast
// at their own API-handler call sites (they know the request's client_id for
// self-echo exclusion, which this store-layer path doesn't have).
var OnStatusChanged func(userID, deviceID int64, profileID, cardID, status string)

func notifyStatusChanged(ctx context.Context, deviceID int64, profileID, cardID, status string) {
	if OnStatusChanged == nil {
		return
	}
	var userID int64
	if err := postgres.Pool.QueryRow(ctx, `SELECT user_id FROM devices WHERE id = $1`, deviceID).Scan(&userID); err != nil {
		return
	}
	OnStatusChanged(userID, deviceID, profileID, cardID, status)
}

// BackfillImpliedStatuses is EnsureImpliedStatus applied in bulk across every
// device/profile/card that already has timecodes — run once at startup to catch up
// profiles that were watching shows before this feature existed. Safe to call on
// every startup: it only inserts rows that don't exist yet.
func BackfillImpliedStatuses(ctx context.Context) {
	watchedThreshold := GetSettingInt(ctx, "watched_threshold")
	watchingThreshold := GetSettingInt(ctx, "watching_threshold")
	tag, err := postgres.Pool.Exec(ctx, `
		INSERT INTO subjective_statuses (device_id, profile_id, card_id, status)
		SELECT DISTINCT tc.device_id, tc.profile_id, tc.card_id,
		       CASE WHEN mc.media_type = 'movie' THEN 'watched' ELSE 'watching' END
		FROM timecodes tc
		JOIN media_cards mc ON mc.card_id = tc.card_id
		WHERE (mc.media_type = 'movie' AND (tc.data::jsonb->>'percent')::numeric >= $1::int)
		   OR (mc.media_type != 'movie' AND (tc.data::jsonb->>'percent')::numeric >= $2::int)
		ON CONFLICT (device_id, profile_id, card_id) DO NOTHING`,
		watchedThreshold, watchingThreshold)
	if err != nil {
		log.Printf("store: backfill implied statuses: %v", err)
		return
	}
	if tag.RowsAffected() > 0 {
		log.Printf("store: backfill implied statuses: inserted %d rows", tag.RowsAffected())
	}
}
