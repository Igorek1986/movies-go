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

	"github.com/jackc/pgx/v5"
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

// CacheTorrent records a processed torrent hash with its linked card, tracker and tracker date.
// On conflict: only upgrades card_id/tracker from NULL → real value; created_at is never changed.
func CacheTorrent(hash, cardID, tracker string, createDate time.Time) {
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
	var cd *time.Time
	if !createDate.IsZero() {
		cd = &createDate
	}
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`INSERT INTO torrents (hash, card_id, tracker, created_at) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (hash) DO UPDATE SET
		   card_id    = COALESCE(torrents.card_id, EXCLUDED.card_id),
		   tracker    = COALESCE(torrents.tracker, EXCLUDED.tracker),
		   created_at = COALESCE(torrents.created_at, EXCLUDED.created_at)`,
		hash, id, tr, cd,
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
			 certification_ru, certification_us, keyword_ids,
			 tmdb_updated_at, updated_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,now(),now(),now())
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
			certification_ru   = COALESCE(EXCLUDED.certification_ru, media_cards.certification_ru),
			certification_us   = COALESCE(EXCLUDED.certification_us, media_cards.certification_us),
			keyword_ids        = COALESCE(EXCLUDED.keyword_ids, media_cards.keyword_ids),
			tmdb_updated_at    = now(),
			updated_at         = now()`,
		cardID, e.ID, e.MediaType, e.Title, e.OriginalTitle, e.Overview,
		e.PosterPath, e.BackdropPath, releaseDate, firstAirDate, lastAirDate,
		e.VoteAverage, e.VoteCount, e.OriginalLanguage, e.Adult, e.Runtime, e.Status, e.ImdbID,
		marshalJSON(e.Genres), e.NumberOfSeasons, e.NumberOfEpisodes, marshalJSON(e.Seasons),
		nilInt(e.MyShowsID), nilInt64(e.KinopoiskID),
		nilStr(category), t.VideoQuality, nilTime(torrentDate),
		lastEpSeason, lastEpNumber, episodeRunTime,
		nilStr(e.CertificationRU), nilStr(e.CertificationUS),
		nilIntSlice(e.KeywordIDs),
	)
	if err != nil {
		log.Printf("store: upsert media_card tmdb=%d %s: %v", e.ID, e.MediaType, err)
	}
	if e.Credits != nil {
		go UpsertCast(context.Background(), cardID, e.Credits.Cast)
		go UpsertCrew(context.Background(), cardID, e.Credits.Crew)
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
			certification_ru   = COALESCE($17, certification_ru),
			certification_us   = COALESCE($18, certification_us),
			keyword_ids        = COALESCE($19, keyword_ids),
			tmdb_updated_at    = now(),
			updated_at         = now()
		WHERE card_id = $20`,
		e.Title, e.OriginalTitle, e.Overview, e.PosterPath, e.BackdropPath,
		e.VoteAverage, e.VoteCount, e.Status,
		genresJSON,
		nilIntFromInt(e.NumberOfSeasons), nilIntFromInt(e.NumberOfEpisodes), seasonsJSON,
		lastEpSeason, lastEpNumber, episodeRunTime,
		runtimeArg,
		nilStr(e.CertificationRU), nilStr(e.CertificationUS),
		nilIntSlice(e.KeywordIDs),
		cardID,
	)
	if err != nil {
		log.Printf("store: refresh card tmdb %s: %v", cardID, err)
	}
	if e.Credits != nil {
		UpsertCast(ctx, cardID, e.Credits.Cast)
		UpsertCrew(ctx, cardID, e.Credits.Crew)
	}
}

func MarkCardTMDBNotFound(ctx context.Context, cardID string) {
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`UPDATE media_cards SET tmdb_not_found_at = now() WHERE card_id = $1`, cardID)
}

func ClearCardTMDBNotFound(ctx context.Context, cardID string) {
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`UPDATE media_cards SET tmdb_not_found_at = NULL WHERE card_id = $1`, cardID)
}

type TMDBMissingCard struct {
	CardID          string  `json:"card_id"`
	TmdbID          int64   `json:"tmdb_id"`
	MediaType       string  `json:"media_type"`
	Title           string  `json:"title"`
	OriginalTitle   string  `json:"original_title"`
	ReleaseDate     string  `json:"release_date"`
	VoteAverage     float64 `json:"vote_average"`
	VoteCount       int     `json:"vote_count"`
	NotFoundAt      string  `json:"not_found_at"`
}

