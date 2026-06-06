package store

import (
	"context"
	"encoding/json"
	"fmt"
	"movies-api/db/postgres"
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
		  AND t.profile_id = $2
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
			release_date::text, first_air_date::text
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

// RecordPlayEvent records one unique play per (card, ident, day), keeping the
// deepest watch progress (max_percent) seen that day.
func RecordPlayEvent(ctx context.Context, cardID, ident string, pct int) {
	if cardID == "" || ident == "" {
		return
	}
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`INSERT INTO media_play_events (card_id, ident, date, max_percent)
		 VALUES ($1, $2, CURRENT_DATE, $3)
		 ON CONFLICT (card_id, ident, date)
		 DO UPDATE SET max_percent = GREATEST(media_play_events.max_percent, EXCLUDED.max_percent)`,
		cardID, ident, pct,
	)
}

// HasPopularData reports whether any play events exist within the given day window.
func HasPopularData(ctx context.Context, days int) bool {
	var count int
	postgres.Pool.QueryRow(ctx, //nolint:errcheck
		`SELECT COUNT(*) FROM media_play_events WHERE date >= CURRENT_DATE - ($1::int * INTERVAL '1 day')`,
		days,
	).Scan(&count)
	return count > 0
}

// GetPopular returns globally popular cards by unique plays from media_play_events.
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
		snip, arg := searchSQL(search, 2)
		searchWhere = " AND " + snip
		args = append(args, arg)
	}

	baseSQL := fmt.Sprintf(`
		SELECT e.card_id, COUNT(*) AS weight, COUNT(DISTINCT e.ident) AS viewers
		FROM media_play_events e
		JOIN media_cards m ON m.card_id = e.card_id
		WHERE e.date >= (CURRENT_DATE - ($1::int * INTERVAL '1 day'))
		  %s
		GROUP BY e.card_id
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
	playsByID := map[string]int{}
	viewersByID := map[string]int{}
	for popRows.Next() {
		var cid string
		var plays, viewers int
		if popRows.Scan(&cid, &plays, &viewers) == nil {
			cardIDs = append(cardIDs, cid)
			playsByID[cid] = plays
			viewersByID[cid] = viewers
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
			m.best_video_quality, m.latest_torrent_date,
			m.certification_ru, m.certification_us
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
			&r.CertificationRU, &r.CertificationUS,
		); err == nil {
			mcMap[fmt.Sprintf("%d_%s", r.TmdbID, r.MediaType)] = r
		}
	}

	var result []MediaRow
	for _, cid := range cardIDs {
		if r, ok := mcMap[cid]; ok {
			r.Plays = playsByID[cid]
			r.Viewers = viewersByID[cid]
			result = append(result, r)
		}
	}

	_ = json.Marshal // suppress unused import
	return result, total
}

// CountPopularCards returns the number of distinct cards with play events
// within the given day window.
func CountPopularCards(ctx context.Context, days int) int {
	var n int
	postgres.Pool.QueryRow(ctx, //nolint:errcheck
		`SELECT COUNT(DISTINCT card_id) FROM media_play_events
		 WHERE date >= CURRENT_DATE - ($1::int * INTERVAL '1 day')`,
		days,
	).Scan(&n)
	return n
}

// PopularDaily is one day of aggregated play activity.
type PopularDaily struct {
	Date    string `json:"date"`
	Plays   int    `json:"plays"`   // total play events that day
	Viewers int    `json:"viewers"` // distinct viewers that day
	Cards   int    `json:"cards"`   // distinct cards played that day
}

// GetPopularDaily returns per-day play dynamics for the given window,
// ordered ascending by date. Days without activity are omitted.
func GetPopularDaily(ctx context.Context, days int) []PopularDaily {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT date::text, COUNT(*), COUNT(DISTINCT ident), COUNT(DISTINCT card_id)
		 FROM media_play_events
		 WHERE date >= CURRENT_DATE - ($1::int * INTERVAL '1 day')
		 GROUP BY date
		 ORDER BY date`,
		days,
	)
	if err != nil {
		return []PopularDaily{}
	}
	defer rows.Close()
	out := []PopularDaily{}
	for rows.Next() {
		var d PopularDaily
		if rows.Scan(&d.Date, &d.Plays, &d.Viewers, &d.Cards) == nil {
			out = append(out, d)
		}
	}
	return out
}

// PopularCard is one card ranked by play activity.
type PopularCard struct {
	CardID       string `json:"card_id"`
	TmdbID       int    `json:"tmdb_id"`
	MediaType    string `json:"media_type"`
	Title        string `json:"title"`
	PosterPath   string `json:"poster_path"`
	Year         string `json:"year"`
	Viewers      int    `json:"viewers"`       // distinct people who watched
	Plays        int    `json:"plays"`         // total play events
	AvgPercent   int    `json:"avg_percent"`   // average deepest watch progress
	FinishedRate int    `json:"finished_rate"` // % of plays with max_percent >= 85
}

// GetPopularCards returns cards ranked by unique viewers within the window.
func GetPopularCards(ctx context.Context, days, limit int) []PopularCard {
	if limit < 1 {
		limit = 200
	}
	rows, err := postgres.Pool.Query(ctx,
		`SELECT e.card_id, m.tmdb_id, m.media_type, m.title,
		        COALESCE(m.poster_path, ''),
		        COALESCE(NULLIF(left(m.release_date::text, 4), ''), left(m.first_air_date::text, 4), ''),
		        COUNT(DISTINCT e.ident) AS viewers, COUNT(*) AS plays,
		        COALESCE(ROUND(AVG(e.max_percent) FILTER (WHERE e.max_percent > 0)), 0)::int AS avg_percent,
		        COALESCE(ROUND(
		            100.0 * COUNT(*) FILTER (WHERE e.max_percent >= 85)
		            / NULLIF(COUNT(*) FILTER (WHERE e.max_percent > 0), 0)
		        ), 0)::int AS finished_rate
		 FROM media_play_events e
		 JOIN media_cards m ON m.card_id = e.card_id
		 WHERE e.date >= CURRENT_DATE - ($1::int * INTERVAL '1 day')
		 GROUP BY e.card_id, m.tmdb_id, m.media_type, m.title, m.poster_path, m.release_date, m.first_air_date
		 ORDER BY viewers DESC, plays DESC
		 LIMIT $2`,
		days, limit,
	)
	if err != nil {
		return []PopularCard{}
	}
	defer rows.Close()
	out := []PopularCard{}
	for rows.Next() {
		var c PopularCard
		if rows.Scan(&c.CardID, &c.TmdbID, &c.MediaType, &c.Title,
			&c.PosterPath, &c.Year, &c.Viewers, &c.Plays,
			&c.AvgPercent, &c.FinishedRate) == nil {
			out = append(out, c)
		}
	}
	return out
}
