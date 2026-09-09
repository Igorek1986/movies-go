package store

import (
	"context"
	"encoding/json"
	"time"

	"movies-api/db/postgres"

	"github.com/jackc/pgx/v5"
)

// This file implements the storage side of instance-to-instance sync — see
// dev/instance-sync.md. Two datasets are exposed as incremental, cursor-based
// feeds (cursor = updated_at, matching JacRed's `time`/`lastsync` pattern):
// media_cards (the syncable subset of fields) and media_play_events. Both the
// "serve" side (ListXSince, called by internal/api/sync_serve.go) and the
// "pull" side (UpsertSyncedX, called by internal/tasks/instance_sync.go) live
// here.

// SyncCard is the syncable subset of a media_cards row — deliberately
// excludes purely local bookkeeping (tmdb_updated_at, tmdb_not_found_at,
// created_at, rand_key): pulling a peer's tmdb_updated_at would make this
// instance think it already checked TMDB and skip a real update.
type SyncCard struct {
	CardID            string          `json:"card_id"`
	TmdbID            int64           `json:"tmdb_id"`
	MediaType         string          `json:"media_type"`
	Title             string          `json:"title"`
	OriginalTitle     string          `json:"original_title"`
	Overview          string          `json:"overview"`
	PosterPath        string          `json:"poster_path"`
	BackdropPath      string          `json:"backdrop_path"`
	ReleaseDate       string          `json:"release_date,omitempty"`
	FirstAirDate      string          `json:"first_air_date,omitempty"`
	LastAirDate       string          `json:"last_air_date,omitempty"`
	VoteAverage       float64         `json:"vote_average"`
	VoteCount         int             `json:"vote_count"`
	OriginalLanguage  string          `json:"original_language"`
	Adult             bool            `json:"adult"`
	Runtime           int             `json:"runtime"`
	EpisodeRunTime    int             `json:"episode_run_time"`
	Status            string          `json:"status"`
	ImdbID            string          `json:"imdb_id"`
	CertificationRU   string          `json:"certification_ru"`
	CertificationUS   string          `json:"certification_us"`
	AgeRating         int             `json:"age_rating"`
	Genres            json.RawMessage `json:"genres,omitempty"`
	Keywords          json.RawMessage `json:"keywords,omitempty"`
	NumberOfSeasons   int             `json:"number_of_seasons"`
	NumberOfEpisodes  int             `json:"number_of_episodes"`
	Seasons           json.RawMessage `json:"seasons,omitempty"`
	LastEpSeason      int             `json:"last_ep_season"`
	LastEpNumber      int             `json:"last_ep_number"`
	MyshowsID         int             `json:"myshows_id"`
	KinopoiskID       int64           `json:"kinopoisk_id"`
	Category          string          `json:"category"`
	BestVideoQuality  int             `json:"best_video_quality"`
	LatestTorrentDate string          `json:"latest_torrent_date,omitempty"`
	Year              int             `json:"year"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

const syncCardColumns = `
	card_id, tmdb_id, media_type,
	COALESCE(title, ''), COALESCE(original_title, ''), COALESCE(overview, ''),
	COALESCE(poster_path, ''), COALESCE(backdrop_path, ''),
	COALESCE(release_date::text, ''), COALESCE(first_air_date::text, ''), COALESCE(last_air_date::text, ''),
	COALESCE(vote_average, 0), COALESCE(vote_count, 0), COALESCE(original_language, ''), adult,
	COALESCE(runtime, 0), COALESCE(episode_run_time, 0), COALESCE(status, ''), COALESCE(imdb_id, ''),
	COALESCE(certification_ru, ''), COALESCE(certification_us, ''), COALESCE(age_rating, 0),
	genres, keywords,
	COALESCE(number_of_seasons, 0), COALESCE(number_of_episodes, 0), seasons,
	COALESCE(last_ep_season, 0), COALESCE(last_ep_number, 0),
	COALESCE(myshows_id, 0), COALESCE(kinopoisk_id, 0), COALESCE(category, ''),
	best_video_quality, COALESCE(latest_torrent_date::text, ''), COALESCE(year, 0), updated_at`

func scanSyncCard(rows pgx.Rows) (SyncCard, error) {
	var c SyncCard
	err := rows.Scan(
		&c.CardID, &c.TmdbID, &c.MediaType, &c.Title, &c.OriginalTitle, &c.Overview,
		&c.PosterPath, &c.BackdropPath,
		&c.ReleaseDate, &c.FirstAirDate, &c.LastAirDate,
		&c.VoteAverage, &c.VoteCount, &c.OriginalLanguage, &c.Adult,
		&c.Runtime, &c.EpisodeRunTime, &c.Status, &c.ImdbID,
		&c.CertificationRU, &c.CertificationUS, &c.AgeRating,
		&c.Genres, &c.Keywords,
		&c.NumberOfSeasons, &c.NumberOfEpisodes, &c.Seasons,
		&c.LastEpSeason, &c.LastEpNumber,
		&c.MyshowsID, &c.KinopoiskID, &c.Category,
		&c.BestVideoQuality, &c.LatestTorrentDate, &c.Year, &c.UpdatedAt,
	)
	return c, err
}

// ListCardsSince returns up to limit media_cards rows with updated_at strictly
// after since, ordered by updated_at ascending. Used both to serve GET
// /api/sync/cards (always open, no gate — see internal/api/sync_serve.go)
// and, locally, to find rows this instance still needs to push to a peer
// (internal/tasks/instance_sync.go).
func ListCardsSince(ctx context.Context, since time.Time, limit int) ([]SyncCard, error) {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT `+syncCardColumns+`
		 FROM media_cards WHERE updated_at > $1
		 ORDER BY updated_at ASC LIMIT $2`,
		since, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncCard
	for rows.Next() {
		c, err := scanSyncCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpsertSyncedCard applies one card pulled from a peer. Merge semantics
// mirror UpsertMediaCard: text/id fields fill in only if locally empty,
// best_video_quality/latest_torrent_date take the max (not a promise this
// instance can hand out the file, just a "seen somewhere" witness — see
// dev/instance-sync.md). tmdb_updated_at/tmdb_not_found_at are never touched
// by sync. updated_at is refreshed to now() so this instance can re-serve the
// row to its own peers (transitive propagation).
//
// runtime/episode_run_time are deliberately left out of this statement's SET
// list (untouched on conflict) — a peer's reported runtime isn't just
// "filled in once and locked", it goes through the same >5% deviation
// self-correction as a real player's reported duration
// (MaybeUpdateRuntimeFromPlayer, called below): a wildly different value
// from a peer does overwrite, since it's as much a real signal about actual
// content length as our own player's timeline reports are.
func UpsertSyncedCard(ctx context.Context, c SyncCard) error {
	_, err := postgres.Pool.Exec(ctx, `
		INSERT INTO media_cards
			(card_id, tmdb_id, media_type, title, original_title, overview,
			 poster_path, backdrop_path, release_date, first_air_date, last_air_date,
			 vote_average, vote_count, original_language, adult, runtime, episode_run_time,
			 status, imdb_id, certification_ru, certification_us, age_rating,
			 genres, keywords, number_of_seasons, number_of_episodes, seasons,
			 last_ep_season, last_ep_number, myshows_id, kinopoisk_id, category,
			 best_video_quality, latest_torrent_date, year, updated_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,
			NULLIF($9,'')::date, NULLIF($10,'')::date, NULLIF($11,'')::date,
			$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,
			$28,$29,$30,$31,$32,$33,NULLIF($34,'')::timestamptz,$35,now(),now())
		ON CONFLICT (card_id) DO UPDATE SET
			title              = COALESCE(NULLIF(media_cards.title, ''), EXCLUDED.title),
			original_title     = COALESCE(NULLIF(media_cards.original_title, ''), EXCLUDED.original_title),
			overview           = COALESCE(NULLIF(media_cards.overview, ''), EXCLUDED.overview),
			poster_path        = COALESCE(NULLIF(media_cards.poster_path, ''), EXCLUDED.poster_path),
			backdrop_path      = COALESCE(NULLIF(media_cards.backdrop_path, ''), EXCLUDED.backdrop_path),
			release_date       = COALESCE(media_cards.release_date, EXCLUDED.release_date),
			first_air_date     = COALESCE(media_cards.first_air_date, EXCLUDED.first_air_date),
			last_air_date      = COALESCE(media_cards.last_air_date, EXCLUDED.last_air_date),
			status             = COALESCE(NULLIF(media_cards.status, ''), EXCLUDED.status),
			imdb_id            = COALESCE(NULLIF(media_cards.imdb_id, ''), EXCLUDED.imdb_id),
			certification_ru   = COALESCE(NULLIF(media_cards.certification_ru, ''), EXCLUDED.certification_ru),
			certification_us   = COALESCE(NULLIF(media_cards.certification_us, ''), EXCLUDED.certification_us),
			age_rating         = CASE WHEN COALESCE(media_cards.age_rating,0) = 0 THEN EXCLUDED.age_rating ELSE media_cards.age_rating END,
			genres             = COALESCE(media_cards.genres, EXCLUDED.genres),
			keywords           = COALESCE(media_cards.keywords, EXCLUDED.keywords),
			number_of_seasons  = COALESCE(NULLIF(media_cards.number_of_seasons,0), EXCLUDED.number_of_seasons),
			number_of_episodes = COALESCE(NULLIF(media_cards.number_of_episodes,0), EXCLUDED.number_of_episodes),
			seasons            = COALESCE(media_cards.seasons, EXCLUDED.seasons),
			last_ep_season     = COALESCE(NULLIF(media_cards.last_ep_season,0), EXCLUDED.last_ep_season),
			last_ep_number     = COALESCE(NULLIF(media_cards.last_ep_number,0), EXCLUDED.last_ep_number),
			myshows_id         = COALESCE(NULLIF(media_cards.myshows_id,0), EXCLUDED.myshows_id),
			kinopoisk_id       = COALESCE(NULLIF(media_cards.kinopoisk_id,0), EXCLUDED.kinopoisk_id),
			category           = COALESCE(NULLIF(media_cards.category, ''), EXCLUDED.category),
			best_video_quality = GREATEST(media_cards.best_video_quality, EXCLUDED.best_video_quality),
			latest_torrent_date = GREATEST(media_cards.latest_torrent_date, EXCLUDED.latest_torrent_date),
			year               = COALESCE(NULLIF(media_cards.year,0), EXCLUDED.year),
			updated_at         = now()`,
		c.CardID, c.TmdbID, c.MediaType, c.Title, c.OriginalTitle, c.Overview,
		c.PosterPath, c.BackdropPath, c.ReleaseDate, c.FirstAirDate, c.LastAirDate,
		c.VoteAverage, c.VoteCount, c.OriginalLanguage, c.Adult, c.Runtime, c.EpisodeRunTime,
		c.Status, c.ImdbID, c.CertificationRU, c.CertificationUS, c.AgeRating,
		nilRaw(c.Genres), nilRaw(c.Keywords), c.NumberOfSeasons, c.NumberOfEpisodes, nilRaw(c.Seasons),
		c.LastEpSeason, c.LastEpNumber, c.MyshowsID, c.KinopoiskID, c.Category,
		c.BestVideoQuality, c.LatestTorrentDate, c.Year,
	)
	if err != nil {
		return err
	}
	if c.Runtime > 0 {
		MaybeUpdateRuntimeFromPlayer(c.CardID, "movie", float64(c.Runtime)*60)
	}
	if c.EpisodeRunTime > 0 {
		MaybeUpdateRuntimeFromPlayer(c.CardID, "tv", float64(c.EpisodeRunTime)*60)
	}
	return nil
}

func nilRaw(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// SyncEvent is one media_play_events row for GET /api/sync/events.
type SyncEvent struct {
	CardID     string    `json:"card_id"`
	Ident      string    `json:"ident"`
	Date       string    `json:"date"`
	MaxPercent int       `json:"max_percent"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ListPlayEventsSince returns up to limit media_play_events rows with
// updated_at strictly after since, ordered by updated_at ascending.
func ListPlayEventsSince(ctx context.Context, since time.Time, limit int) ([]SyncEvent, error) {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT card_id, ident, date::text, max_percent, updated_at
		 FROM media_play_events WHERE updated_at > $1
		 ORDER BY updated_at ASC LIMIT $2`,
		since, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncEvent
	for rows.Next() {
		var e SyncEvent
		if err := rows.Scan(&e.CardID, &e.Ident, &e.Date, &e.MaxPercent, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UpsertSyncedPlayEvent applies one play event pulled from a peer, merging
// max_percent as the max of local/incoming. Silently skipped (nil error) if
// the card doesn't exist locally yet (FK violation) — it'll apply cleanly
// once the card itself arrives via card sync or the local parser; a play
// event for a card we don't carry isn't useful to rank anyway.
func UpsertSyncedPlayEvent(ctx context.Context, e SyncEvent) error {
	_, err := postgres.Pool.Exec(ctx, `
		INSERT INTO media_play_events (card_id, ident, date, max_percent, updated_at)
		VALUES ($1, $2, $3::date, $4, now())
		ON CONFLICT (card_id, ident, date) DO UPDATE SET
			max_percent = GREATEST(media_play_events.max_percent, EXCLUDED.max_percent),
			updated_at  = now()`,
		e.CardID, e.Ident, e.Date, e.MaxPercent,
	)
	if isFKViolation(err) {
		return nil
	}
	return err
}

func isFKViolation(err error) bool {
	if err == nil {
		return false
	}
	// pgx wraps *pgconn.PgError; code 23503 = foreign_key_violation.
	type pgErrCode interface{ SQLState() string }
	if pe, ok := err.(pgErrCode); ok {
		return pe.SQLState() == "23503"
	}
	return false
}