func GetTMDBMissingCards(ctx context.Context) []TMDBMissingCard {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT card_id, tmdb_id, media_type, title, original_title,
		       COALESCE(LEFT(COALESCE(release_date::text, first_air_date::text, ''), 4), '') AS year,
		       vote_average, vote_count, tmdb_not_found_at
		FROM media_cards
		WHERE tmdb_not_found_at IS NOT NULL
		ORDER BY tmdb_not_found_at DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []TMDBMissingCard
	for rows.Next() {
		var c TMDBMissingCard
		var notFoundAt *time.Time
		if rows.Scan(&c.CardID, &c.TmdbID, &c.MediaType, &c.Title, &c.OriginalTitle,
			&c.ReleaseDate, &c.VoteAverage, &c.VoteCount, &notFoundAt) == nil {
			if notFoundAt != nil {
				c.NotFoundAt = notFoundAt.Format("2006-01-02")
			}
			out = append(out, c)
		}
	}
	return out
}

type NewTodayCard struct {
	CardID          string   `json:"card_id"`
	TmdbID          int64    `json:"tmdb_id"`
	MediaType       string   `json:"media_type"`
	Title           string   `json:"title"`
	OriginalTitle   string   `json:"original_title"`
	Year            string   `json:"year"`
	VoteAverage     float64  `json:"vote_average"`
	VoteCount       int      `json:"vote_count"`
	CreatedAt       string   `json:"created_at"`
	Trackers        string   `json:"trackers"`
	Language        string   `json:"language"`
	Runtime         int      `json:"runtime"`
	EpisodeRunTime  int      `json:"episode_run_time"`
	BestVideoQuality  int    `json:"best_video_quality"`
	Category          string `json:"category"`
	Categories        []string `json:"categories"`
	LatestTorrentDate string `json:"latest_torrent_date"`
	ReleaseDate       string `json:"release_date"`
}

func GetNewTodayCards(ctx context.Context) []NewTodayCard {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT mc.card_id, mc.tmdb_id, mc.media_type, mc.title, mc.original_title,
		       COALESCE(LEFT(COALESCE(mc.release_date::text, mc.first_air_date::text, ''), 4), '') AS year,
		       mc.vote_average, mc.vote_count, mc.created_at,
		       COALESCE(STRING_AGG(DISTINCT t.tracker, ',' ORDER BY t.tracker), '') AS trackers,
		       COALESCE(mc.original_language, '') AS language,
		       COALESCE(mc.runtime, 0), COALESCE(mc.episode_run_time, 0),
		       COALESCE(mc.best_video_quality, 0), COALESCE(mc.category, ''),
		       COALESCE(mc.latest_torrent_date::text, ''),
		       COALESCE(COALESCE(mc.release_date::text, mc.first_air_date::text), '')
		FROM media_cards mc
		LEFT JOIN torrents t ON t.card_id = mc.card_id
		WHERE mc.created_at::date = CURRENT_DATE
		GROUP BY mc.card_id, mc.tmdb_id, mc.media_type, mc.title, mc.original_title,
		         mc.release_date, mc.first_air_date, mc.vote_average, mc.vote_count, mc.created_at,
		         mc.original_language, mc.runtime, mc.episode_run_time, mc.best_video_quality, mc.category,
		         mc.latest_torrent_date
		ORDER BY mc.created_at DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []NewTodayCard
	for rows.Next() {
		var c NewTodayCard
		var createdAt time.Time
		if rows.Scan(&c.CardID, &c.TmdbID, &c.MediaType, &c.Title, &c.OriginalTitle,
			&c.Year, &c.VoteAverage, &c.VoteCount, &createdAt, &c.Trackers, &c.Language,
			&c.Runtime, &c.EpisodeRunTime, &c.BestVideoQuality, &c.Category,
			&c.LatestTorrentDate, &c.ReleaseDate) == nil {
			c.CreatedAt = createdAt.Format("15:04")
			c.Categories = cardCategories(c)
			out = append(out, c)
		}
	}
	if out == nil {
		out = []NewTodayCard{}
	}
	return out
}

