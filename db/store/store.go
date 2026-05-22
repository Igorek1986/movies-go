// Package store provides PostgreSQL-backed storage operations.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"movies-api/db/models"
	"movies-api/db/postgres"
	"log"
	"strings"
	"time"
)

// normalizeSearch strips punctuation that varies across sources so that
// "Ван-Пис", "Ван Пис", "Ван'Пис" all match each other.
// Both the query and the stored column are normalized the same way.
// normalizeSearch strips punctuation so "Ван-Пис", "Ван Пис", "Ван'Пис" all match.
func normalizeSearch(q string) string {
	const punct = `-''.,:;!?()[]\x27` // ASCII + common punct
	var b strings.Builder
	prev := ' '
	for _, r := range strings.ToLower(q) {
		if strings.ContainsRune(punct, r) {
			r = ' '
		}
		if r == ' ' && prev == ' ' {
			continue
		}
		b.WriteRune(r)
		prev = r
	}
	return strings.TrimSpace(b.String())
}

// searchSQL returns a WHERE snippet and the normalized ILIKE arg for full-text search.
// Uses REGEXP_REPLACE on stored columns to normalize punctuation at query time.
func searchSQL(q string, n int) (snippet string, arg string) {
	norm := normalizeSearch(q)
	col := `REGEXP_REPLACE(LOWER(%s), '[-''.,;:!?()\[\]]', ' ', 'g')`
	title := fmt.Sprintf(col, "m.title")
	orig := fmt.Sprintf(col, "m.original_title")
	snippet = fmt.Sprintf("(%s ILIKE $%d OR %s ILIKE $%d)", title, n, orig, n)
	arg = "%" + norm + "%"
	return
}

// ─── Torrent cache ────────────────────────────────────────────────────────────

// TorrentStatus checks if a hash has been processed before.
// Returns (cached=false, "") if hash unknown.
// Returns (cached=true, "") if processed but TMDB not found (retry allowed).
// Returns (cached=true, cardID) if enriched successfully.
func TorrentStatus(hash string) (cached bool, cardID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var id *string
	err := postgres.Pool.QueryRow(ctx,
		`SELECT card_id FROM torrents WHERE hash = $1`, hash,
	).Scan(&id)
	if err != nil {
		return false, "" // not in table
	}
	if id == nil {
		return true, "" // processed but not found in TMDB
	}
	return true, *id
}

// CacheTorrent records a processed torrent hash with its linked card and tracker.
// On conflict: only upgrades card_id/tracker from NULL → real value, never clears them.
func CacheTorrent(hash, cardID, tracker string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var id *string
	if cardID != "" {
		id = &cardID
	}
	var tr *string
	if tracker != "" {
		tr = &tracker
	}
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`INSERT INTO torrents (hash, card_id, tracker) VALUES ($1, $2, $3)
		 ON CONFLICT (hash) DO UPDATE SET
		   card_id = COALESCE(torrents.card_id, EXCLUDED.card_id),
		   tracker = COALESCE(torrents.tracker, EXCLUDED.tracker)`,
		hash, id, tr,
	)
}

// CountCardsByTracker returns the number of distinct linked cards per tracker.
func CountCardsByTracker() map[string]int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := postgres.Pool.Query(ctx,
		`SELECT tracker, COUNT(DISTINCT card_id)
		 FROM torrents
		 WHERE tracker IS NOT NULL AND card_id IS NOT NULL
		 GROUP BY tracker`,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	result := map[string]int{}
	for rows.Next() {
		var tr string
		var cnt int
		if rows.Scan(&tr, &cnt) == nil {
			result[tr] = cnt
		}
	}
	return result
}

// ─── Parse timestamp ──────────────────────────────────────────────────────────

// LastParsedAtFor returns the last successful parse time for the given tracker,
// or zero time if never parsed. Key: {tracker}_last_parsed_at.
func LastParsedAtFor(tracker string) time.Time {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var val string
	err := postgres.Pool.QueryRow(ctx,
		`SELECT value FROM app_settings WHERE key = $1`,
		tracker+"_last_parsed_at",
	).Scan(&val)
	if err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return time.Time{}
	}
	return t
}

