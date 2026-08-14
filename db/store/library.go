package store

import (
	"context"
	"log"
	"movies-api/db/postgres"
)

// ListWatchingCardIDs returns TV shows with subjective status "watching" that the
// profile is still actively following — excludes shows that have finished airing
// (TMDB status "Ended") AND been fully watched; those belong in
// ListCompletedCardIDs instead ("Просмотрел"), not "Смотрю". A currently-airing
// show that's fully caught up stays here (there's still a next episode pending).
// Sorted by last watched date, most recent first.
func ListWatchingCardIDs(ctx context.Context, deviceID int64, profileID string, percent int) []string {
	if percent < 1 {
		percent = 90
	}
	cutoff := AiredCutoffDate(ctx)
	//nolint:gosec // cutoff comes from AiredCutoffDate (admin setting only), not user input
	rows, err := postgres.Pool.Query(ctx, `
		WITH watched_hashes AS (
			SELECT tc.item AS hash
			FROM timecodes tc
			WHERE tc.device_id = $1 AND tc.profile_id = $2
			  AND ((tc.data::jsonb->>'percent')::numeric >= $3
			       OR (tc.data::jsonb->>'special')::boolean IS TRUE)
		),
		last_watch AS (
			SELECT card_id, MAX(updated_at) AS last_watched
			FROM timecodes WHERE device_id = $1 AND profile_id = $2
			GROUP BY card_id
		)
		SELECT mc.card_id
		FROM subjective_statuses ss
		JOIN media_cards mc ON mc.card_id = ss.card_id AND mc.media_type = 'tv'
		LEFT JOIN last_watch lw ON lw.card_id = mc.card_id
		WHERE ss.device_id = $1 AND ss.profile_id = $2 AND ss.status = 'watching'
		  AND NOT (
			mc.status = 'Ended'
			AND (SELECT COUNT(*) FROM episodes e
			     WHERE e.tmdb_show_id = mc.tmdb_id AND NOT e.is_special
			       AND EXISTS (SELECT 1 FROM watched_hashes wh WHERE wh.hash = e.hash))
			    >= GREATEST(1, COALESCE(
			         (SELECT COUNT(*) FROM episodes e2
			          WHERE e2.tmdb_show_id = mc.tmdb_id AND NOT e2.is_special
			            AND e2.air_date IS NOT NULL AND e2.air_date <= `+cutoff+`),
			         mc.number_of_episodes))
		  )
		ORDER BY COALESCE(lw.last_watched, ss.updated_at) DESC`,
		deviceID, profileID, percent)
	if err != nil {
		log.Printf("store: list watching card ids: %v", err)
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// ListCompletedCardIDs returns "Просмотрел": TV shows that have finished airing
// and been fully watched (moved here from ListWatchingCardIDs) plus every movie
// with subjective status "watched". Sorted by last watched date, most recent first.
func ListCompletedCardIDs(ctx context.Context, deviceID int64, profileID string, percent int) []string {
	if percent < 1 {
		percent = 90
	}
	cutoff := AiredCutoffDate(ctx)
	//nolint:gosec // cutoff comes from AiredCutoffDate (admin setting only), not user input
	rows, err := postgres.Pool.Query(ctx, `
		WITH watched_hashes AS (
			SELECT tc.item AS hash
			FROM timecodes tc
			WHERE tc.device_id = $1 AND tc.profile_id = $2
			  AND ((tc.data::jsonb->>'percent')::numeric >= $3
			       OR (tc.data::jsonb->>'special')::boolean IS TRUE)
		),
		last_watch AS (
			SELECT card_id, MAX(updated_at) AS last_watched
			FROM timecodes WHERE device_id = $1 AND profile_id = $2
			GROUP BY card_id
		),
		completed AS (
			SELECT mc.card_id, COALESCE(lw.last_watched, ss.updated_at) AS activity
			FROM subjective_statuses ss
			JOIN media_cards mc ON mc.card_id = ss.card_id AND mc.media_type = 'tv'
			LEFT JOIN last_watch lw ON lw.card_id = mc.card_id
			WHERE ss.device_id = $1 AND ss.profile_id = $2 AND ss.status = 'watching'
			  AND mc.status = 'Ended'
			  AND (SELECT COUNT(*) FROM episodes e
			       WHERE e.tmdb_show_id = mc.tmdb_id AND NOT e.is_special
			         AND EXISTS (SELECT 1 FROM watched_hashes wh WHERE wh.hash = e.hash))
			      >= GREATEST(1, COALESCE(
			           (SELECT COUNT(*) FROM episodes e2
			            WHERE e2.tmdb_show_id = mc.tmdb_id AND NOT e2.is_special
			              AND e2.air_date IS NOT NULL AND e2.air_date <= `+cutoff+`),
			           mc.number_of_episodes))

			UNION ALL

			SELECT mc.card_id, COALESCE(lw.last_watched, ss.updated_at) AS activity
			FROM subjective_statuses ss
			JOIN media_cards mc ON mc.card_id = ss.card_id AND mc.media_type = 'movie'
			LEFT JOIN last_watch lw ON lw.card_id = mc.card_id
			WHERE ss.device_id = $1 AND ss.profile_id = $2 AND ss.status = 'watched'
		)
		SELECT card_id FROM completed ORDER BY activity DESC`,
		deviceID, profileID, percent)
	if err != nil {
		log.Printf("store: list completed card ids: %v", err)
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}
