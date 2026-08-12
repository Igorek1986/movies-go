package store

import (
	"context"
	"log"
	"movies-api/db/postgres"
)

// UnwatchedTVShow is one entry of UnwatchedTVShows: a TV show the profile is
// actively watching, with its unwatched-episode progress.
type UnwatchedTVShow struct {
	CardID         string
	AiredCount     int  // aired non-special episodes
	WatchedCount   int  // of those, watched (>= percent, or marked special)
	NextSeason     *int // season of the earliest unwatched aired episode
	NextEpisodeNum *int // episode number of the earliest unwatched aired episode
}

// UnwatchedTVShows returns TV shows the profile is actively watching (at least one
// watched aired episode) that still have an aired episode not yet watched — the local
// equivalent of MyShows' "Непросмотренные" list. Ordered by last watched episode, most
// recently watched first.
func UnwatchedTVShows(ctx context.Context, deviceID int64, profileID string, percent int) []UnwatchedTVShow {
	if percent < 1 {
		percent = 90
	}
	rows, err := postgres.Pool.Query(ctx, `
		WITH watched_hashes AS (
			SELECT tc.item AS hash
			FROM timecodes tc
			WHERE tc.device_id = $1 AND tc.profile_id = $2
			  AND ((tc.data::jsonb->>'percent')::numeric >= $3
			       OR (tc.data::jsonb->>'special')::boolean IS TRUE)
		),
		watching AS (
			SELECT tc.card_id, MAX(tc.updated_at) AS last_watched
			FROM timecodes tc
			WHERE tc.device_id = $1 AND tc.profile_id = $2
			GROUP BY tc.card_id
		)
		SELECT mc.card_id, c.aired, c.watched, ne.season, ne.episode
		FROM watching w
		JOIN media_cards mc ON mc.card_id = w.card_id AND mc.media_type = 'tv'
		JOIN LATERAL (
			SELECT COUNT(*) FILTER (
					WHERE e.air_date IS NOT NULL AND e.air_date <= CURRENT_DATE
			       ) AS aired,
			       COUNT(*) FILTER (
					WHERE e.air_date IS NOT NULL AND e.air_date <= CURRENT_DATE
					  AND EXISTS (SELECT 1 FROM watched_hashes wh WHERE wh.hash = e.hash)
			       ) AS watched
			FROM episodes e
			WHERE e.tmdb_show_id = mc.tmdb_id AND NOT e.is_special
		) c ON true
		LEFT JOIN LATERAL (
			SELECT e.season, e.episode
			FROM episodes e
			WHERE e.tmdb_show_id = mc.tmdb_id AND NOT e.is_special
			  AND e.air_date IS NOT NULL AND e.air_date <= CURRENT_DATE
			  AND NOT EXISTS (SELECT 1 FROM watched_hashes wh WHERE wh.hash = e.hash)
			ORDER BY e.air_date ASC, e.season ASC, e.episode ASC
			LIMIT 1
		) ne ON true
		WHERE c.watched >= 1 AND c.watched < c.aired
		ORDER BY w.last_watched DESC`,
		deviceID, profileID, percent)
	if err != nil {
		log.Printf("store: unwatched tv shows: %v", err)
		return nil
	}
	defer rows.Close()
	var out []UnwatchedTVShow
	for rows.Next() {
		var s UnwatchedTVShow
		if err := rows.Scan(&s.CardID, &s.AiredCount, &s.WatchedCount, &s.NextSeason, &s.NextEpisodeNum); err != nil {
			continue
		}
		out = append(out, s)
	}
	return out
}