// AllCardsParams holds server-side filter/sort/page params for GetAllCards.
type AllCardsParams struct {
	Page            int
	PerPage         int
	Search          string
	MediaType       []string // display: "Фильм" / "Сериал"
	Year            []string
	Language        []string // display: uppercase e.g. "RU"; "—" means empty
	Trackers        []string
	RuntimeMin      *int
	RuntimeMax      *int
	NoRuntime       string // "movie" or "tv": cards missing runtime / episode_run_time
	TorrentDateFrom string
	TorrentDateTo   string
	ReleaseDateFrom string
	ReleaseDateTo   string
	SortBy          string
	SortDir         string
}

// AllCardsResult is the paginated response for GetAllCards.
type AllCardsResult struct {
	Cards []NewTodayCard `json:"cards"`
	Total int            `json:"total"`
}

// AllCardsDistinct holds distinct filter values for the all-cards page.
type AllCardsDistinct struct {
	MediaType [][]any `json:"media_type"`
	Year      [][]any `json:"year"`
	Language  [][]any `json:"language"`
	Trackers  [][]any `json:"trackers"`
}

func GetAllCards(ctx context.Context, p AllCardsParams) AllCardsResult {
	if p.PerPage <= 0 {
		p.PerPage = 100
	}
	if p.Page <= 0 {
		p.Page = 1
	}
	sortColMap := map[string]string{
		"latest_torrent_date": "mc.latest_torrent_date",
		"release_date":        "mc.release_date",
		"created_at":          "mc.created_at",
	}
	sortCol := sortColMap[p.SortBy]
	if sortCol == "" {
		sortCol = "mc.latest_torrent_date"
	}
	if p.SortDir != "asc" {
		p.SortDir = "desc"
	}

	var args []any
	var conds []string
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if p.Search != "" {
		ph := arg("%" + strings.ToLower(p.Search) + "%")
		conds = append(conds, fmt.Sprintf("(LOWER(mc.title) LIKE %s OR LOWER(mc.original_title) LIKE %s)", ph, ph))
	}

	if len(p.MediaType) > 0 {
		dbTypes := make([]string, 0, len(p.MediaType))
		for _, t := range p.MediaType {
			switch t {
			case "Фильм":
				dbTypes = append(dbTypes, "movie")
			case "Сериал":
				dbTypes = append(dbTypes, "tv")
			}
		}
		if len(dbTypes) > 0 {
			conds = append(conds, fmt.Sprintf("mc.media_type = ANY(%s)", arg(dbTypes)))
		}
	}

	if len(p.Year) > 0 {
		var parts []string
		var nonDash []string
		for _, y := range p.Year {
			if y == "—" {
				parts = append(parts, "COALESCE(mc.release_date::text, mc.first_air_date::text, '') = ''")
			} else {
				nonDash = append(nonDash, y)
			}
		}
		if len(nonDash) > 0 {
			parts = append(parts, fmt.Sprintf(
				"LEFT(COALESCE(mc.release_date::text, mc.first_air_date::text, ''), 4) = ANY(%s)", arg(nonDash)))
		}
		if len(parts) > 0 {
			conds = append(conds, "("+strings.Join(parts, " OR ")+")")
		}
	}

	if len(p.Language) > 0 {
		var parts []string
		var langs []string
		for _, l := range p.Language {
			if l == "—" {
				parts = append(parts, "COALESCE(mc.original_language, '') = ''")
			} else {
				langs = append(langs, strings.ToLower(l))
			}
		}
		if len(langs) > 0 {
			parts = append(parts, fmt.Sprintf("LOWER(COALESCE(mc.original_language, '')) = ANY(%s)", arg(langs)))
		}
		if len(parts) > 0 {
			conds = append(conds, "("+strings.Join(parts, " OR ")+")")
		}
	}

	if len(p.Trackers) > 0 {
		conds = append(conds, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM torrents t2 WHERE t2.card_id = mc.card_id AND t2.tracker = ANY(%s))", arg(p.Trackers)))
	}

	if p.RuntimeMin != nil {
		conds = append(conds, fmt.Sprintf(
			"CASE WHEN mc.media_type='movie' THEN COALESCE(mc.runtime,0) ELSE COALESCE(mc.episode_run_time,0) END >= %s", arg(*p.RuntimeMin)))
	}
	if p.RuntimeMax != nil {
		conds = append(conds, fmt.Sprintf(
			"CASE WHEN mc.media_type='movie' THEN COALESCE(mc.runtime,0) ELSE COALESCE(mc.episode_run_time,0) END <= %s", arg(*p.RuntimeMax)))
	}

	switch p.NoRuntime {
	case "movie":
		conds = append(conds, "mc.media_type='movie' AND (mc.runtime IS NULL OR mc.runtime=0)")
	case "tv":
		conds = append(conds, "mc.media_type='tv' AND (mc.episode_run_time IS NULL OR mc.episode_run_time=0)")
	}

	if p.TorrentDateFrom != "" {
		conds = append(conds, fmt.Sprintf("mc.latest_torrent_date >= %s", arg(p.TorrentDateFrom)))
	}
	if p.TorrentDateTo != "" {
		conds = append(conds, fmt.Sprintf("mc.latest_torrent_date <= %s", arg(p.TorrentDateTo)))
	}
	if p.ReleaseDateFrom != "" {
		conds = append(conds, fmt.Sprintf("COALESCE(mc.release_date, mc.first_air_date) >= %s", arg(p.ReleaseDateFrom)))
	}
	if p.ReleaseDateTo != "" {
		conds = append(conds, fmt.Sprintf("COALESCE(mc.release_date, mc.first_air_date) <= %s", arg(p.ReleaseDateTo)))
	}

	whereSQL := ""
	if len(conds) > 0 {
		whereSQL = "WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	postgres.Pool.QueryRow(ctx, //nolint:errcheck
		fmt.Sprintf("SELECT COUNT(*) FROM media_cards mc %s", whereSQL), args...).Scan(&total)

	if total == 0 {
		return AllCardsResult{Cards: []NewTodayCard{}, Total: 0}
	}

	offset := (p.Page - 1) * p.PerPage
	limitPh := arg(p.PerPage)
	offsetPh := arg(offset)

	mainSQL := fmt.Sprintf(`
		SELECT mc.card_id, mc.tmdb_id, mc.media_type, mc.title, mc.original_title,
		       COALESCE(LEFT(COALESCE(mc.release_date::text, mc.first_air_date::text, ''), 4), '') AS year,
		       mc.vote_average, mc.vote_count, mc.created_at,
		       COALESCE(STRING_AGG(DISTINCT t.tracker, ',' ORDER BY t.tracker), '') AS trackers,
		       COALESCE(mc.original_language, '') AS language,
		       COALESCE(mc.runtime, 0), COALESCE(mc.episode_run_time, 0),
		       COALESCE(mc.best_video_quality, 0), COALESCE(mc.category, ''),
		       COALESCE(mc.latest_torrent_date::text, ''),
		       COALESCE(COALESCE(mc.release_date::text, mc.first_air_date::text), '')
		FROM media_cards mc
		LEFT JOIN torrents t ON t.card_id = mc.card_id
		%s
		GROUP BY mc.card_id, mc.tmdb_id, mc.media_type, mc.title, mc.original_title,
		         mc.release_date, mc.first_air_date, mc.vote_average, mc.vote_count, mc.created_at,
		         mc.original_language, mc.runtime, mc.episode_run_time, mc.best_video_quality,
		         mc.category, mc.latest_torrent_date
		ORDER BY %s %s NULLS LAST, mc.created_at DESC
		LIMIT %s OFFSET %s`,
		whereSQL, sortCol, p.SortDir, limitPh, offsetPh)

	rows, err := postgres.Pool.Query(ctx, mainSQL, args...)
	if err != nil {
		return AllCardsResult{Cards: []NewTodayCard{}, Total: total}
	}
	defer rows.Close()

	var out []NewTodayCard
	for rows.Next() {
		var c NewTodayCard
		var createdAt time.Time
		if rows.Scan(&c.CardID, &c.TmdbID, &c.MediaType, &c.Title, &c.OriginalTitle,
			&c.Year, &c.VoteAverage, &c.VoteCount, &createdAt, &c.Trackers, &c.Language,
			&c.Runtime, &c.EpisodeRunTime, &c.BestVideoQuality, &c.Category,
			&c.LatestTorrentDate, &c.ReleaseDate) == nil {
			c.CreatedAt = createdAt.Format("2006-01-02")
			c.Categories = cardCategories(c)
			out = append(out, c)
		}
	}
	if out == nil {
		out = []NewTodayCard{}
	}
	return AllCardsResult{Cards: out, Total: total}
}

