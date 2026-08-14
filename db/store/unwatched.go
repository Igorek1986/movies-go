package store

import (
	"context"
	"fmt"
	"log"
	"movies-api/db/postgres"
)

// AiredCutoffDate returns a SQL date expression for deciding whether an episode
// counts as "aired". By default it's just CURRENT_DATE, but the admin settings
// aired_cutoff_days/aired_cutoff_hour let deployments delay that past midnight of
// air_date — TMDB's air_date is date-only, and subtitles/torrents for a show
// airing "today" often aren't actually available until later, sometimes the next
// day. Safe to interpolate directly: both values always come from admin-only
// settings, never from user input.
func AiredCutoffDate(ctx context.Context) string {
	d := GetSettingInt(ctx, "aired_cutoff_days")
	h := GetSettingInt(ctx, "aired_cutoff_hour")
	if d <= 0 && h <= 0 {
		return "CURRENT_DATE"
	}
	return fmt.Sprintf("(now() - interval '%d days' - interval '%d hours')::date", d, h)
}

// UnwatchedTVShow is one entry of UnwatchedTVShows: a TV show the profile is
// actively watching, with its unwatched-episode progress.
type UnwatchedTVShow struct {
	CardID         string
	AiredCount     int  // aired non-special episodes
	WatchedCount   int  // of those, watched (>= percent, or marked special)
	NextSeason     *int // season of the earliest unwatched aired episode
	NextEpisodeNum *int // episode number of the earliest unwatched aired episode
}

// unwatchedOrderBy maps a client-facing sort key to a literal ORDER BY expression —
// same options as myshows.js's myshows_sort_order. Never interpolate the raw sort
// param directly into SQL; only values from this whitelist are used.
func unwatchedOrderBy(sortOrder string) string {
	switch sortOrder {
	case "unwatched_count":
		return "(c.aired - c.watched) DESC, w.last_watched DESC"
	case "air_date":
		return "la.last_air_date DESC NULLS LAST, w.last_watched DESC"
	case "air_date_asc":
		return "la.last_air_date ASC NULLS LAST, w.last_watched DESC"
	case "first_unwatched_date":
		return "ne.air_date DESC NULLS LAST, w.last_watched DESC"
	case "first_unwatched_date_asc":
		return "ne.air_date ASC NULLS LAST, w.last_watched DESC"
	case "alphabet":
		return "mc.title ASC"
	case "progress":
		return "(c.watched::float / NULLIF(c.aired, 0)) DESC, w.last_watched DESC"
	default:
		return "w.last_watched DESC"
	}
}

