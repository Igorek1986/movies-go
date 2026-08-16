package store

import (
	"context"
	"log"
	"movies-api/db/postgres"
)

// Subjective status values. TV shows use Watching/Planned/Stopped/NotWatching;
// movies use Watched/Planned/NotWatching. An empty string means "no explicit
// status" — callers should treat that as NotWatching unless EnsureImpliedStatus
// has since implied one from actual watch activity.
const (
	StatusWatching    = "watching"     // Смотрю (tv)
	StatusPlanned     = "planned"      // Буду смотреть (tv + movie)
	StatusStopped     = "stopped"      // Перестал смотреть (tv)
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
	// "Непросмотренные" (its "watching" gate) — e.g. "Перестал смотреть" must
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

// EnsureImpliedStatus re-derives status on every real watch event — "watching" for
// a TV show once its percent crosses watching_threshold (default 0 — any timecode
// at all, matching the original behavior), "watched" for a movie once its percent
// crosses watched_threshold. Real watch activity always wins, INCLUDING over an
// explicit choice ("не смотрю"/"брошено") — a status set a while ago is weaker
// evidence than "just actually watched an episode". Called from live timecode
// writes only (not from BackfillImpliedStatuses, which stays ON CONFLICT DO
// NOTHING — it replays historical data in bulk on every startup and must not
// stomp on statuses the user set after that old activity happened).
func EnsureImpliedStatus(ctx context.Context, deviceID int64, profileID, cardID string, percent float64) {
	watchedThreshold := GetSettingInt(ctx, "watched_threshold")
	watchingThreshold := GetSettingInt(ctx, "watching_threshold")
	_, err := postgres.Pool.Exec(ctx, `
		INSERT INTO subjective_statuses (device_id, profile_id, card_id, status)
		SELECT $1, $2, $3::varchar, CASE WHEN mc.media_type = 'movie' THEN 'watched' ELSE 'watching' END
		FROM media_cards mc
		WHERE mc.card_id = $3::varchar
		  AND (
		        (mc.media_type = 'movie' AND $4::float8 >= $5::int)
		     OR (mc.media_type != 'movie' AND $4::float8 >= $6::int)
		      )
		ON CONFLICT (device_id, profile_id, card_id) DO UPDATE
		SET status = EXCLUDED.status, updated_at = now()`,
		deviceID, profileID, cardID, percent, watchedThreshold, watchingThreshold)
	if err != nil {
		log.Printf("store: ensure implied status: %v", err)
	}
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