func GetAllCardsDistinct(ctx context.Context) AllCardsDistinct {
	var d AllCardsDistinct

	if rows, err := postgres.Pool.Query(ctx, `
		SELECT CASE WHEN media_type='movie' THEN 'Фильм' ELSE 'Сериал' END, COUNT(*)
		FROM media_cards GROUP BY 1 ORDER BY 2 DESC`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var v string; var cnt int
			if rows.Scan(&v, &cnt) == nil {
				d.MediaType = append(d.MediaType, []any{v, cnt})
			}
		}
	}

	if rows, err := postgres.Pool.Query(ctx, `
		SELECT CASE WHEN COALESCE(LEFT(COALESCE(release_date::text, first_air_date::text,''),4),'')='' THEN '—'
		            ELSE LEFT(COALESCE(release_date::text, first_air_date::text),4) END AS yr,
		       COUNT(*) FROM media_cards GROUP BY 1 ORDER BY yr DESC NULLS LAST`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var v string; var cnt int
			if rows.Scan(&v, &cnt) == nil {
				d.Year = append(d.Year, []any{v, cnt})
			}
		}
	}

	if rows, err := postgres.Pool.Query(ctx, `
		SELECT CASE WHEN COALESCE(original_language,'')='' THEN '—' ELSE UPPER(original_language) END AS lang,
		       COUNT(*) FROM media_cards GROUP BY 1 ORDER BY 2 DESC`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var v string; var cnt int
			if rows.Scan(&v, &cnt) == nil {
				d.Language = append(d.Language, []any{v, cnt})
			}
		}
	}

	if rows, err := postgres.Pool.Query(ctx, `
		SELECT tracker, COUNT(DISTINCT card_id) FROM torrents
		WHERE tracker != '' GROUP BY tracker ORDER BY 2 DESC`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var v string; var cnt int
			if rows.Scan(&v, &cnt) == nil {
				d.Trackers = append(d.Trackers, []any{v, cnt})
			}
		}
	}

	if d.MediaType == nil { d.MediaType = [][]any{} }
	if d.Year == nil { d.Year = [][]any{} }
	if d.Language == nil { d.Language = [][]any{} }
	if d.Trackers == nil { d.Trackers = [][]any{} }
	return d
}

