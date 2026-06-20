package store

import (
	"context"
	"movies-api/db/postgres"
)

const myshowsPageSize = 20

// MyshowsStatusItem is the incoming item from the plugin.
type MyshowsStatusItem struct {
	MyshowsID         int
	TmdbID            int64
	MediaType         string
	CacheType         string // watching/watchlist/watched/cancelled
	UnwatchedCount    *int
	NextEpisode       *string
	ProgressMarker    *string
	UnwatchedEpisodes []int64 // id непросмотренных серий (для watching)
}

// MyshowsCard is the outgoing card for list responses.
type MyshowsCard struct {
	TmdbID          int64   `json:"id"`
	MediaType       string  `json:"media_type"`
	MyshowsID       int     `json:"myshows_id"`
	Title           string  `json:"title,omitempty"`
	OriginalTitle   string  `json:"original_title,omitempty"`
	Name            string  `json:"name,omitempty"`
	OriginalName    string  `json:"original_name,omitempty"`
	PosterPath      string  `json:"poster_path"`
	BackdropPath    string  `json:"backdrop_path"`
	Overview        string  `json:"overview"`
	VoteAverage     float64 `json:"vote_average"`
	ReleaseDate     string  `json:"release_date,omitempty"`
	FirstAirDate    string  `json:"first_air_date,omitempty"`
	NumberOfSeasons int     `json:"number_of_seasons,omitempty"`
	UnwatchedCount  *int    `json:"unwatched_count,omitempty"`
	NextEpisode     *string `json:"next_episode,omitempty"`
	ProgressMarker  *string `json:"progress_marker,omitempty"`
	UnwatchedEpisodes []int64 `json:"unwatched_episodes,omitempty"`
}

// ─── Watching ─────────────────────────────────────────────────────────────────

