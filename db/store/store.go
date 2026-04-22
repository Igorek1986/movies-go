// Package store provides PostgreSQL-backed storage operations.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"lampa-api/db/models"
	"lampa-api/db/postgres"
	"log"
	"strings"
	"time"
)

// ─── Torrent cache ────────────────────────────────────────────────────────────

// TorrentCached returns true if we have already attempted TMDB lookup for this hash.
func TorrentCached(hash string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var exists bool
	postgres.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM torrents WHERE hash = $1)`, hash,
	).Scan(&exists) //nolint:errcheck
	return exists
}

// CacheTorrent marks this torrent hash as already processed.
func CacheTorrent(hash string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`INSERT INTO torrents (hash) VALUES ($1) ON CONFLICT (hash) DO NOTHING`,
		hash,
	)
}

// ─── Parse timestamp ──────────────────────────────────────────────────────────

// LastParsedAt returns the time of the last successful rutor parse, or zero time if never.
func LastParsedAt() time.Time {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var val string
	err := postgres.Pool.QueryRow(ctx,
		`SELECT value FROM app_settings WHERE key = 'rutor_last_parsed_at'`,
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

// SetLastParsedAt records the current time as the last successful rutor parse.
func SetLastParsedAt() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	val := time.Now().UTC().Format(time.RFC3339)
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`INSERT INTO app_settings (key, value) VALUES ('rutor_last_parsed_at', $1)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		val,
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

	_, err := postgres.Pool.Exec(ctx, `
		INSERT INTO media_cards
			(card_id, tmdb_id, media_type, title, original_title, overview,
			 poster_path, backdrop_path, release_date, first_air_date, last_air_date,
			 vote_average, vote_count, original_language, adult, runtime, status, imdb_id,
			 genres, number_of_seasons, number_of_episodes, seasons,
			 myshows_id, kinopoisk_id,
			 rutor_category, best_video_quality, latest_torrent_date,
			 tmdb_updated_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,now(),now())
		ON CONFLICT (tmdb_id, media_type) DO UPDATE SET
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
			runtime            = EXCLUDED.runtime,
			status             = EXCLUDED.status,
			imdb_id            = COALESCE(EXCLUDED.imdb_id, media_cards.imdb_id),
			genres             = EXCLUDED.genres,
			number_of_seasons  = EXCLUDED.number_of_seasons,
			number_of_episodes = EXCLUDED.number_of_episodes,
			seasons            = EXCLUDED.seasons,
			myshows_id         = COALESCE(EXCLUDED.myshows_id, media_cards.myshows_id),
			kinopoisk_id       = COALESCE(EXCLUDED.kinopoisk_id, media_cards.kinopoisk_id),
			rutor_category     = COALESCE(EXCLUDED.rutor_category, media_cards.rutor_category),
			best_video_quality = GREATEST(media_cards.best_video_quality, EXCLUDED.best_video_quality),
			latest_torrent_date = GREATEST(media_cards.latest_torrent_date, EXCLUDED.latest_torrent_date),
			tmdb_updated_at    = now(),
			updated_at         = now()`,
		cardID, e.ID, e.MediaType, e.Title, e.OriginalTitle, e.Overview,
		e.PosterPath, e.BackdropPath, releaseDate, firstAirDate, lastAirDate,
		e.VoteAverage, e.VoteCount, e.OriginalLanguage, e.Adult, e.Runtime, e.Status, e.ImdbID,
		marshalJSON(e.Genres), e.NumberOfSeasons, e.NumberOfEpisodes, marshalJSON(e.Seasons),
		nilInt(e.MyShowsID), nilInt64(e.KinopoiskID),
		nilStr(t.Categories), t.VideoQuality, nilTime(t.CreateDate),
	)
	if err != nil {
		log.Printf("store: upsert media_card tmdb=%d %s: %v", e.ID, e.MediaType, err)
	}
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
	Categories      []string // rutor_category values e.g. "Movie", "Series"
	Language        string   // "ru", "notru", or ""
	MinVideoQuality int
	MaxVideoQuality int
	MinVoteCount    int
	OrderByNew      bool
	OrderByRating   bool
	Child           bool
	Year            int
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
		where = append(where, fmt.Sprintf("EXTRACT(YEAR FROM COALESCE(m.release_date, m.first_air_date)) = $%d", n))
		args = append(args, f.Year)
		n++
	}
	if f.MinVoteCount > 0 {
		where = append(where, fmt.Sprintf("m.vote_count >= $%d", n))
		args = append(args, f.MinVoteCount)
		n++
	}
	if len(f.Categories) > 0 {
		placeholders := make([]string, len(f.Categories))
		for i, cat := range f.Categories {
			placeholders[i] = fmt.Sprintf("$%d", n)
			args = append(args, cat)
			n++
		}
		where = append(where, "m.rutor_category IN ("+strings.Join(placeholders, ",")+")")
	}
	if f.Search != "" {
		where = append(where, fmt.Sprintf("(m.title ILIKE $%d OR m.original_title ILIKE $%d)", n, n))
		args = append(args, "%"+f.Search+"%")
		n++
	}
	if f.HideWatched && f.DeviceID > 0 {
		where = append(where, fmt.Sprintf(`NOT EXISTS (
			SELECT 1 FROM timecodes tc
			WHERE tc.device_id = $%d
			  AND tc.lampa_profile_id = $%d
			  AND tc.card_id = (m.tmdb_id::text || '_' || m.media_type)
			  AND (tc.data::jsonb->>'percent')::numeric >= $%d
		)`, n, n+1, n+2))
		args = append(args, f.DeviceID, f.ProfileID, f.WatchedPercent)
		n += 3
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	orderBy := "m.release_date DESC"
	if f.OrderByNew {
		orderBy = "m.latest_torrent_date DESC"
	} else if f.OrderByRating {
		orderBy = "m.vote_average DESC, m.vote_count DESC"
	}

	countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM media_cards m %s`, whereClause)
	if err := postgres.Pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		log.Printf("store: count category: %v", err)
		return
	}

	dataSQL := fmt.Sprintf(`
		SELECT m.tmdb_id, m.media_type, m.title, m.original_title,
			m.overview, m.poster_path, m.backdrop_path,
			m.release_date, m.first_air_date, m.last_air_date,
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
			m.release_date, m.first_air_date, m.last_air_date,
			m.vote_average, m.vote_count, m.original_language, m.adult, m.status,
			m.number_of_seasons, m.seasons, m.last_ep_season, m.last_ep_number, m.updated_at,
			m.best_video_quality, m.latest_torrent_date
		FROM media_cards m
		WHERE m.title ILIKE $1 OR m.original_title ILIKE $1
		ORDER BY m.vote_count DESC
		LIMIT $2`,
		"%"+query+"%", limit,
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
		       poster_path, backdrop_path, release_date, first_air_date, last_air_date,
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