// cardCategories returns human-readable category names a card belongs to.
func cardCategories(c NewTodayCard) []string {
	currentYear := time.Now().Year()
	yearInt := 0
	if len(c.Year) == 4 {
		fmt.Sscan(c.Year, &yearInt) //nolint:errcheck
	}
	isNew2 := yearInt >= currentYear-2+1
	isNew4 := yearInt >= currentYear-4+1
	isRu   := c.Language == "ru"
	q      := c.BestVideoQuality

	var cats []string
	switch c.Category {
	case models.CatCartoonMovie:
		cats = append(cats, "Мультфильмы")
	case models.CatCartoonSeries:
		cats = append(cats, "Мультсериалы")
	case models.CatAnime:
		cats = append(cats, "Аниме")
	default:
		if c.MediaType == "movie" {
			if q >= 300 {
				if isNew4 {
					cats = append(cats, "4K новые")
				} else {
					cats = append(cats, "4K")
				}
			}
			if isNew2 && q >= 200 {
				if isRu {
					cats = append(cats, "Рус. новые")
				} else {
					cats = append(cats, "Новые фильмы")
				}
			} else if !isNew2 {
				if isRu {
					cats = append(cats, "Рус. фильмы")
				} else {
					cats = append(cats, "Фильмы")
				}
			}
			if c.VoteCount >= 1000 {
				cats = append(cats, "Топ фильмы")
			}
		} else {
			if isRu {
				cats = append(cats, "Рус. сериалы")
			} else {
				cats = append(cats, "Сериалы")
			}
		}
	}
	if yearInt > 0 {
		cats = append(cats, fmt.Sprintf("%d", yearInt))
	}
	return cats
}

