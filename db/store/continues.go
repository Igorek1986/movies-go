package store

import (
	"context"
	"encoding/json"
	"fmt"
	"lampa-api/db/postgres"
	"strings"
	"time"
)

type ContinuesEntry struct {
	ID            int64   `json:"id"`
	MediaType     string  `json:"media_type"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	PosterPath    string  `json:"poster_path"`
	BackdropPath  string  `json:"backdrop_path"`
	Overview      string  `json:"overview"`
	ReleaseDate   string  `json:"release_date"`
	FirstAirDate  string  `json:"first_air_date"`
	VoteAverage   float64 `json:"vote_average"`
	MaxPercent    float64 `json:"max_percent"`
}

// GetContinues returns items that are in-progress (not fully watched).
func GetContinues(ctx context.Context, deviceID int64, profileID, mediaFilter string, minPct, page, perPage int) ([]ContinuesEntry, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	if minPct < 1 {
		minPct = 90
	}

	// Aggregate timecodes: card_id → {max_pct, last_watched}
	mediaWhere := ""
	if mediaFilter != "" {
		mediaWhere = fmt.Sprintf(" AND t.card_id LIKE '%%_%s'", mediaFilter)
	}
	rows, err := postgres.Pool.Query(ctx, fmt.Sprintf(`
		SELECT t.card_id,
		       MAX((t.data::jsonb->>'percent')::float) AS max_pct,
		       MAX(t.updated_at) AS last_watched
		FROM timecodes t
		WHERE t.device_id = $1
		  AND t.lampa_profile_id = $2
		  AND t.card_id ~ '^[0-9]+_(movie|tv)$'
		  %s
		GROUP BY t.card_id
		HAVING MAX((t.data::jsonb->>'percent')::float) < $3
		ORDER BY MAX(t.updated_at) DESC`, mediaWhere),
		deviceID, profileID, float64(minPct),
	)
	if err != nil {
		return nil, 0
	}
	defer rows.Close()

	type agg struct {
		cardID      string
		maxPct      float64
		lastWatched time.Time
	}
	var all []agg
	for rows.Next() {
		var a agg
		if rows.Scan(&a.cardID, &a.maxPct, &a.lastWatched) == nil {
			all = append(all, a)
		}
	}
	rows.Close()

	total := len(all)
	if total == 0 {
		return nil, 0
	}

	start := (page - 1) * perPage
	if start >= total {
		return nil, total
	}
	page_items := all[start:]
	if len(page_items) > perPage {
		page_items = page_items[:perPage]
	}

	// Fetch media cards.
	cardIDs := make([]string, len(page_items))
	for i, a := range page_items {
		cardIDs[i] = a.cardID
	}
	placeholders := make([]string, len(cardIDs))
	pargs := make([]any, len(cardIDs))
	for i, cid := range cardIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		pargs[i] = cid
	}
	mcRows, err := postgres.Pool.Query(ctx,
		fmt.Sprintf(`SELECT card_id, tmdb_id, media_type, title, original_title,
			poster_path, backdrop_path, overview, vote_average,
			release_date, first_air_date
			FROM media_cards WHERE card_id IN (%s)`, strings.Join(placeholders, ",")),
		pargs...,
	)

	type mcData struct {
		cardID, mediaType, title, origTitle, poster, backdrop, overview string
		tmdbID                                                            int64
		voteAvg                                                           float64
		releaseDate, firstAirDate                                         *string
	}
	mcMap := map[string]mcData{}
	if err == nil {
		for mcRows.Next() {
			var m mcData
			var seasonsJSON []byte
			_ = seasonsJSON
			mcRows.Scan(&m.cardID, &m.tmdbID, &m.mediaType, &m.title, &m.origTitle, //nolint:errcheck
				&m.poster, &m.backdrop, &m.overview, &m.voteAvg,
				&m.releaseDate, &m.firstAirDate)
			mcMap[m.cardID] = m
		}
		mcRows.Close()
	}

	var result []ContinuesEntry
	for _, a := range page_items {
		m := mcMap[a.cardID]
		release := ""
		if m.releaseDate != nil {
			release = *m.releaseDate
		}
		firstAir := ""
		if m.firstAirDate != nil {
			firstAir = *m.firstAirDate
		}
		result = append(result, ContinuesEntry{
			ID:            m.tmdbID,
			MediaType:     m.mediaType,
			Title:         m.title,
			OriginalTitle: m.origTitle,
			PosterPath:    m.poster,
			BackdropPath:  m.backdrop,
			Overview:      m.overview,
			ReleaseDate:   release,
			FirstAirDate:  firstAir,
			VoteAverage:   m.voteAvg,
			MaxPercent:    a.maxPct,
		})
	}
	return result, total
}

// GetPopular returns globally popular cards weighted by view_count.
func GetPopular(ctx context.Context, page, perPage int, search string) ([]MediaRow, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}

	searchWhere := ""
	var args []any
	args = append(args, 30) // popular_period_days
	if search != "" {
		searchWhere = " AND (m.title ILIKE $2 OR m.original_title ILIKE $2)"
		args = append(args, "%"+search+"%")
	}

	baseSQL := fmt.Sprintf(`
		SELECT t.card_id, SUM(t.view_count) AS weight
		FROM timecodes t
		JOIN media_cards m ON m.card_id = t.card_id
		WHERE t.view_count > 0
		  AND t.counted_at >= (CURRENT_DATE - ($1 || ' days')::interval)
		  %s
		GROUP BY t.card_id
		HAVING SUM(t.view_count) > 0
		ORDER BY weight DESC`, searchWhere)

	var total int
	postgres.Pool.QueryRow(ctx, //nolint:errcheck
		fmt.Sprintf("SELECT COUNT(*) FROM (%s) sq", baseSQL), args...,
	).Scan(&total)

	offset := (page - 1) * perPage
	popArgs := append(args, perPage, offset)
	popRows, err := postgres.Pool.Query(ctx,
		fmt.Sprintf("%s LIMIT $%d OFFSET $%d", baseSQL, len(args)+1, len(args)+2),
		popArgs...,
	)
	if err != nil {
		return nil, 0
	}
	defer popRows.Close()

	var cardIDs []string
	for popRows.Next() {
		var cid string
		var w float64
		if popRows.Scan(&cid, &w) == nil {
			cardIDs = append(cardIDs, cid)
		}
	}
	popRows.Close()

	if len(cardIDs) == 0 {
		return nil, total
	}

	placeholders := make([]string, len(cardIDs))
	mcArgs := make([]any, len(cardIDs))
	for i, cid := range cardIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		mcArgs[i] = cid
	}
	mcRowsQ, err := postgres.Pool.Query(ctx,
		fmt.Sprintf(`SELECT m.tmdb_id, m.media_type, m.title, m.original_title,
			m.overview, m.poster_path, m.backdrop_path,
			m.release_date::text, m.first_air_date::text, m.last_air_date::text,
			m.vote_average, m.vote_count, m.original_language, m.adult, m.status,
			m.number_of_seasons, m.seasons, m.last_ep_season, m.last_ep_number, m.updated_at,
			m.best_video_quality, m.latest_torrent_date
			FROM media_cards m WHERE m.card_id IN (%s)`, strings.Join(placeholders, ",")),
		mcArgs...,
	)
	if err != nil {
		return nil, total
	}
	defer mcRowsQ.Close()

	mcMap := map[string]MediaRow{}
	for mcRowsQ.Next() {
		var r MediaRow
		if err := mcRowsQ.Scan(
			&r.TmdbID, &r.MediaType, &r.Title, &r.OriginalTitle,
			&r.Overview, &r.PosterPath, &r.BackdropPath,
			&r.ReleaseDate, &r.FirstAirDate, &r.LastAirDate,
			&r.VoteAverage, &r.VoteCount, &r.OriginalLanguage, &r.Adult, &r.Status,
			&r.NumberOfSeasons, &r.Seasons, &r.LastEpSeason, &r.LastEpNumber, &r.UpdatedAt,
			&r.VideoQuality, &r.LatestTorrentDate,
		); err == nil {
			mcMap[fmt.Sprintf("%d_%s", r.TmdbID, r.MediaType)] = r
		}
	}

	var result []MediaRow
	for _, cid := range cardIDs {
		if r, ok := mcMap[cid]; ok {
			result = append(result, r)
		}
	}

	_ = json.Marshal // suppress unused import
	return result, total
}