// SetLastParsedAtFor records the current time as the last successful parse for tracker.
func SetLastParsedAtFor(tracker string) {
	SetLastParsedAtTimeFor(tracker, time.Now().UTC())
}

// SetLastParsedAtTimeFor records a specific time as the last successful parse for tracker.
func SetLastParsedAtTimeFor(tracker string, t time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	val := t.UTC().Format(time.RFC3339)
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`INSERT INTO app_settings (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		tracker+"_last_parsed_at", val,
	)
}

// ResetLastParsedAtFor deletes the last_parsed_at record for tracker (triggers full scan).
func ResetLastParsedAtFor(tracker string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`DELETE FROM app_settings WHERE key = $1`,
		tracker+"_last_parsed_at",
	)
}

// LastParsedAt returns the last successful rutor parse time (legacy wrapper).
func LastParsedAt() time.Time { return LastParsedAtFor("rutor") }

// SetLastParsedAt records the current time as the last successful rutor parse (legacy wrapper).
func SetLastParsedAt() { SetLastParsedAtFor("rutor") }

// SetLastParsedAtTime records a specific time as the last successful rutor parse (legacy wrapper).
func SetLastParsedAtTime(t time.Time) { SetLastParsedAtTimeFor("rutor", t) }

// UpdateQuality bumps best_video_quality for an already-known torrent (no TMDB call).
// Uses GREATEST so quality never decreases.
func UpdateQuality(cardID string, quality int) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`UPDATE media_cards SET best_video_quality = GREATEST(best_video_quality, $2)
		 WHERE card_id = $1 AND best_video_quality < $2`,
		cardID, quality,
	)
}

// ─── Media card upsert ────────────────────────────────────────────────────────

// UpsertMediaCard inserts or updates a media_cards row from an enriched Entity.
// Also updates best_video_quality and latest_torrent_date from the linked torrent.
func UpsertMediaCard(e *models.Entity, t *models.TorrentDetails) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cardID := fmt.Sprintf("%d_%s", e.ID, e.MediaType)

	releaseDate := nilStr(fmtDate(e.ReleaseDate))
	firstAirDate := nilStr(fmtDate(e.FirstAirDate))
	lastAirDate := nilStr(fmtDate(e.LastAirDate))
	if e.MediaType == "tv" && firstAirDate == nil {
		firstAirDate = releaseDate
	}

	var lastEpSeason, lastEpNumber *int
	if e.LastEpisodeToAir != nil && e.LastEpisodeToAir.SeasonNumber > 0 {
		lastEpSeason = &e.LastEpisodeToAir.SeasonNumber
		lastEpNumber = &e.LastEpisodeToAir.EpisodeNumber
	}
	var episodeRunTime *int
	if len(e.EpisodeRunTime) > 0 && e.EpisodeRunTime[0] > 0 {
		episodeRunTime = &e.EpisodeRunTime[0]
	}

	category := t.Categories
	if category == "" {
		if e.MediaType == "tv" {
			category = models.CatSeries
		} else {
			category = models.CatMovie
		}
	}

	torrentDate := t.CreateDate
	if torrentDate.IsZero() {
		dateStr := e.ReleaseDate
		if e.MediaType == "tv" {
			dateStr = e.LastAirDate
		}
		if d, err := time.Parse("2006-01-02", fmtDate(dateStr)); err == nil && !d.After(time.Now()) {
			torrentDate = d
		}
	}

	_, err := postgres.Pool.Exec(ctx, `
		INSERT INTO media_cards
			(card_id, tmdb_id, media_type, title, original_title, overview,
			 poster_path, backdrop_path, release_date, first_air_date, last_air_date,
			 vote_average, vote_count, original_language, adult, runtime, status, imdb_id,
			 genres, number_of_seasons, number_of_episodes, seasons,
			 myshows_id, kinopoisk_id,
			 category, best_video_quality, latest_torrent_date,
			 last_ep_season, last_ep_number, episode_run_time,
			 tmdb_updated_at, updated_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,now(),now(),now())
		ON CONFLICT (card_id) DO UPDATE SET
			title              = EXCLUDED.title,
			original_title     = EXCLUDED.original_title,
			overview           = EXCLUDED.overview,
			poster_path        = EXCLUDED.poster_path,
			backdrop_path      = EXCLUDED.backdrop_path,
			release_date       = COALESCE(EXCLUDED.release_date, media_cards.release_date),
			first_air_date     = COALESCE(EXCLUDED.first_air_date, media_cards.first_air_date),
			last_air_date      = COALESCE(EXCLUDED.last_air_date, media_cards.last_air_date),
			vote_average       = EXCLUDED.vote_average,
			vote_count         = EXCLUDED.vote_count,
			original_language  = EXCLUDED.original_language,
			adult              = EXCLUDED.adult,
			runtime            = CASE WHEN EXCLUDED.runtime > 0 THEN EXCLUDED.runtime ELSE media_cards.runtime END,
			status             = EXCLUDED.status,
			imdb_id            = COALESCE(EXCLUDED.imdb_id, media_cards.imdb_id),
			genres             = EXCLUDED.genres,
			number_of_seasons  = COALESCE(EXCLUDED.number_of_seasons, media_cards.number_of_seasons),
			number_of_episodes = COALESCE(EXCLUDED.number_of_episodes, media_cards.number_of_episodes),
			seasons            = COALESCE(EXCLUDED.seasons, media_cards.seasons),
			myshows_id         = COALESCE(EXCLUDED.myshows_id, media_cards.myshows_id),
			kinopoisk_id       = COALESCE(EXCLUDED.kinopoisk_id, media_cards.kinopoisk_id),
			category           = COALESCE(EXCLUDED.category, media_cards.category),
			best_video_quality = GREATEST(media_cards.best_video_quality, EXCLUDED.best_video_quality),
			latest_torrent_date = CASE
				WHEN media_cards.media_type = 'tv'
					THEN GREATEST(media_cards.latest_torrent_date, EXCLUDED.latest_torrent_date)
				WHEN EXCLUDED.best_video_quality > media_cards.best_video_quality
					THEN EXCLUDED.latest_torrent_date
				ELSE media_cards.latest_torrent_date
			END,
			last_ep_season     = COALESCE(EXCLUDED.last_ep_season, media_cards.last_ep_season),
			last_ep_number     = COALESCE(EXCLUDED.last_ep_number, media_cards.last_ep_number),
			episode_run_time   = COALESCE(EXCLUDED.episode_run_time, media_cards.episode_run_time),
			tmdb_updated_at    = now(),
			updated_at         = now()`,
		cardID, e.ID, e.MediaType, e.Title, e.OriginalTitle, e.Overview,
		e.PosterPath, e.BackdropPath, releaseDate, firstAirDate, lastAirDate,
		e.VoteAverage, e.VoteCount, e.OriginalLanguage, e.Adult, e.Runtime, e.Status, e.ImdbID,
		marshalJSON(e.Genres), e.NumberOfSeasons, e.NumberOfEpisodes, marshalJSON(e.Seasons),
		nilInt(e.MyShowsID), nilInt64(e.KinopoiskID),
		nilStr(category), t.VideoQuality, nilTime(torrentDate),
		lastEpSeason, lastEpNumber, episodeRunTime,
	)
	if err != nil {
		log.Printf("store: upsert media_card tmdb=%d %s: %v", e.ID, e.MediaType, err)
	}
}

// RefreshCardTMDB обновляет только TMDB-поля карточки, не трогая торрент-данные.
// Вызывается из фоновой горутины при сохранении таймкода.
func RefreshCardTMDB(ctx context.Context, cardID string, e *models.Entity) {
	seasonsJSON := marshalJSON(e.Seasons)
	genresJSON := marshalJSON(e.Genres)

	var lastEpSeason, lastEpNumber *int
	if e.LastEpisodeToAir != nil && e.LastEpisodeToAir.SeasonNumber > 0 {
		lastEpSeason = &e.LastEpisodeToAir.SeasonNumber
		lastEpNumber = &e.LastEpisodeToAir.EpisodeNumber
	}
	var episodeRunTime *int
	if len(e.EpisodeRunTime) > 0 && e.EpisodeRunTime[0] > 0 {
		episodeRunTime = &e.EpisodeRunTime[0]
	}

	var runtimeArg *int
	if e.Runtime > 0 {
		runtimeArg = &e.Runtime
	}

	_, err := postgres.Pool.Exec(ctx, `
		UPDATE media_cards SET
			title              = $1,
			original_title     = $2,
			overview           = $3,
			poster_path        = $4,
			backdrop_path      = $5,
			vote_average       = $6,
			vote_count         = $7,
			status             = $8,
			genres             = $9,
			number_of_seasons  = COALESCE($10, number_of_seasons),
			number_of_episodes = COALESCE($11, number_of_episodes),
			seasons            = COALESCE($12, seasons),
			last_ep_season     = COALESCE($13, last_ep_season),
			last_ep_number     = COALESCE($14, last_ep_number),
			episode_run_time   = COALESCE($15, episode_run_time),
			runtime            = COALESCE($16, runtime),
			tmdb_updated_at    = now(),
			updated_at         = now()
		WHERE card_id = $17`,
		e.Title, e.OriginalTitle, e.Overview, e.PosterPath, e.BackdropPath,
		e.VoteAverage, e.VoteCount, e.Status,
		genresJSON,
		nilIntFromInt(e.NumberOfSeasons), nilIntFromInt(e.NumberOfEpisodes), seasonsJSON,
		lastEpSeason, lastEpNumber, episodeRunTime,
		runtimeArg,
		cardID,
	)
	if err != nil {
		log.Printf("store: refresh card tmdb %s: %v", cardID, err)
	}
}

func nilIntFromInt(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

// ─── Category listing ─────────────────────────────────────────────────────────

const defaultPageSize = 20

// MediaRow is a joined result from media_cards.
type MediaRow struct {
	TmdbID            int64
	MediaType         string
	Title             string
	OriginalTitle     string
	Overview          string
	PosterPath        string
	BackdropPath      string
	ReleaseDate       *string
	FirstAirDate      *string
	LastAirDate       *string
	VoteAverage       float64
	VoteCount         int
	OriginalLanguage  *string
	Adult             bool
	Status            *string
	NumberOfSeasons   *int
	Seasons           []byte
	LastEpSeason      *int
	LastEpNumber      *int
	UpdatedAt         time.Time
	VideoQuality      int
	AudioQuality      int
	LatestTorrentDate *time.Time
}

// CategoryFilter defines how to filter and sort a category listing.
type CategoryFilter struct {
	MediaTypes      []string // "movie", "tv"
	Categories      []string // category values e.g. "Movie", "Series"
	Language        string   // "ru", "notru", or ""
	MinVideoQuality int
	MaxVideoQuality int
	MinVoteCount    int
	OrderByRating   bool
	Child           bool
	Year            int    // exact release year filter
	TrackerFilter   []string // if non-empty, only show cards linked to at least one of these trackers
	NewOnly         bool   // only items released within last YearDelta years AND quality >= 200
	OldOnly         bool   // only items released more than YearDelta years ago (complement of NewOnly)
	YearDelta       int    // years window for NewOnly/OldOnly (default 2, use 4 for 4K)
	Page            int
	PerPage         int
	Search          string
	HideWatched     bool
	DeviceID        int64
	ProfileID       string
	WatchedPercent  int
}

// ListCategory returns a page of media_cards matching the filter.
func ListCategory(f CategoryFilter) (rows []MediaRow, total int) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if f.Page < 1 {
		f.Page = 1
	}
	perPage := f.PerPage
	if perPage < 1 {
		perPage = defaultPageSize
	}
	offset := (f.Page - 1) * perPage

	var where []string
	var args []interface{}
	n := 1

	if f.NewOnly || f.OldOnly {
		delta := f.YearDelta
		if delta < 1 {
			delta = 2
		}
		// NUMParser: |year - currentYear| < delta  →  year >= currentYear - delta + 1
		cutoffStr := fmt.Sprintf("%d", time.Now().Year()-delta+1)
		if f.NewOnly {
			where = append(where, fmt.Sprintf(
				"LEFT(COALESCE(m.release_date::text, m.first_air_date::text, ''), 4) >= $%d", n))
			args = append(args, cutoffStr)
			n++
		}
		if f.OldOnly {
			where = append(where, fmt.Sprintf(
				"(COALESCE(m.release_date::text, m.first_air_date::text, '') = '' OR "+
					"LEFT(COALESCE(m.release_date::text, m.first_air_date::text), 4) < $%d)", n))
			args = append(args, cutoffStr)
			n++
		}
	}

	if len(f.MediaTypes) > 0 {
		placeholders := make([]string, len(f.MediaTypes))
		for i, mt := range f.MediaTypes {
			placeholders[i] = fmt.Sprintf("$%d", n)
			args = append(args, mt)
			n++
		}
		where = append(where, "m.media_type IN ("+strings.Join(placeholders, ",")+")")
	}
	switch f.Language {
	case "ru":
		where = append(where, "m.original_language = 'ru'")
	case "notru":
		where = append(where, "m.original_language <> 'ru'")
	}
	if f.MinVideoQuality > 0 {
		where = append(where, fmt.Sprintf("m.best_video_quality >= $%d", n))
		args = append(args, f.MinVideoQuality)
		n++
	}
	if f.MaxVideoQuality > 0 {
		where = append(where, fmt.Sprintf("m.best_video_quality <= $%d", n))
		args = append(args, f.MaxVideoQuality)
		n++
	}
	if f.Child {
		where = append(where, "m.adult = false")
		where = append(where, "(m.age_rating IS NULL OR m.age_rating <= 12)")
		where = append(where, "NOT (m.genres @> '[{\"id\":27}]' OR m.genres @> '[{\"id\":53}]' OR m.genres @> '[{\"id\":80}]')")
	}
	if f.Year > 0 {
		where = append(where, fmt.Sprintf("LEFT(COALESCE(m.release_date::text, m.first_air_date::text, ''), 4) = $%d", n))
		args = append(args, fmt.Sprintf("%d", f.Year))
		n++
	}
	if f.MinVoteCount > 0 {
		where = append(where, fmt.Sprintf("m.vote_count >= $%d", n))
		args = append(args, f.MinVoteCount)
		n++
	}
	if len(f.TrackerFilter) > 0 {
		where = append(where, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM torrents t WHERE t.card_id = m.card_id AND t.tracker = ANY($%d))", n))
		args = append(args, f.TrackerFilter)
		n++
	}
	if len(f.Categories) > 0 {
		placeholders := make([]string, len(f.Categories))
		for i, cat := range f.Categories {
			placeholders[i] = fmt.Sprintf("$%d", n)
			args = append(args, cat)
			n++
		}
		where = append(where, "m.category IN ("+strings.Join(placeholders, ",")+")")
	}
	if f.Search != "" {
		snip, arg := searchSQL(f.Search, n)
		where = append(where, snip)
		args = append(args, arg)
		n++
	}
	if f.HideWatched && f.DeviceID > 0 {
		where = append(where, fmt.Sprintf(`NOT (
			CASE m.media_type
			WHEN 'movie' THEN
				EXISTS (
					SELECT 1 FROM timecodes tc
					WHERE tc.device_id = $%d
					  AND tc.profile_id = $%d
					  AND tc.card_id = (m.tmdb_id::text || '_' || m.media_type)
					  AND (tc.data::jsonb->>'percent')::numeric >= $%d
				)
			ELSE
				(
					SELECT COUNT(*) FILTER (
						WHERE (tc.data::jsonb->>'percent')::numeric >= $%d
						   OR (tc.data::jsonb->>'special')::boolean IS TRUE
					)
					FROM timecodes tc
					WHERE tc.device_id = $%d
					  AND tc.profile_id = $%d
					  AND tc.card_id = (m.tmdb_id::text || '_' || m.media_type)
				) >= GREATEST(1, COALESCE(
					(SELECT COUNT(*)::int FROM episodes e
					 WHERE e.tmdb_show_id = m.tmdb_id
					   AND NOT e.is_special
					   AND e.air_date IS NOT NULL AND e.air_date <= CURRENT_DATE),
					m.number_of_episodes
				))
			END
		)`, n, n+1, n+2, n+2, n, n+1))
		args = append(args, f.DeviceID, f.ProfileID, f.WatchedPercent)
		n += 3
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	orderBy := "m.latest_torrent_date DESC NULLS LAST, m.created_at DESC"
	if f.OrderByRating {
		orderBy = "m.vote_average DESC NULLS LAST, m.vote_count DESC NULLS LAST, m.created_at DESC"
	} else if f.OldOnly || f.Year > 0 {
		// Archive / year categories: sort by release/air date descending
		orderBy = "COALESCE(m.release_date, m.first_air_date) DESC NULLS LAST, m.created_at DESC"
	}

	countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM media_cards m %s`, whereClause)
	if err := postgres.Pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		log.Printf("store: count category: %v", err)
		return
	}

	dataSQL := fmt.Sprintf(`
		SELECT m.tmdb_id, m.media_type, m.title, m.original_title,
			m.overview, m.poster_path, m.backdrop_path,
			m.release_date::text, m.first_air_date::text, m.last_air_date::text,
			m.vote_average, m.vote_count, m.original_language, m.adult, m.status,
			m.number_of_seasons, m.seasons, m.last_ep_season, m.last_ep_number, m.updated_at,
			m.best_video_quality, m.latest_torrent_date
		FROM media_cards m
		%s
		ORDER BY %s
		LIMIT %d OFFSET %d`, whereClause, orderBy, perPage, offset)

	qrows, err := postgres.Pool.Query(ctx, dataSQL, args...)
	if err != nil {
		log.Printf("store: query category: %v", err)
		return
	}
	defer qrows.Close()

	for qrows.Next() {
		var r MediaRow
		if err := qrows.Scan(
			&r.TmdbID, &r.MediaType, &r.Title, &r.OriginalTitle,
			&r.Overview, &r.PosterPath, &r.BackdropPath,
			&r.ReleaseDate, &r.FirstAirDate, &r.LastAirDate,
			&r.VoteAverage, &r.VoteCount, &r.OriginalLanguage, &r.Adult, &r.Status,
			&r.NumberOfSeasons, &r.Seasons, &r.LastEpSeason, &r.LastEpNumber, &r.UpdatedAt,
			&r.VideoQuality, &r.LatestTorrentDate,
		); err != nil {
			log.Printf("store: scan row: %v", err)
			continue
		}
		rows = append(rows, r)
	}
	return
}