func DeleteCard(ctx context.Context, cardID string) error {
	_, err := postgres.Pool.Exec(ctx, `DELETE FROM media_cards WHERE card_id = $1`, cardID)
	return err
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
	CertificationRU   *string
	CertificationUS   *string
	// Popularity counts — populated only by GetPopular, 0 elsewhere.
	Viewers      int
	Plays        int
	AvgPercent   int // average deepest watch progress
	FinishedRate int // % of plays watched to the end (>=85%)
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
	OrderByCreatedAt bool    // ORDER BY created_at DESC (recently added to tracker)
	RandomOrder     bool     // ORDER BY RANDOM()
	// RandSeed, when non-nil, selects a random page via the indexed rand_key column
	// instead of a full ORDER BY RANDOM() scan: rows are taken from a rotation of the
	// rand_key permutation starting at the seed (so the same seed paginates without
	// duplicates). Used for genre_* / genre_random. Count is skipped (caller supplies total).
	RandSeed *float64
	Genres          []string // genre names (OR logic), e.g. ["боевик", "Боевик и Приключения"]
	Child                bool
	ChildAge             int      // computed from birth year; -1 = child but no age set, >=0 = cert-based filter
	ChildBlockedKeywords []int    // TMDB keyword IDs to exclude for child profiles
	ChildTextKeywords    []string // text words to block in title/overview
	HideUnrated          bool     // exclude cards with no certification (plugin hide_unrated=1)
	Year            int      // exact release year filter
	TrackerFilter   []string // if non-empty, only show cards linked to at least one of these trackers
	NewOnly         bool     // only items released within last YearDelta years AND quality >= 200
	OldOnly         bool     // only items released more than YearDelta years ago (complement of NewOnly)
	YearDelta       int      // years window for NewOnly/OldOnly (default 2, use 4 for 4K)
	Page            int
	PerPage         int
	Search          string
	HideWatched     bool
	DeviceID        int64
	ProfileID       string
	WatchedPercent  int
	RequirePoster   bool // exclude cards with empty/null poster_path
	RecentDays      int  // if > 0, only cards that have a torrent added to tracker within last N days
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

	where, args, n := categoryWhere(f)

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	// Random collections (genre_*/genre_random): indexed seek over rand_key.
	// Count is skipped — the caller fills total_results from the per-category cache.
	if f.RandSeed != nil {
		return listRandomByKey(ctx, whereClause, args, *f.RandSeed, n, perPage, offset), 0
	}

	orderBy := "m.latest_torrent_date DESC NULLS LAST, m.created_at DESC"
	if f.RandomOrder {
		orderBy = "RANDOM()"
	} else if f.OrderByRating {
		orderBy = "m.vote_average DESC NULLS LAST, m.vote_count DESC NULLS LAST, m.created_at DESC"
	} else if f.OrderByCreatedAt {
		orderBy = "m.created_at DESC NULLS LAST"
	} else if f.OldOnly || f.Year > 0 {
		// Archive / year categories: sort by release/air date descending
		orderBy = "COALESCE(m.release_date, m.first_air_date) DESC NULLS LAST, m.created_at DESC"
	}

	countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM media_cards m %s`, whereClause)
	if err := postgres.Pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		log.Printf("store: count category: %v", err)
		return
	}

	dataSQL := fmt.Sprintf(`SELECT %s FROM media_cards m %s ORDER BY %s LIMIT %d OFFSET %d`,
		mediaCardCols, whereClause, orderBy, perPage, offset)

	qrows, err := postgres.Pool.Query(ctx, dataSQL, args...)
	if err != nil {
		log.Printf("store: query category: %v", err)
		return
	}
	defer qrows.Close()
	rows = scanMediaRows(qrows)
	return
}

// categoryWhere builds the WHERE conditions (and their args) shared by ListCategory
// and CountCategory. n is the next positional-parameter index.
func categoryWhere(f CategoryFilter) (where []string, args []interface{}, n int) {
	n = 1

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
		if f.ChildAge >= 0 {
			// cert_level: RU cert → numeric; US cert → numeric (fallback)
			certLevel := `
				CASE
					WHEN m.certification_ru = '0+'  THEN 0
					WHEN m.certification_ru = '6+'  THEN 6
					WHEN m.certification_ru = '12+' THEN 12
					WHEN m.certification_ru = '16+' THEN 16
					WHEN m.certification_ru = '18+' THEN 18
					WHEN (m.certification_ru IS NULL OR m.certification_ru = '') AND m.certification_us IN ('G','TV-G','TV-Y') THEN 0
					WHEN (m.certification_ru IS NULL OR m.certification_ru = '') AND m.certification_us IN ('PG','TV-Y7','TV-PG') THEN 6
					WHEN (m.certification_ru IS NULL OR m.certification_ru = '') AND m.certification_us IN ('PG-13','TV-14') THEN 12
					WHEN (m.certification_ru IS NULL OR m.certification_ru = '') AND m.certification_us = 'R' THEN 16
					WHEN (m.certification_ru IS NULL OR m.certification_ru = '') AND m.certification_us IN ('NC-17','TV-MA') THEN 18
					ELSE NULL
				END`
			where = append(where, fmt.Sprintf(
				"(%s IS NULL OR %s <= $%d)",
				certLevel, certLevel, n,
			))
			args = append(args, f.ChildAge)
			n++
		} else {
			// No birth year — fallback to old age_rating based filter
			where = append(where, "(m.age_rating IS NULL OR m.age_rating <= 12)")
		}
		if f.ChildAge < 12 { // includes -1 (no age) and 0, 6
			where = append(where, "NOT (m.genres @> '[{\"id\":27}]' OR m.genres @> '[{\"id\":53}]' OR m.genres @> '[{\"id\":80}]')")
		}
		if len(f.ChildBlockedKeywords) > 0 {
			where = append(where, fmt.Sprintf("(m.keyword_ids IS NULL OR NOT m.keyword_ids && $%d)", n))
			args = append(args, f.ChildBlockedKeywords)
			n++
		}
	}
	if f.HideUnrated {
		where = append(where, "(COALESCE(m.certification_ru,'') != '' OR COALESCE(m.certification_us,'') != '')")
	}
	// Text keyword filter applies to both child and adult profiles (set by applyChildFilter or applyAdultTextFilter)
	if len(f.ChildTextKeywords) > 0 {
		clauses := make([]string, len(f.ChildTextKeywords))
		for i, word := range f.ChildTextKeywords {
			clauses[i] = fmt.Sprintf("(m.title ILIKE $%d OR COALESCE(m.overview,'') ILIKE $%d)", n, n+1)
			args = append(args, "%"+word+"%", "%"+word+"%")
			n += 2
		}
		where = append(where, "NOT ("+strings.Join(clauses, " OR ")+")")
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
	if len(f.Genres) > 0 {
		parts := make([]string, len(f.Genres))
		for i, g := range f.Genres {
			parts[i] = fmt.Sprintf(`m.genres @> $%d::jsonb`, n)
			args = append(args, fmt.Sprintf(`[{"name":"%s"}]`, g))
			n++
		}
		if len(parts) == 1 {
			where = append(where, parts[0])
		} else {
			where = append(where, "("+strings.Join(parts, " OR ")+")")
		}
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
	if f.RequirePoster {
		where = append(where, "m.poster_path IS NOT NULL AND m.poster_path <> ''")
	}
	if f.RecentDays > 0 {
		where = append(where, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM torrents t_rd WHERE t_rd.card_id = m.card_id AND t_rd.created_at >= NOW() - INTERVAL '%d days')",
			f.RecentDays,
		))
	}
	if f.HideWatched && f.DeviceID > 0 {
		// «Досмотренные» card_id считаем ОДНИМ предвычисленным подзапросом (а не
		// коррелированными подзапросами на каждую карточку-кандидата). Подзапрос
		// независим от внешнего запроса → PostgreSQL хэширует его в анти-джойн.
		// Счёт эпизодов для TV считается только по уже просмотренным карточкам
		// (их немного), а не по всему каталогу.
		where = append(where, fmt.Sprintf(`m.card_id NOT IN (
			SELECT mc.card_id
			FROM (
				SELECT tc.card_id, COUNT(*) FILTER (
						WHERE ((tc.data::jsonb->>'percent')::numeric >= $%d
						   OR (tc.data::jsonb->>'special')::boolean IS TRUE)
						  AND NOT EXISTS (SELECT 1 FROM episodes e_sp
						                  WHERE e_sp.hash = tc.item AND e_sp.is_special)
					) AS w
				FROM timecodes tc
				WHERE tc.device_id = $%d AND tc.profile_id = $%d
				GROUP BY tc.card_id
			) wt
			JOIN media_cards mc ON mc.card_id = wt.card_id
			WHERE (mc.media_type = 'movie' AND wt.w >= 1)
			   OR (mc.media_type = 'tv' AND wt.w >= GREATEST(1, COALESCE(
					(SELECT COUNT(*)::int FROM episodes e
					 WHERE e.tmdb_show_id = mc.tmdb_id
					   AND NOT e.is_special
					   AND e.air_date IS NOT NULL AND e.air_date <= CURRENT_DATE),
					mc.number_of_episodes)))
		)`, n+2, n, n+1))
		args = append(args, f.DeviceID, f.ProfileID, f.WatchedPercent)
		n += 3
	}

	return where, args, n
}

// mediaCardCols is the SELECT list for category card rows (scanned by scanMediaRows).
const mediaCardCols = `m.tmdb_id, m.media_type, m.title, m.original_title,
	m.overview, m.poster_path, m.backdrop_path,
	m.release_date::text, m.first_air_date::text, m.last_air_date::text,
	m.vote_average, m.vote_count, m.original_language, m.adult, m.status,
	m.number_of_seasons, m.seasons, m.last_ep_season, m.last_ep_number, m.updated_at,
	m.best_video_quality, m.latest_torrent_date,
	m.certification_ru, m.certification_us`

// scanMediaRows reads MediaRow values from a query using the mediaCardCols list.
func scanMediaRows(qrows pgx.Rows) (rows []MediaRow) {
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
			&r.CertificationRU, &r.CertificationUS,
		); err != nil {
			log.Printf("store: scan row: %v", err)
			continue
		}
		rows = append(rows, r)
	}
	return
}

// CountCategory returns the number of cards matching the filter (ignoring pagination
// and RandSeed). Used to cache per-category totals for random collections.
func CountCategory(f CategoryFilter) int {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	where, args, _ := categoryWhere(f)
	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}
	return countCards(ctx, whereClause, args)
}

func countCards(ctx context.Context, whereClause string, args []interface{}) int {
	var c int
	if err := postgres.Pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM media_cards m "+whereClause, args...).Scan(&c); err != nil {
		log.Printf("store: count cards: %v", err)
	}
	return c
}

func andCond(whereClause, cond string) string {
	if whereClause == "" {
		return "WHERE " + cond
	}
	return whereClause + " AND " + cond
}

func queryRandPage(ctx context.Context, whereClause string, args []interface{}, limit, offset int) []MediaRow {
	// card_id is a unique tiebreaker → deterministic order so OFFSET paging across
	// separate page requests never repeats or skips a row at a boundary.
	sql := fmt.Sprintf("SELECT %s FROM media_cards m %s ORDER BY m.rand_key, m.card_id LIMIT %d OFFSET %d",
		mediaCardCols, whereClause, limit, offset)
	qrows, err := postgres.Pool.Query(ctx, sql, args...)
	if err != nil {
		log.Printf("store: query rand page: %v", err)
		return nil
	}
	return scanMediaRows(qrows)
}

// listRandomByKey returns a page from the rand_key permutation rotated to start at seed.
// The permutation is: cards with rand_key >= seed (ascending), then cards with
// rand_key < seed (ascending) — i.e. a circular order. Because the same seed yields the
// same rotation, paginating with OFFSET never repeats a card. It reads only ~perPage rows
// via the rand_key index instead of scanning every matching row like ORDER BY RANDOM().
func listRandomByKey(ctx context.Context, whereClause string, args []interface{}, seed float64, seedN, perPage, offset int) []MediaRow {
	seedArgs := append(append([]interface{}{}, args...), seed)
	highWhere := andCond(whereClause, fmt.Sprintf("m.rand_key >= $%d", seedN))

	high := queryRandPage(ctx, highWhere, seedArgs, perPage, offset)
	if len(high) >= perPage {
		return high
	}

	// High part exhausted (or offset is past it) — wrap to the low part (rand_key < seed).
	lowWhere := andCond(whereClause, fmt.Sprintf("m.rand_key < $%d", seedN))
	highCount := offset + len(high) // valid when offset was within the high part
	if len(high) == 0 {
		highCount = countCards(ctx, highWhere, seedArgs)
	}
	lowOffset := offset - highCount
	if lowOffset < 0 {
		lowOffset = 0
	}
	low := queryRandPage(ctx, lowWhere, seedArgs, perPage-len(high), lowOffset)
	return append(high, low...)
}

// ─── Search ───────────────────────────────────────────────────────────────────

func SearchMedia(query string, limit, offset int) []MediaRow {
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
			m.best_video_quality, m.latest_torrent_date,
			m.certification_ru, m.certification_us
		FROM media_cards m
		WHERE REGEXP_REPLACE(LOWER(m.title), '[-''.,;:!?()\[\]]', ' ', 'g') ILIKE $1
		   OR REGEXP_REPLACE(LOWER(m.original_title), '[-''.,;:!?()\[\]]', ' ', 'g') ILIKE $1
		ORDER BY m.latest_torrent_date DESC NULLS LAST, m.vote_count DESC
		LIMIT $2 OFFSET $3`,
		"%"+normalizeSearch(query)+"%", limit, offset,
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
			&r.CertificationRU, &r.CertificationUS,
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

func nilIntSlice(s []int) interface{} {
	if len(s) == 0 {
		return nil
	}
	return s
}
