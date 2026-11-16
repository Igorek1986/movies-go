package store

import (
	"context"
	"fmt"
	"movies-api/db/postgres"
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
	// StillPath: nil = never checked against TMDB yet, "" = checked, TMDB
	// has none, non-empty = a bare TMDB path. See SetEpisodeSeasonInfo.
	StillPath *string
	// Overview: same nil/""/non-empty sentinel as StillPath — backfilled in
	// the same TMDB call (the season endpoint returns both per episode).
	Overview *string
}

// EpisodeSeasonInfo is what TMDB's /tv/{id}/season/{n} returns per episode
// that we care about — defined here (not in movies/tmdb) so that package can
// import db/store (it already does, for other TMDB→DB writes) without a
// cycle.
type EpisodeSeasonInfo struct {
	StillPath string
	Overview  string
}

// MediaCardEpInfo holds data needed to drive MyShows episode sync.
type MediaCardEpInfo struct {
	CardID           string
	TmdbID           int64
	OriginalTitle    string
	Title            string // localized (Russian) title from TMDB
	Year             string // "2020" from first_air_date or release_date
	ImdbID           *string
	MyshowsID        *int // myshows show_id (used to fetch episodes)
	EpisodesSyncedAt *time.Time
	Tracked          bool
	EpisodeRunTime   *int
	Status           *string
}

// episodeCardFields uses only a tracking flag; no user data is sent to MyShows.
const episodeCardFields = `mc.card_id, mc.tmdb_id, COALESCE(mc.original_title,''),
 COALESCE(mc.title,''), mc.imdb_id, mc.myshows_id, mc.episodes_synced_at,
 mc.episode_run_time, mc.status,
 COALESCE(LEFT(COALESCE(mc.first_air_date::text, mc.release_date::text, ''), 4), ''),
 (EXISTS (SELECT 1 FROM timecodes t WHERE t.card_id = mc.card_id)
  OR EXISTS (SELECT 1 FROM subjective_statuses ss
             WHERE ss.card_id = mc.card_id AND ss.status = 'watching'))`

func scanEpisodeCard(scan func(...any) error) (*MediaCardEpInfo, error) {
	mc := &MediaCardEpInfo{}
	err := scan(&mc.CardID, &mc.TmdbID, &mc.OriginalTitle, &mc.Title, &mc.ImdbID,
		&mc.MyshowsID, &mc.EpisodesSyncedAt, &mc.EpisodeRunTime, &mc.Status, &mc.Year, &mc.Tracked)
	return mc, err
}

// GetMediaCardEpInfo returns sync-relevant fields for a card. Returns nil if not found.
func GetMediaCardEpInfo(ctx context.Context, cardID string) *MediaCardEpInfo {
	mc, err := scanEpisodeCard(postgres.Pool.QueryRow(ctx,
		`SELECT `+episodeCardFields+` FROM media_cards mc WHERE mc.card_id = $1`, cardID).Scan)
	if err != nil {
		return nil
	}
	return mc
}

// EpisodeSyncDue is shared by scheduled, device and card-view refreshes.
// A missing future air date or an obsolete Ended status must not stop discovery.
func EpisodeSyncDue(mc *MediaCardEpInfo, now time.Time) bool {
	if mc.MyshowsID == nil || *mc.MyshowsID <= 0 {
		return false
	}
	if mc.EpisodesSyncedAt == nil {
		return true
	}
	interval := 24 * time.Hour
	if !mc.Tracked && mc.Status != nil && (*mc.Status == "Ended" || *mc.Status == "Canceled") {
		interval *= 30
	}
	return !now.Before(mc.EpisodesSyncedAt.Add(interval))
}

// SetMyshowsID persists the myshows_id for a media card.
func SetMyshowsID(ctx context.Context, cardID string, myshowsID int) error {
	_, err := postgres.Pool.Exec(ctx,
		`UPDATE media_cards SET myshows_id = $1, updated_at = now() WHERE card_id = $2`, myshowsID, cardID)
	return err
}

