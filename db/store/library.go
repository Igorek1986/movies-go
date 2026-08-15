package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"movies-api/db/postgres"
)

type favoriteBlob struct {
	Book []int64 `json:"book"`
	Card []struct {
		ID              int64  `json:"id"`
		MediaType       string `json:"media_type"`
		Name            string `json:"name"`
		FirstAirDate    string `json:"first_air_date"`
		NumberOfSeasons int    `json:"number_of_seasons"`
	} `json:"card"`
}

// ListFavoriteCardIDs returns card_ids from the profile's favorite "book"
// (bookmarks) category, most recently added first. This reads the same blob
// np_profiles.js syncs from Lampa (profiles.favorite) — a single JSON value per
// profile, not the subjective_statuses table. The "book" array only holds tmdb
// ids (no media_type); media_type is resolved from the accompanying "card"
// array in the same blob. Lampa's own card objects (see utils.js's
// card_fields) never carry an explicit "media_type" field at all — only ones
// added through the web UI's toggleFavorite do — so entries synced natively
// from Lampa need the same tv/movie heuristic status.js uses: presence of
// name/first_air_date/number_of_seasons means a TV show.
func ListFavoriteCardIDs(ctx context.Context, deviceID int64, profileID string) []string {
	var raw *string
	err := postgres.Pool.QueryRow(ctx,
		`SELECT favorite FROM profiles WHERE device_id = $1 AND profile_id = $2`,
		deviceID, profileID).Scan(&raw)
	if err != nil || raw == nil {
		return nil
	}
	var blob favoriteBlob
	if json.Unmarshal([]byte(*raw), &blob) != nil {
		return nil
	}
	mediaTypeByID := make(map[int64]string, len(blob.Card))
	for _, c := range blob.Card {
		mt := c.MediaType
		if mt == "" {
			if c.Name != "" || c.FirstAirDate != "" || c.NumberOfSeasons > 0 {
				mt = "tv"
			} else {
				mt = "movie"
			}
		}
		mediaTypeByID[c.ID] = mt
	}
	out := make([]string, 0, len(blob.Book))
	for _, id := range blob.Book {
		mt := mediaTypeByID[id]
		if mt == "" {
			continue
		}
		out = append(out, fmt.Sprintf("%d_%s", id, mt))
	}
	return out
}

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