// UnwatchedTVShows returns TV shows the profile is actively watching (at least one
// watched aired episode) that still have an aired episode not yet watched — the local
// equivalent of MyShows' "Непросмотренные" list. sortOrder mirrors myshows.js's
// myshows_sort_order values (empty/unknown = most recently watched first).
func UnwatchedTVShows(ctx context.Context, deviceID int64, profileID string, percent int, sortOrder string) []UnwatchedTVShow {
	if percent < 1 {
		percent = 90
	}
	cutoff := AiredCutoffDate(ctx)
	//nolint:gosec // unwatchedOrderBy only returns literals from a fixed whitelist; cutoff comes from AiredCutoffDate (admin setting only)
	sql := `
		WITH watched_hashes AS (
			SELECT tc.item AS hash
			FROM timecodes tc
			WHERE tc.device_id = $1 AND tc.profile_id = $2
			  AND ((tc.data::jsonb->>'percent')::numeric >= $3
			       OR (tc.data::jsonb->>'special')::boolean IS TRUE)
		),
		watching AS (
			-- Cap candidates by recency before the per-show episode LATERAL below —
			-- bounds query cost regardless of how many shows the profile has ever
			-- touched over the years (only the count, not correctness, changes: a
			-- show untouched in ages behind 300 more-recent ones wouldn't realistically
			-- still be "actively watching" anyway).
			SELECT tc.card_id, MAX(tc.updated_at) AS last_watched
			FROM timecodes tc
			WHERE tc.device_id = $1 AND tc.profile_id = $2
			GROUP BY tc.card_id
			ORDER BY MAX(tc.updated_at) DESC
			LIMIT 300
		)
		SELECT mc.card_id, c.aired, c.watched, ne.season, ne.episode
		FROM watching w
		JOIN media_cards mc ON mc.card_id = w.card_id AND mc.media_type = 'tv'
		JOIN LATERAL (
			SELECT COUNT(*) FILTER (
					WHERE e.air_date IS NOT NULL AND e.air_date <= ` + cutoff + `
			       ) AS aired,
			       COUNT(*) FILTER (
					WHERE e.air_date IS NOT NULL AND e.air_date <= ` + cutoff + `
					  AND EXISTS (SELECT 1 FROM watched_hashes wh WHERE wh.hash = e.hash)
			       ) AS watched
			FROM episodes e
			WHERE e.tmdb_show_id = mc.tmdb_id AND NOT e.is_special
		) c ON true
		LEFT JOIN LATERAL (
			SELECT e.season, e.episode, e.air_date
			FROM episodes e
			WHERE e.tmdb_show_id = mc.tmdb_id AND NOT e.is_special
			  AND e.air_date IS NOT NULL AND e.air_date <= ` + cutoff + `
			  AND NOT EXISTS (SELECT 1 FROM watched_hashes wh WHERE wh.hash = e.hash)
			ORDER BY e.air_date ASC, e.season ASC, e.episode ASC
			LIMIT 1
		) ne ON true
		LEFT JOIN LATERAL (
			SELECT MAX(e.air_date) AS last_air_date
			FROM episodes e
			WHERE e.tmdb_show_id = mc.tmdb_id AND NOT e.is_special
			  AND e.air_date IS NOT NULL AND e.air_date <= ` + cutoff + `
		) la ON true
		WHERE c.watched >= 1 AND c.watched < c.aired
		ORDER BY ` + unwatchedOrderBy(sortOrder)
	rows, err := postgres.Pool.Query(ctx, sql, deviceID, profileID, percent)
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

// UnwatchedTVShowProgress returns the unwatched-episode progress for a single TV show
// card (device+profile scoped) — the cheap, targeted counterpart to UnwatchedTVShows,
// meant to be called right after a single timecode is confirmed instead of refetching
// the whole "Непросмотренные" list. ok is false if the card has no aired episodes at
// all (e.g. unknown show or not a TV card).
func UnwatchedTVShowProgress(ctx context.Context, deviceID int64, profileID, cardID string, percent int) (show UnwatchedTVShow, ok bool) {
	if percent < 1 {
		percent = 90
	}
	show.CardID = cardID
	cutoff := AiredCutoffDate(ctx)
	err := postgres.Pool.QueryRow(ctx, `
		WITH watched_hashes AS (
			SELECT tc.item AS hash
			FROM timecodes tc
			WHERE tc.device_id = $1 AND tc.profile_id = $2
			  AND ((tc.data::jsonb->>'percent')::numeric >= $4
			       OR (tc.data::jsonb->>'special')::boolean IS TRUE)
		)
		SELECT c.aired, c.watched, ne.season, ne.episode
		FROM media_cards mc
		JOIN LATERAL (
			SELECT COUNT(*) FILTER (
					WHERE e.air_date IS NOT NULL AND e.air_date <= `+cutoff+`
			       ) AS aired,
			       COUNT(*) FILTER (
					WHERE e.air_date IS NOT NULL AND e.air_date <= `+cutoff+`
					  AND EXISTS (SELECT 1 FROM watched_hashes wh WHERE wh.hash = e.hash)
			       ) AS watched
			FROM episodes e
			WHERE e.tmdb_show_id = mc.tmdb_id AND NOT e.is_special
		) c ON true
		LEFT JOIN LATERAL (
			SELECT e.season, e.episode
			FROM episodes e
			WHERE e.tmdb_show_id = mc.tmdb_id AND NOT e.is_special
			  AND e.air_date IS NOT NULL AND e.air_date <= `+cutoff+`
			  AND NOT EXISTS (SELECT 1 FROM watched_hashes wh WHERE wh.hash = e.hash)
			ORDER BY e.air_date ASC, e.season ASC, e.episode ASC
			LIMIT 1
		) ne ON true
		WHERE mc.card_id = $3 AND mc.media_type = 'tv'`,
		deviceID, profileID, cardID, percent,
	).Scan(&show.AiredCount, &show.WatchedCount, &show.NextSeason, &show.NextEpisodeNum)
	if err != nil {
		return show, false
	}
	return show, show.AiredCount > 0
}

// WatchedEpisode is one entry of WatchedEpisodes.
type WatchedEpisode struct {
	Hash    string
	Season  int
	Episode int
}

// WatchedEpisodes returns the episodes of one TV show (device+profile scoped) that are
// watched (>= percent, or marked special) — used to put a "watched" checkmark on
// individual episode cards (season/episode picker, Explorer lists), mirroring
// myshows.js's per-episode checkmark feature.
func WatchedEpisodes(ctx context.Context, deviceID int64, profileID, cardID string, percent int) []WatchedEpisode {
	if percent < 1 {
		percent = 90
	}
	rows, err := postgres.Pool.Query(ctx, `
		SELECT e.hash, e.season, e.episode
		FROM media_cards mc
		JOIN episodes e ON e.tmdb_show_id = mc.tmdb_id AND NOT e.is_special
		JOIN timecodes tc ON tc.device_id = $1 AND tc.profile_id = $2 AND tc.item = e.hash
		WHERE mc.card_id = $3 AND mc.media_type = 'tv'
		  AND ((tc.data::jsonb->>'percent')::numeric >= $4
		       OR (tc.data::jsonb->>'special')::boolean IS TRUE)`,
		deviceID, profileID, cardID, percent)
	if err != nil {
		log.Printf("store: watched episodes: %v", err)
		return nil
	}
	defer rows.Close()
	var out []WatchedEpisode
	for rows.Next() {
		var e WatchedEpisode
		if rows.Scan(&e.Hash, &e.Season, &e.Episode) == nil {
			out = append(out, e)
		}
	}
	return out
}