// HasEpisodes проверяет есть ли хоть один эпизод для этого сериала.
func HasEpisodes(ctx context.Context, tmdbShowID int64) bool {
	var exists bool
	postgres.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM episodes WHERE tmdb_show_id=$1)`, tmdbShowID).Scan(&exists) //nolint:errcheck
	return exists
}

// GetEpisodes returns all episodes for a TMDB show ordered by season, episode.
func GetEpisodes(ctx context.Context, tmdbShowID int64) []EpisodeRow {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT season, episode, title, duration_sec, is_special, COALESCE(hash,''), air_date, still_path, overview
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
			&ep.IsSpecial, &ep.Hash, &ep.AirDate, &ep.StillPath, &ep.Overview,
		); err == nil {
			result = append(result, ep)
		}
	}
	return result
}

// SetEpisodeSeasonInfo persists TMDB's still_path/overview per episode for
// one season (episode_number → info, "" meaning TMDB has none/empty). Any
// local episode in that season not covered by TMDB's response is also
// stamped with empty strings for both in the same transaction, so a mismatched/
// renumbered episode (e.g. a MyShows-only special) doesn't get re-queried
// against TMDB on every future view.
func SetEpisodeSeasonInfo(ctx context.Context, tmdbShowID int64, season int16, info map[int]EpisodeSeasonInfo) error {
	tx, err := postgres.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for epNum, v := range info {
		if _, err := tx.Exec(ctx,
			`UPDATE episodes SET still_path=$4, overview=$5 WHERE tmdb_show_id=$1 AND season=$2 AND episode=$3`,
			tmdbShowID, season, epNum, v.StillPath, v.Overview,
		); err != nil {
			return fmt.Errorf("set season info s%de%d: %w", season, epNum, err)
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE episodes SET still_path=COALESCE(still_path,''), overview=COALESCE(overview,'')
		 WHERE tmdb_show_id=$1 AND season=$2 AND (still_path IS NULL OR overview IS NULL)`,
		tmdbShowID, season,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// GetEpisodeRefreshCandidates loads oldest eligible cards first. deviceID=0
// means the whole catalog; otherwise restrict to that device's timecodes.
// EpisodeSyncDue applies the same refresh policy as opening a card.
func GetEpisodeRefreshCandidates(ctx context.Context, deviceID int64) ([]*MediaCardEpInfo, error) {
	rows, err := postgres.Pool.Query(ctx, `SELECT `+episodeCardFields+`
  FROM media_cards mc
  WHERE mc.media_type = 'tv' AND mc.myshows_id > 0
    AND (mc.episodes_synced_at IS NULL OR mc.episodes_synced_at <= now() - interval '1 day')
    AND ($1::bigint = 0 OR EXISTS (SELECT 1 FROM timecodes t
                                 WHERE t.card_id = mc.card_id AND t.device_id = $1))
  ORDER BY 11 DESC, mc.episodes_synced_at ASC NULLS FIRST, mc.card_id`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*MediaCardEpInfo
	now := time.Now()
	for rows.Next() {
		mc, err := scanEpisodeCard(rows.Scan)
		if err != nil {
			return nil, err
		}
		if EpisodeSyncDue(mc, now) {
			result = append(result, mc)
		}
	}
	return result, rows.Err()
}

// UpsertEpisodes inserts or updates episodes for a show from MyShows.
func UpsertEpisodes(ctx context.Context, tmdbShowID int64, eps []EpisodeRow) error {
	tx, err := postgres.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, ep := range eps {
		var msEpID *int
		if ep.MyshowsEpID > 0 {
			msEpID = &ep.MyshowsEpID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO episodes (tmdb_show_id, season, episode, title, duration_sec, is_special, hash, air_date, myshows_ep_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (tmdb_show_id, season, episode) DO UPDATE SET
				title        = COALESCE(EXCLUDED.title, episodes.title),
				duration_sec = COALESCE(EXCLUDED.duration_sec, episodes.duration_sec),
				is_special   = EXCLUDED.is_special,
				hash         = EXCLUDED.hash,
				air_date     = COALESCE(EXCLUDED.air_date, episodes.air_date),
				myshows_ep_id = COALESCE(EXCLUDED.myshows_ep_id, episodes.myshows_ep_id)`,
			tmdbShowID, ep.Season, ep.Episode, ep.Title, ep.DurationSec,
			ep.IsSpecial, ep.Hash, ep.AirDate, msEpID,
		); err != nil {
			return fmt.Errorf("upsert episode s%de%d: %w", ep.Season, ep.Episode, err)
		}
	}

	// Remove stale rows for episodes that MyShows renumbered: same myshows_ep_id
	// but a different (season, episode) than the current batch. This cleans up
	// duplicates like S01E-1 / S01E00 created across syncs (same source episode).
	var epIDs, seasons, episodes []int
	for _, ep := range eps {
		if ep.MyshowsEpID > 0 {
			epIDs = append(epIDs, ep.MyshowsEpID)
			seasons = append(seasons, int(ep.Season))
			episodes = append(episodes, int(ep.Episode))
		}
	}
	if len(epIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			DELETE FROM episodes e
			USING unnest($2::int[], $3::int[], $4::int[]) AS b(ep_id, season, episode)
			WHERE e.tmdb_show_id = $1
			  AND e.myshows_ep_id = b.ep_id
			  AND (e.season <> b.season OR e.episode <> b.episode)`,
			tmdbShowID, epIDs, seasons, episodes,
		); err != nil {
			return fmt.Errorf("dedup episodes: %w", err)
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
