package store

import (
	"context"
	"log"
	"movies-api/db/postgres"
)

// UpcomingEpisode is one entry of UpcomingEpisodes.
type UpcomingEpisode struct {
	CardID      string
	TmdbShowID  int64
	Title       string
	PosterPath  string
	Season      int
	Episode     int
	AirDate     string
	EpisodeName string
}

// UpcomingEpisodes returns episodes airing in [from, to) for TV shows the profile is
// actively watching — same "watching" criterion as UnwatchedTVShows (has at least one
// timecode for the show, capped to the 300 most recently touched). Unlike the
// watched/unwatched logic, this always uses the real air_date, never the
// aired_cutoff-adjusted one — the calendar shows actual release dates, not when a
// torrent is likely to actually appear.
func UpcomingEpisodes(ctx context.Context, deviceID int64, profileID, from, to string) []UpcomingEpisode {
	rows, err := postgres.Pool.Query(ctx, `
		WITH watching AS (
			SELECT tc.card_id, MAX(tc.updated_at) AS last_watched
			FROM timecodes tc
			WHERE tc.device_id = $1 AND tc.profile_id = $2
			GROUP BY tc.card_id
			ORDER BY MAX(tc.updated_at) DESC
			LIMIT 300
		)
		SELECT mc.card_id, mc.tmdb_id, mc.title, COALESCE(mc.poster_path, ''),
		       e.season, e.episode, e.air_date::text, COALESCE(e.title, '')
		FROM watching w
		JOIN media_cards mc ON mc.card_id = w.card_id AND mc.media_type = 'tv'
		JOIN episodes e ON e.tmdb_show_id = mc.tmdb_id AND NOT e.is_special
		WHERE e.air_date IS NOT NULL AND e.air_date >= $3 AND e.air_date < $4
		ORDER BY e.air_date ASC, mc.title ASC`,
		deviceID, profileID, from, to)
	if err != nil {
		log.Printf("store: upcoming episodes: %v", err)
		return nil
	}
	defer rows.Close()
	var out []UpcomingEpisode
	for rows.Next() {
		var e UpcomingEpisode
		if rows.Scan(&e.CardID, &e.TmdbShowID, &e.Title, &e.PosterPath,
			&e.Season, &e.Episode, &e.AirDate, &e.EpisodeName) == nil {
			out = append(out, e)
		}
	}
	return out
}
