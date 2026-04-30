package store

import (
	"context"
	"fmt"
	"lampa-api/db/postgres"
	"time"
)

// EpisodeRow represents one row from the episodes table.
type EpisodeRow struct {
	MyshowsEpID int
	Season      int16
	Episode     int16
	Title       *string
	DurationSec *int
	IsSpecial   bool
	Hash        string
	AirDate     *time.Time
}

// MediaCardEpInfo holds data needed to drive MyShows episode sync.
type MediaCardEpInfo struct {
	CardID           string
	TmdbID           int64
	OriginalTitle    string
	Title            string     // localized (Russian) title from TMDB
	Year             string     // "2020" from first_air_date or release_date
	ImdbID           *string
	MyshowsID        *int       // myshows show_id (used to fetch episodes)
	EpisodesSyncedAt *time.Time
	NextEpAirDate    *string
	EpisodeRunTime   *int
	Status           *string
}

// GetMediaCardEpInfo returns sync-relevant fields for a card. Returns nil if not found.
func GetMediaCardEpInfo(ctx context.Context, cardID string) *MediaCardEpInfo {
	var mc MediaCardEpInfo
	err := postgres.Pool.QueryRow(ctx, `
		SELECT card_id, tmdb_id, COALESCE(original_title,''), COALESCE(title,''), imdb_id,
		       myshows_id, episodes_synced_at, next_ep_air_date, episode_run_time, status,
		       COALESCE(LEFT(COALESCE(first_air_date::text, release_date::text, ''), 4), '')
		FROM media_cards WHERE card_id = $1`, cardID,
	).Scan(
		&mc.CardID, &mc.TmdbID, &mc.OriginalTitle, &mc.Title, &mc.ImdbID,
		&mc.MyshowsID, &mc.EpisodesSyncedAt, &mc.NextEpAirDate, &mc.EpisodeRunTime, &mc.Status,
		&mc.Year,
	)
	if err != nil {
		return nil
	}
	return &mc
}

// SetMyshowsID persists the myshows_id for a media card.
func SetMyshowsID(ctx context.Context, cardID string, myshowsID int) error {
	_, err := postgres.Pool.Exec(ctx,
		`UPDATE media_cards SET myshows_id = $1 WHERE card_id = $2`, myshowsID, cardID)
	return err
}

// GetEpisodes returns all episodes for a TMDB show ordered by season, episode.
func GetEpisodes(ctx context.Context, tmdbShowID int64) []EpisodeRow {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT season, episode, title, duration_sec, is_special, COALESCE(hash,''), air_date
		FROM episodes WHERE tmdb_show_id = $1
		ORDER BY season, episode`, int32(tmdbShowID))
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []EpisodeRow
	for rows.Next() {
		var ep EpisodeRow
		if err := rows.Scan(
			&ep.Season, &ep.Episode, &ep.Title, &ep.DurationSec,
			&ep.IsSpecial, &ep.Hash, &ep.AirDate,
		); err == nil {
			result = append(result, ep)
		}
	}
	return result
}

// UpsertEpisodes replaces all episodes for a show with a fresh set from MyShows.
func UpsertEpisodes(ctx context.Context, tmdbShowID int64, eps []EpisodeRow) error {
	tx, err := postgres.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx,
		`DELETE FROM episodes WHERE tmdb_show_id = $1`, tmdbShowID,
	); err != nil {
		return err
	}

	for _, ep := range eps {
		var msEpID *int
		if ep.MyshowsEpID > 0 {
			msEpID = &ep.MyshowsEpID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO episodes (tmdb_show_id, season, episode, title, duration_sec, is_special, hash, air_date, myshows_ep_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			tmdbShowID, ep.Season, ep.Episode, ep.Title, ep.DurationSec,
			ep.IsSpecial, ep.Hash, ep.AirDate, msEpID,
		); err != nil {
			return fmt.Errorf("insert episode s%de%d: %w", ep.Season, ep.Episode, err)
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE media_cards SET episodes_synced_at = now() WHERE tmdb_id = $1 AND media_type = 'tv'`,
		tmdbShowID,
	); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// EpisodeCount returns the number of episodes stored for a show.
func EpisodeCount(ctx context.Context, tmdbShowID int64) int {
	var n int
	postgres.Pool.QueryRow(ctx, //nolint:errcheck
		`SELECT COUNT(*) FROM episodes WHERE tmdb_show_id = $1`, tmdbShowID,
	).Scan(&n)
	return n
}