// ─── Search ───────────────────────────────────────────────────────────────────

func SearchMedia(query string, limit int) []MediaRow {
	if limit <= 0 {
		limit = 20
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	qrows, err := postgres.Pool.Query(ctx, `
		SELECT m.tmdb_id, m.media_type, m.title, m.original_title,
			m.overview, m.poster_path, m.backdrop_path,
			m.release_date::text, m.first_air_date::text, m.last_air_date::text,
			m.vote_average, m.vote_count, m.original_language, m.adult, m.status,
			m.number_of_seasons, m.seasons, m.last_ep_season, m.last_ep_number, m.updated_at,
			m.best_video_quality, m.latest_torrent_date
		FROM media_cards m
		WHERE REGEXP_REPLACE(LOWER(m.title), '[-''.,;:!?()\[\]]', ' ', 'g') ILIKE $1
		   OR REGEXP_REPLACE(LOWER(m.original_title), '[-''.,;:!?()\[\]]', ' ', 'g') ILIKE $1
		ORDER BY m.vote_count DESC
		LIMIT $2`,
		"%"+normalizeSearch(query)+"%", limit,
	)
	if err != nil {
		log.Printf("store: search %q: %v", query, err)
		return nil
	}
	defer qrows.Close()

	var result []MediaRow
	for qrows.Next() {
		var r MediaRow
		if err := qrows.Scan(
			&r.TmdbID, &r.MediaType, &r.Title, &r.OriginalTitle,
			&r.Overview, &r.PosterPath, &r.BackdropPath,
			&r.ReleaseDate, &r.FirstAirDate, &r.LastAirDate,
			&r.VoteAverage, &r.VoteCount, &r.OriginalLanguage, &r.Adult, &r.Status,
			&r.NumberOfSeasons, &r.Seasons, &r.LastEpSeason, &r.LastEpNumber, &r.UpdatedAt,
			&r.VideoQuality, &r.LatestTorrentDate,
		); err != nil {
			continue
		}
		result = append(result, r)
	}
	return result
}

// ─── Media card reads ─────────────────────────────────────────────────────────

func GetMediaCard(tmdbID int64, mediaType string) *models.Entity {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var e models.Entity
	var genresRaw, seasonsRaw []byte
	var releaseDate, firstAirDate, lastAirDate *string

	err := postgres.Pool.QueryRow(ctx, `
		SELECT tmdb_id, media_type, title, original_title, overview,
		       poster_path, backdrop_path, release_date::text, first_air_date::text, last_air_date::text,
		       vote_average, vote_count, original_language, adult, runtime, status, imdb_id,
		       genres, number_of_seasons, number_of_episodes, seasons,
		       myshows_id, kinopoisk_id
		FROM media_cards WHERE tmdb_id = $1 AND media_type = $2`,
		tmdbID, mediaType,
	).Scan(
		&e.ID, &e.MediaType, &e.Title, &e.OriginalTitle, &e.Overview,
		&e.PosterPath, &e.BackdropPath, &releaseDate, &firstAirDate, &lastAirDate,
		&e.VoteAverage, &e.VoteCount, &e.OriginalLanguage, &e.Adult, &e.Runtime, &e.Status, &e.ImdbID,
		&genresRaw, &e.NumberOfSeasons, &e.NumberOfEpisodes, &seasonsRaw,
		&e.MyShowsID, &e.KinopoiskID,
	)
	if err != nil {
		return nil
	}
	if releaseDate != nil {
		e.ReleaseDate = *releaseDate
	}
	if firstAirDate != nil {
		e.FirstAirDate = *firstAirDate
	}
	if lastAirDate != nil {
		e.LastAirDate = *lastAirDate
	}
	if genresRaw != nil {
		json.Unmarshal(genresRaw, &e.Genres) //nolint:errcheck
	}
	if seasonsRaw != nil {
		json.Unmarshal(seasonsRaw, &e.Seasons) //nolint:errcheck
	}
	return &e
}

func FindByIMDB(imdbID string) *models.Entity {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var tmdbID int64
	var mediaType string
	err := postgres.Pool.QueryRow(ctx,
		`SELECT tmdb_id, media_type FROM media_cards WHERE imdb_id = $1 LIMIT 1`,
		imdbID,
	).Scan(&tmdbID, &mediaType)
	if err != nil {
		return nil
	}
	return GetMediaCard(tmdbID, mediaType)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func marshalJSON(v any) []byte {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

func fmtDate(s string) string {
	if len(s) < 10 {
		return ""
	}
	for _, layout := range []string{"2006-01-02", "02.01.2006"} {
		if t, err := time.Parse(layout, s[:10]); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}

func nilStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nilInt(v int) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

func nilInt64(v int64) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

func nilTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return t
}