// UpsertWatching replaces the watching list for a device+profile with a fresh set.
// Also dual-writes watching items as "watched" in myshows_user_status.
func UpsertWatching(ctx context.Context, deviceID int64, profileID string, items []MyshowsStatusItem) error {
	tx, err := postgres.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if len(items) == 0 {
		_, err = tx.Exec(ctx, `DELETE FROM myshows_watching WHERE device_id=$1 AND profile_id=$2`, deviceID, profileID)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	// Upsert each item and collect item IDs.
	itemIDs := make([]int64, 0, len(items))
	for _, it := range items {
		var itemID int64
		err = tx.QueryRow(ctx, `
			INSERT INTO myshows_items (myshows_id, tmdb_id, media_type)
			VALUES ($1, $2, $3)
			ON CONFLICT (myshows_id) DO UPDATE SET
				tmdb_id    = EXCLUDED.tmdb_id,
				media_type = EXCLUDED.media_type
			RETURNING id`, it.MyshowsID, it.TmdbID, it.MediaType,
		).Scan(&itemID)
		if err != nil {
			return err
		}
		itemIDs = append(itemIDs, itemID)

		_, err = tx.Exec(ctx, `
			INSERT INTO myshows_watching (device_id, profile_id, item_id, unwatched_count, next_episode, progress_marker, unwatched_episode_ids, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, now())
			ON CONFLICT (device_id, profile_id, item_id) DO UPDATE SET
				unwatched_count       = EXCLUDED.unwatched_count,
				next_episode          = EXCLUDED.next_episode,
				progress_marker       = EXCLUDED.progress_marker,
				unwatched_episode_ids = EXCLUDED.unwatched_episode_ids,
				updated_at            = now()`,
			deviceID, profileID, itemID, it.UnwatchedCount, it.NextEpisode, it.ProgressMarker, it.UnwatchedEpisodes,
		)
		if err != nil {
			return err
		}

		// Dual-write: watching → watched
		_, err = tx.Exec(ctx, `
			INSERT INTO myshows_user_status (device_id, profile_id, item_id, cache_type, updated_at)
			VALUES ($1, $2, $3, 'watched', now())
			ON CONFLICT (device_id, profile_id, item_id) DO UPDATE SET
				cache_type = 'watched', updated_at = now()`,
			deviceID, profileID, itemID,
		)
		if err != nil {
			return err
		}
	}

	// Remove stale watching rows (items no longer in the list).
	if len(itemIDs) > 0 {
		_, err = tx.Exec(ctx, `
			DELETE FROM myshows_watching
			WHERE device_id=$1 AND profile_id=$2 AND item_id <> ALL($3)`,
			deviceID, profileID, itemIDs,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// GetWatching returns all watching items for a device+profile joined with media_cards.
func GetWatching(ctx context.Context, deviceID int64, profileID string) ([]MyshowsCard, int, error) {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT mi.myshows_id, mi.tmdb_id, mi.media_type,
		       COALESCE(mc.title,''), COALESCE(mc.original_title,''),
		       COALESCE(mc.poster_path,''), COALESCE(mc.backdrop_path,''),
		       COALESCE(mc.overview,''), COALESCE(mc.vote_average,0),
		       COALESCE(mc.release_date::text,''), COALESCE(mc.first_air_date::text,''),
		       COALESCE(mc.number_of_seasons,0),
		       mw.unwatched_count, mw.next_episode, mw.progress_marker, mw.unwatched_episode_ids
		FROM myshows_watching mw
		JOIN myshows_items mi ON mi.id = mw.item_id
		LEFT JOIN media_cards mc ON mc.tmdb_id = mi.tmdb_id AND mc.media_type = mi.media_type
		WHERE mw.device_id=$1 AND mw.profile_id=$2
		ORDER BY mw.updated_at DESC`, deviceID, profileID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var cards []MyshowsCard
	for rows.Next() {
		var c MyshowsCard
		var releaseDate, firstAirDate string
		if err := rows.Scan(
			&c.MyshowsID, &c.TmdbID, &c.MediaType,
			&c.Title, &c.OriginalTitle,
			&c.PosterPath, &c.BackdropPath, &c.Overview, &c.VoteAverage,
			&releaseDate, &firstAirDate, &c.NumberOfSeasons,
			&c.UnwatchedCount, &c.NextEpisode, &c.ProgressMarker, &c.UnwatchedEpisodes,
		); err != nil {
			continue
		}
		applyCardDates(&c, releaseDate, firstAirDate)
		cards = append(cards, c)
	}
	return cards, len(cards), nil
}

// ─── Profile shows (full watching+finished list for timetable) ───────────────

// UpsertProfileShows replaces the full profile show list for a device+profile.
func UpsertProfileShows(ctx context.Context, deviceID int64, profileID string, items []MyshowsStatusItem) error {
	tx, err := postgres.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if len(items) == 0 {
		_, err = tx.Exec(ctx, `DELETE FROM myshows_profile_shows WHERE device_id=$1 AND profile_id=$2`, deviceID, profileID)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	itemIDs := make([]int64, 0, len(items))
	for _, it := range items {
		var itemID int64
		err = tx.QueryRow(ctx, `
			INSERT INTO myshows_items (myshows_id, tmdb_id, media_type)
			VALUES ($1, $2, $3)
			ON CONFLICT (myshows_id) DO UPDATE SET
				tmdb_id    = EXCLUDED.tmdb_id,
				media_type = EXCLUDED.media_type
			RETURNING id`, it.MyshowsID, it.TmdbID, it.MediaType,
		).Scan(&itemID)
		if err != nil {
			return err
		}
		itemIDs = append(itemIDs, itemID)

		_, err = tx.Exec(ctx, `
			INSERT INTO myshows_profile_shows (device_id, profile_id, item_id, updated_at)
			VALUES ($1, $2, $3, now())
			ON CONFLICT (device_id, profile_id, item_id) DO UPDATE SET updated_at = now()`,
			deviceID, profileID, itemID,
		)
		if err != nil {
			return err
		}
	}

	_, err = tx.Exec(ctx, `
		DELETE FROM myshows_profile_shows
		WHERE device_id=$1 AND profile_id=$2 AND item_id <> ALL($3)`,
		deviceID, profileID, itemIDs,
	)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// GetProfileShows returns all profile shows for a device+profile joined with media_cards.
func GetProfileShows(ctx context.Context, deviceID int64, profileID string) ([]MyshowsCard, int, error) {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT mi.myshows_id, mi.tmdb_id, mi.media_type,
		       COALESCE(mc.title,''), COALESCE(mc.original_title,''),
		       COALESCE(mc.poster_path,''), COALESCE(mc.backdrop_path,''),
		       COALESCE(mc.overview,''), COALESCE(mc.vote_average,0),
		       COALESCE(mc.release_date::text,''), COALESCE(mc.first_air_date::text,''),
		       COALESCE(mc.number_of_seasons,0)
		FROM myshows_profile_shows mps
		JOIN myshows_items mi ON mi.id = mps.item_id
		LEFT JOIN media_cards mc ON mc.tmdb_id = mi.tmdb_id AND mc.media_type = mi.media_type
		WHERE mps.device_id=$1 AND mps.profile_id=$2
		ORDER BY mps.updated_at DESC`, deviceID, profileID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var cards []MyshowsCard
	for rows.Next() {
		var c MyshowsCard
		var releaseDate, firstAirDate string
		if err := rows.Scan(
			&c.MyshowsID, &c.TmdbID, &c.MediaType,
			&c.Title, &c.OriginalTitle,
			&c.PosterPath, &c.BackdropPath, &c.Overview, &c.VoteAverage,
			&releaseDate, &firstAirDate, &c.NumberOfSeasons,
		); err != nil {
			continue
		}
		applyCardDates(&c, releaseDate, firstAirDate)
		cards = append(cards, c)
	}
	return cards, len(cards), nil
}

// ─── User status (watchlist / watched / cancelled) ────────────────────────────

// UpsertStatus replaces the status list for a given cache_type with a fresh set.
func UpsertStatus(ctx context.Context, deviceID int64, profileID, cacheType string, items []MyshowsStatusItem) error {
	tx, err := postgres.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if len(items) == 0 {
		_, err = tx.Exec(ctx, `
			DELETE FROM myshows_user_status
			WHERE device_id=$1 AND profile_id=$2 AND cache_type=$3`,
			deviceID, profileID, cacheType)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	itemIDs := make([]int64, 0, len(items))
	for _, it := range items {
		var itemID int64
		err = tx.QueryRow(ctx, `
			INSERT INTO myshows_items (myshows_id, tmdb_id, media_type)
			VALUES ($1, $2, $3)
			ON CONFLICT (myshows_id) DO UPDATE SET
				tmdb_id    = EXCLUDED.tmdb_id,
				media_type = EXCLUDED.media_type
			RETURNING id`, it.MyshowsID, it.TmdbID, it.MediaType,
		).Scan(&itemID)
		if err != nil {
			return err
		}
		itemIDs = append(itemIDs, itemID)

		_, err = tx.Exec(ctx, `
			INSERT INTO myshows_user_status (device_id, profile_id, item_id, cache_type, updated_at)
			VALUES ($1, $2, $3, $4, now())
			ON CONFLICT (device_id, profile_id, item_id) DO UPDATE SET
				cache_type = EXCLUDED.cache_type, updated_at = now()`,
			deviceID, profileID, itemID, cacheType,
		)
		if err != nil {
			return err
		}
	}

	// Remove stale rows for this cache_type.
	if len(itemIDs) > 0 {
		_, err = tx.Exec(ctx, `
			DELETE FROM myshows_user_status
			WHERE device_id=$1 AND profile_id=$2 AND cache_type=$3 AND item_id <> ALL($4)`,
			deviceID, profileID, cacheType, itemIDs,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// UpsertStatusByMyshowsID updates myshows_user_status using only myshows_id (no tmdb_id).
// Used for serial_status / movie_status endpoints. Skips unknown myshows_ids.
func UpsertStatusByMyshowsID(ctx context.Context, deviceID int64, profileID string, items []MyshowsStatusItem) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := postgres.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, it := range items {
		if it.CacheType == "" {
			continue
		}
		var itemID int64
		err = tx.QueryRow(ctx,
			`SELECT id FROM myshows_items WHERE myshows_id=$1`, it.MyshowsID,
		).Scan(&itemID)
		if err != nil {
			continue // not in our DB yet — skip
		}

		if it.CacheType == "remove" {
			tx.Exec(ctx, `DELETE FROM myshows_watching WHERE device_id=$1 AND profile_id=$2 AND item_id=$3`, deviceID, profileID, itemID)    //nolint:errcheck
			tx.Exec(ctx, `DELETE FROM myshows_user_status WHERE device_id=$1 AND profile_id=$2 AND item_id=$3`, deviceID, profileID, itemID) //nolint:errcheck
			continue
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO myshows_user_status (device_id, profile_id, item_id, cache_type, updated_at)
			VALUES ($1, $2, $3, $4, now())
			ON CONFLICT (device_id, profile_id, item_id) DO UPDATE SET
				cache_type = EXCLUDED.cache_type, updated_at = now()`,
			deviceID, profileID, itemID, it.CacheType,
		)
		if err != nil {
			continue
		}

		if it.CacheType != "watching" {
			tx.Exec(ctx, `DELETE FROM myshows_watching WHERE device_id=$1 AND profile_id=$2 AND item_id=$3`, deviceID, profileID, itemID) //nolint:errcheck
		}
	}

	return tx.Commit(ctx)
}

// GetStatusPage returns a paginated list for a given cache_type.
func GetStatusPage(ctx context.Context, deviceID int64, profileID, cacheType string, page int) ([]MyshowsCard, int, int, error) {
	var total int
	err := postgres.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM myshows_user_status mus
		JOIN myshows_items mi ON mi.id = mus.item_id
		WHERE mus.device_id=$1 AND mus.profile_id=$2 AND mus.cache_type=$3`,
		deviceID, profileID, cacheType,
	).Scan(&total)
	if err != nil {
		return nil, 0, 0, err
	}

	totalPages := (total + myshowsPageSize - 1) / myshowsPageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if page < 1 {
		page = 1
	}

	rows, err := postgres.Pool.Query(ctx, `
		SELECT mi.myshows_id, mi.tmdb_id, mi.media_type,
		       COALESCE(mc.title,''), COALESCE(mc.original_title,''),
		       COALESCE(mc.poster_path,''), COALESCE(mc.backdrop_path,''),
		       COALESCE(mc.overview,''), COALESCE(mc.vote_average,0),
		       COALESCE(mc.release_date::text,''), COALESCE(mc.first_air_date::text,''),
		       COALESCE(mc.number_of_seasons,0)
		FROM myshows_user_status mus
		JOIN myshows_items mi ON mi.id = mus.item_id
		LEFT JOIN media_cards mc ON mc.tmdb_id = mi.tmdb_id AND mc.media_type = mi.media_type
		WHERE mus.device_id=$1 AND mus.profile_id=$2 AND mus.cache_type=$3
		ORDER BY mus.updated_at DESC
		LIMIT $4 OFFSET $5`,
		deviceID, profileID, cacheType, myshowsPageSize, (page-1)*myshowsPageSize,
	)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()

	var cards []MyshowsCard
	for rows.Next() {
		var c MyshowsCard
		var releaseDate, firstAirDate string
		if err := rows.Scan(
			&c.MyshowsID, &c.TmdbID, &c.MediaType,
			&c.Title, &c.OriginalTitle,
			&c.PosterPath, &c.BackdropPath, &c.Overview, &c.VoteAverage,
			&releaseDate, &firstAirDate, &c.NumberOfSeasons,
		); err != nil {
			continue
		}
		applyCardDates(&c, releaseDate, firstAirDate)
		cards = append(cards, c)
	}
	return cards, total, totalPages, nil
}

// ─── Single-item status ───────────────────────────────────────────────────────

// SetSingleStatus upserts or removes status for one item.
func SetSingleStatus(ctx context.Context, deviceID int64, profileID string, it MyshowsStatusItem) error {
	tx, err := postgres.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var itemID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO myshows_items (myshows_id, tmdb_id, media_type)
		VALUES ($1, $2, $3)
		ON CONFLICT (myshows_id) DO UPDATE SET
			tmdb_id    = EXCLUDED.tmdb_id,
			media_type = EXCLUDED.media_type
		RETURNING id`, it.MyshowsID, it.TmdbID, it.MediaType,
	).Scan(&itemID)
	if err != nil {
		return err
	}

	if it.CacheType == "remove" {
		tx.Exec(ctx, `DELETE FROM myshows_watching WHERE device_id=$1 AND profile_id=$2 AND item_id=$3`, deviceID, profileID, itemID)    //nolint:errcheck
		tx.Exec(ctx, `DELETE FROM myshows_user_status WHERE device_id=$1 AND profile_id=$2 AND item_id=$3`, deviceID, profileID, itemID) //nolint:errcheck
		return tx.Commit(ctx)
	}

	if it.CacheType == "watching" {
		_, err = tx.Exec(ctx, `
			INSERT INTO myshows_watching (device_id, profile_id, item_id, unwatched_count, next_episode, progress_marker, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, now())
			ON CONFLICT (device_id, profile_id, item_id) DO UPDATE SET
				unwatched_count = EXCLUDED.unwatched_count,
				next_episode    = EXCLUDED.next_episode,
				progress_marker = EXCLUDED.progress_marker,
				updated_at      = now()`,
			deviceID, profileID, itemID, it.UnwatchedCount, it.NextEpisode, it.ProgressMarker,
		)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO myshows_user_status (device_id, profile_id, item_id, cache_type, updated_at)
			VALUES ($1, $2, $3, 'watched', now())
			ON CONFLICT (device_id, profile_id, item_id) DO UPDATE SET
				cache_type = 'watched', updated_at = now()`,
			deviceID, profileID, itemID,
		)
	} else {
		_, err = tx.Exec(ctx, `
			INSERT INTO myshows_user_status (device_id, profile_id, item_id, cache_type, updated_at)
			VALUES ($1, $2, $3, $4, now())
			ON CONFLICT (device_id, profile_id, item_id) DO UPDATE SET
				cache_type = EXCLUDED.cache_type, updated_at = now()`,
			deviceID, profileID, itemID, it.CacheType,
		)
		if err == nil {
			tx.Exec(ctx, `DELETE FROM myshows_watching WHERE device_id=$1 AND profile_id=$2 AND item_id=$3`, deviceID, profileID, itemID) //nolint:errcheck
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// GetSingleStatus returns the cache_type for one item by tmdb_id+media_type.
// Returns "" if not found.
func GetSingleStatus(ctx context.Context, deviceID int64, profileID string, tmdbID int64, mediaType string) string {
	// Check watching first.
	var n int
	postgres.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM myshows_watching mw
		JOIN myshows_items mi ON mi.id = mw.item_id
		WHERE mw.device_id=$1 AND mw.profile_id=$2
		  AND mi.tmdb_id=$3 AND mi.media_type=$4`,
		deviceID, profileID, tmdbID, mediaType,
	).Scan(&n) //nolint:errcheck
	if n > 0 {
		return "watching"
	}

	var cacheType string
	postgres.Pool.QueryRow(ctx, `
		SELECT mus.cache_type FROM myshows_user_status mus
		JOIN myshows_items mi ON mi.id = mus.item_id
		WHERE mus.device_id=$1 AND mus.profile_id=$2
		  AND mi.tmdb_id=$3 AND mi.media_type=$4`,
		deviceID, profileID, tmdbID, mediaType,
	).Scan(&cacheType) //nolint:errcheck
	return cacheType
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func applyCardDates(c *MyshowsCard, releaseDate, firstAirDate string) {
	if c.MediaType == "tv" {
		c.Name = c.Title
		c.OriginalName = c.OriginalTitle
		c.Title = ""
		c.OriginalTitle = ""
		c.FirstAirDate = firstAirDate
	} else {
		c.ReleaseDate = releaseDate
		c.NumberOfSeasons = 0
	}
}

// ─── Timetable ────────────────────────────────────────────────────────────────

type TimetableEpisode struct {
	TmdbShowID int    `json:"tmdb_show_id"`
	Season     int    `json:"season_number"`
	Episode    int    `json:"episode_number"`
	AirDate    string `json:"air_date"`
	Title      string `json:"name"`
}

// GetTimetableByTmdbIDs returns future, non-special episodes for the given TMDB show IDs.
func GetTimetableByTmdbIDs(ctx context.Context, ids []int64) ([]TimetableEpisode, error) {
	if len(ids) == 0 {
		return []TimetableEpisode{}, nil
	}
	rows, err := postgres.Pool.Query(ctx, `
		SELECT e.tmdb_show_id, e.season, e.episode,
		       COALESCE(e.air_date::text, ''), COALESCE(e.title, '')
		FROM episodes e
		WHERE e.tmdb_show_id = ANY($1)
		  AND e.air_date >= CURRENT_DATE
		  AND e.is_special = false
		ORDER BY e.air_date, e.tmdb_show_id, e.season, e.episode`,
		ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var eps []TimetableEpisode
	for rows.Next() {
		var ep TimetableEpisode
		if err := rows.Scan(&ep.TmdbShowID, &ep.Season, &ep.Episode, &ep.AirDate, &ep.Title); err != nil {
			continue
		}
		eps = append(eps, ep)
	}
	return eps, nil
}
