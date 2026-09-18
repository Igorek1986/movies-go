package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strconv"
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
	LastEpSeason      *int            `json:"last_ep_season,omitempty"`
	LastEpNumber      *int            `json:"last_ep_number,omitempty"`
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
	last_ep_season, last_ep_number,
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

// ListCardsSince returns up to limit media_cards rows ordered by
// (updated_at, card_id) strictly after (since, sinceTie). Used both to serve
// GET /api/sync/cards (always open, no gate — see internal/api/sync_serve.go)
// and, locally, to find rows this instance still needs to push to a peer
// (internal/tasks/instance_sync.go).
//
// card_id is a required tiebreaker, not cosmetic: a plain "updated_at >
// since" cursor silently drops rows once more than one page's worth of rows
// share the exact same updated_at — which happens for real, not just in
// theory, whenever a migration backfills a new/changed column with
// DEFAULT now() across every existing row in one statement (Postgres
// evaluates now() once per statement, so thousands of rows land on the
// identical timestamp). The next page's "> since" then excludes every row
// still sitting on that boundary value, since none of them is strictly
// greater — pagination looks like it "finished" while most of the table
// was never actually returned. card_id (the primary key) breaks that tie.
func ListCardsSince(ctx context.Context, since time.Time, sinceTie string, limit int) ([]SyncCard, error) {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT `+syncCardColumns+`
		 FROM media_cards
		 WHERE updated_at > $1 OR (updated_at = $1 AND card_id > $2)
		 ORDER BY updated_at ASC, card_id ASC LIMIT $3`,
		since, sinceTie, limit,
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
// by sync.
//
// updated_at takes GREATEST(local, incoming) — NOT now(). Stamping it "now"
// was tried first and caused an actual production incident: pulling a card
// bumped its local updated_at to the moment of the pull, which immediately
// made that same card look "freshly changed" to this instance's OWN push
// cursor, so it got pushed straight back to the peer it just came from; the
// peer's own pull-apply then bumped its copy the same way, which made it
// look new to this instance's next pull — full-catalog retransmission every
// single tick, forever, instead of a one-time sync. GREATEST breaks the
// loop: re-applying a row with a timestamp that's <= what's already stored
// leaves updated_at untouched, so it stops looking "new" to any cursor
// (push or serve) the moment both sides agree on it — while a genuinely
// newer incoming value (a real change) still advances it, so transitive
// re-serving to a third peer keeps working exactly as before.
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
			$28,$29,$30,$31,$32,$33,NULLIF($34,'')::timestamptz,$35,$36,now())
		ON CONFLICT (card_id) DO UPDATE SET
			title              = COALESCE(NULLIF(media_cards.title, ''), EXCLUDED.title),
			original_title     = COALESCE(NULLIF(media_cards.original_title, ''), EXCLUDED.original_title),
			overview           = COALESCE(NULLIF(media_cards.overview, ''), EXCLUDED.overview),
			poster_path        = COALESCE(NULLIF(media_cards.poster_path, ''), EXCLUDED.poster_path),
			backdrop_path      = COALESCE(NULLIF(media_cards.backdrop_path, ''), EXCLUDED.backdrop_path),
			release_date       = COALESCE(media_cards.release_date, EXCLUDED.release_date),
			first_air_date     = COALESCE(media_cards.first_air_date, EXCLUDED.first_air_date),
			last_air_date      = COALESCE(media_cards.last_air_date, EXCLUDED.last_air_date),
			runtime            = COALESCE(NULLIF(media_cards.runtime,0), EXCLUDED.runtime),
			episode_run_time   = COALESCE(NULLIF(media_cards.episode_run_time,0), EXCLUDED.episode_run_time),
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
			last_ep_season     = COALESCE(media_cards.last_ep_season, EXCLUDED.last_ep_season),
			last_ep_number     = COALESCE(media_cards.last_ep_number, EXCLUDED.last_ep_number),
			myshows_id         = COALESCE(NULLIF(media_cards.myshows_id,0), EXCLUDED.myshows_id),
			kinopoisk_id       = COALESCE(NULLIF(media_cards.kinopoisk_id,0), EXCLUDED.kinopoisk_id),
			category           = COALESCE(NULLIF(media_cards.category, ''), EXCLUDED.category),
			best_video_quality = GREATEST(media_cards.best_video_quality, EXCLUDED.best_video_quality),
			latest_torrent_date = GREATEST(media_cards.latest_torrent_date, EXCLUDED.latest_torrent_date),
			year               = COALESCE(NULLIF(media_cards.year,0), EXCLUDED.year),
			updated_at         = GREATEST(media_cards.updated_at, EXCLUDED.updated_at)`,
		c.CardID, c.TmdbID, c.MediaType, c.Title, c.OriginalTitle, c.Overview,
		c.PosterPath, c.BackdropPath, c.ReleaseDate, c.FirstAirDate, c.LastAirDate,
		c.VoteAverage, c.VoteCount, c.OriginalLanguage, c.Adult, c.Runtime, c.EpisodeRunTime,
		c.Status, c.ImdbID, c.CertificationRU, c.CertificationUS, c.AgeRating,
		nilRaw(c.Genres), nilRaw(c.Keywords), c.NumberOfSeasons, c.NumberOfEpisodes, nilRaw(c.Seasons),
		c.LastEpSeason, c.LastEpNumber, c.MyshowsID, c.KinopoiskID, c.Category,
		c.BestVideoQuality, c.LatestTorrentDate, c.Year, c.UpdatedAt,
	)
	if err != nil {
		return err
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

// ListPlayEventsSince returns up to limit media_play_events rows ordered by
// (updated_at, card_id|ident|date) strictly after (since, sinceTie) — see
// ListCardsSince's comment for why the tiebreak is load-bearing, not
// cosmetic (media_play_events.updated_at is exactly the column that hit
// this in practice: added by a migration with DEFAULT now(), so every
// pre-existing row shares one timestamp).
func ListPlayEventsSince(ctx context.Context, since time.Time, sinceTie string, limit int) ([]SyncEvent, error) {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT card_id, ident, date::text, max_percent, updated_at
		 FROM media_play_events
		 WHERE updated_at > $1 OR (updated_at = $1 AND (card_id || '|' || ident || '|' || date::text) > $2)
		 ORDER BY updated_at ASC, (card_id || '|' || ident || '|' || date::text) ASC LIMIT $3`,
		since, sinceTie, limit,
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
//
// updated_at takes GREATEST(local, incoming), not now() — same reasoning as
// UpsertSyncedCard's doc comment: now() here caused the exact same
// full-catalog echo loop between two peers that both push and pull.
func UpsertSyncedPlayEvent(ctx context.Context, e SyncEvent) error {
	_, err := postgres.Pool.Exec(ctx, `
		INSERT INTO media_play_events (card_id, ident, date, max_percent, updated_at)
		VALUES ($1, $2, $3::date, $4, $5)
		ON CONFLICT (card_id, ident, date) DO UPDATE SET
			max_percent = GREATEST(media_play_events.max_percent, EXCLUDED.max_percent),
			updated_at  = GREATEST(media_play_events.updated_at, EXCLUDED.updated_at)`,
		e.CardID, e.Ident, e.Date, e.MaxPercent, e.UpdatedAt,
	)
	if isFKViolation(err) {
		return nil
	}
	return err
}

// SyncTorrent is one torrents row for GET /api/sync/torrents — the
// hash→card_id dedup index, not a playable source (see torrents' own doc
// comment: magnet links aren't stored, only the hash). Syncing it is what
// makes a card that arrived via card sync actually show up in the catalog:
// categoryWhere requires EXISTS torrents for that card_id, so a
// metadata-only card with zero local torrent rows stays hidden until this
// instance's own parser happens to find a matching release — which may
// never happen if the release only exists on a tracker (e.g. Kinozal) this
// instance doesn't scrape. Rows with no card_id yet are excluded: nothing
// useful to hand a peer before local resolution has run.
type SyncTorrent struct {
	Hash        string    `json:"hash"`
	CardID      string    `json:"card_id"`
	Tracker     string    `json:"tracker"`
	TmdbID      int64     `json:"tmdb_id"`
	MediaType   string    `json:"media_type"`
	CreatedAt   string    `json:"created_at,omitempty"` // tracker's own upload date — informational only, never the sync cursor
	FirstSeenAt time.Time `json:"first_seen_at"`
}

// ListTorrentsSince returns up to limit torrents rows ordered by
// (first_seen_at, hash) strictly after (since, sinceTie) — first_seen_at,
// not created_at, is the cursor: see its schema.sql comment for why the
// tracker's own upload date can't be used here (an old release discovered
// late would sit permanently behind any reasonable cursor).
func ListTorrentsSince(ctx context.Context, since time.Time, sinceTie string, limit int) ([]SyncTorrent, error) {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT hash, card_id, COALESCE(tracker, ''), COALESCE(tmdb_id, 0), COALESCE(media_type, ''),
		        COALESCE(created_at::text, ''), first_seen_at
		 FROM torrents
		 WHERE card_id IS NOT NULL
		   AND (first_seen_at > $1 OR (first_seen_at = $1 AND hash > $2))
		 ORDER BY first_seen_at ASC, hash ASC LIMIT $3`,
		since, sinceTie, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncTorrent
	for rows.Next() {
		var t SyncTorrent
		if err := rows.Scan(&t.Hash, &t.CardID, &t.Tracker, &t.TmdbID, &t.MediaType, &t.CreatedAt, &t.FirstSeenAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// UpsertSyncedTorrent applies one torrent pulled from a peer. A torrent hash
// is immutable once known — there's nothing to merge, only fields to fill in
// if this instance's own copy (if any) has them NULL — and first_seen_at is
// left out of the UPDATE SET entirely, so an already-known hash keeps
// whichever timestamp it already has and never looks "new" again to this
// instance's own push cursor (the same echo-loop risk UpsertSyncedCard's
// GREATEST comment describes, avoided here by never touching the value
// post-insert instead of by taking a max).
//
// On a genuine INSERT (hash never seen before), first_seen_at is left to its
// column DEFAULT (now()) rather than carrying over t.FirstSeenAt — the
// peer's own discovery time. Using the peer's timestamp here silently broke
// relaying through a hub: a torrent found by a slow-to-catch-up spoke keeps
// its old first_seen_at all the way through the hub, so any OTHER spoke
// whose own pull cursor has already advanced past that timestamp (routine,
// once the historical backlog settles) can never see it via ListTorrentsSince's
// cursor — the hub genuinely has the row, it's just permanently "in the
// past" from that spoke's point of view. Stamping our own now() on arrival
// makes a freshly-relayed row look new to OUR OWN outbound feed, which is
// exactly what the hub-relay ("Одиссея") scenario needs.
func UpsertSyncedTorrent(ctx context.Context, t SyncTorrent) error {
	_, err := postgres.Pool.Exec(ctx, `
		INSERT INTO torrents (hash, card_id, tracker, tmdb_id, media_type, created_at)
		VALUES ($1, $2, NULLIF($3,''), NULLIF($4,0), NULLIF($5,''), NULLIF($6,'')::timestamptz)
		ON CONFLICT (hash) DO UPDATE SET
			card_id    = COALESCE(torrents.card_id, EXCLUDED.card_id),
			tracker    = COALESCE(torrents.tracker, EXCLUDED.tracker),
			tmdb_id    = COALESCE(torrents.tmdb_id, EXCLUDED.tmdb_id),
			media_type = COALESCE(torrents.media_type, EXCLUDED.media_type),
			created_at = COALESCE(torrents.created_at, EXCLUDED.created_at)`,
		t.Hash, t.CardID, t.Tracker, t.TmdbID, t.MediaType, t.CreatedAt,
	)
	return err
}

// ─── Instance identity ──────────────────────────────────────────────────────

var instanceNameAdjectives = []string{
	"тихий", "быстрый", "северный", "южный", "старый", "новый", "дальний",
	"ясный", "туманный", "золотой", "серебряный", "ночной",
}
var instanceNameNouns = []string{
	"маяк", "вокзал", "причал", "мост", "порт", "форпост", "узел", "хаб",
	"остров", "перекрёсток", "терминал", "аванпост",
}

// dockerContainerID matches a bare Docker-assigned hostname (12 lowercase
// hex chars) — the default inside a container with no explicit `hostname:`
// in docker-compose.yml (true for this project today). Not worth using as a
// self-identifying name: meaningless to a human, AND unstable — it's a new
// random value every container recreation, unlike a real hostname or this
// function's own word-based fallback (both stay put across rebuilds once
// persisted to instance_name).
var dockerContainerID = regexp.MustCompile(`^[0-9a-f]{12}$`)

// GetInstanceName returns this instance's self-identification — advertised
// to peers (GET /api/sync/* responses, POST push bodies) and used to label
// "who" in sync_activity_log. Generates and persists a name on first call if
// unset (no static default in SettingDefaults, so two fresh instances don't
// collide before anyone renames one — see /admin/sync): the machine's real
// hostname when it looks like one, a random word-pair otherwise (Docker's
// default container hostname, or any lookup failure) — either way with a
// random numeric suffix, so two instances that happen to share a hostname
// (or roll the same word pair) still end up distinguishable.
func GetInstanceName(ctx context.Context) string {
	if name, ok := GetSetting(ctx, "instance_name"); ok && name != "" {
		return name
	}
	base, err := os.Hostname()
	if err != nil || base == "" || dockerContainerID.MatchString(base) {
		base = instanceNameAdjectives[rand.Intn(len(instanceNameAdjectives))] + "-" +
			instanceNameNouns[rand.Intn(len(instanceNameNouns))]
	}
	name := base + "-" + strconv.Itoa(1000+rand.Intn(9000))
	SetSetting(ctx, "instance_name", name)
	return name
}

// ─── Sync activity log ──────────────────────────────────────────────────────
// Records what actually changed from instance-to-instance sync, and with
// which peer — see internal/api/admin.go's /admin/sync-activity. Logged only
// when a page had something to report (applied>0 || failed>0), same
// convention as the log.Printf calls in instance_sync.go this sits next to.
// direction is "pull" (this instance pulled from a peer) or "push_in" (a
// peer pushed into this instance) — both represent this instance's own data
// actually changing; an outbound push doesn't change local data, so isn't
// logged here (see instance_sync.go's pushCards/pushEvents).

type SyncActivityDaily struct {
	Date  string `json:"date"`
	Total int    `json:"total"`
	Pull  int    `json:"pull"`
	Push  int    `json:"push"`
}

func LogSyncActivity(ctx context.Context, direction, dataset, peerName, peerURL string, applied, failed int) {
	postgres.Pool.Exec(ctx, //nolint:errcheck
		`INSERT INTO sync_activity_log (direction, dataset, peer_name, peer_url, applied, failed)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		direction, dataset, peerName, peerURL, applied, failed,
	)
}

// GetSyncActivityDaily returns per-day activity counts for the given window,
// ordered ascending by date. Every day is present (zero-filled) — same
// pattern as GetPopularDaily.
func GetSyncActivityDaily(ctx context.Context, days int) []SyncActivityDaily {
	if days < 1 {
		days = 30
	}
	rows, err := postgres.Pool.Query(ctx,
		`SELECT d::date::text,
		        COALESCE(s.total, 0), COALESCE(s.pull, 0), COALESCE(s.push, 0)
		 FROM generate_series(
		        CURRENT_DATE - (($1::int - 1) * INTERVAL '1 day'),
		        CURRENT_DATE, INTERVAL '1 day') d
		 LEFT JOIN (
		        SELECT synced_at::date AS date, COUNT(*) AS total,
		               COUNT(*) FILTER (WHERE direction = 'pull') AS pull,
		               COUNT(*) FILTER (WHERE direction = 'push_in') AS push
		        FROM sync_activity_log
		        WHERE synced_at >= CURRENT_DATE - (($1::int - 1) * INTERVAL '1 day')
		        GROUP BY synced_at::date
		 ) s ON s.date = d::date
		 ORDER BY d`,
		days,
	)
	if err != nil {
		return []SyncActivityDaily{}
	}
	defer rows.Close()
	out := []SyncActivityDaily{}
	for rows.Next() {
		var d SyncActivityDaily
		if rows.Scan(&d.Date, &d.Total, &d.Pull, &d.Push) == nil {
			out = append(out, d)
		}
	}
	return out
}

type SyncActivityRow struct {
	Direction string `json:"direction"`
	Dataset   string `json:"dataset"`
	PeerName  string `json:"peer_name"`
	PeerURL   string `json:"peer_url"`
	Applied   int    `json:"applied"`
	Failed    int    `json:"failed"`
	SyncedAt  string `json:"synced_at"`
}

// GetSyncActivityList returns activity log entries, newest first. By default
// covers the whole `days` window; if `date` (YYYY-MM-DD) is given, restricts
// to that single day — used by the daily-chart filter.
func GetSyncActivityList(ctx context.Context, days int, date string, limit int) []SyncActivityRow {
	if limit < 1 {
		limit = 500
	}
	where := "synced_at >= now() - ($1::int * INTERVAL '1 day')"
	args := []any{days}
	if date != "" {
		where = "synced_at::date = $1::date"
		args = []any{date}
	}
	limitIdx := len(args) + 1
	args = append(args, limit)
	rows, err := postgres.Pool.Query(ctx, fmt.Sprintf(
		`SELECT direction, dataset, peer_name, peer_url, applied, failed, synced_at
		 FROM sync_activity_log
		 WHERE %s
		 ORDER BY synced_at DESC
		 LIMIT $%d`, where, limitIdx),
		args...,
	)
	if err != nil {
		return []SyncActivityRow{}
	}
	defer rows.Close()
	out := []SyncActivityRow{}
	for rows.Next() {
		var r SyncActivityRow
		var syncedAt time.Time
		if rows.Scan(&r.Direction, &r.Dataset, &r.PeerName, &r.PeerURL, &r.Applied, &r.Failed, &syncedAt) == nil {
			r.SyncedAt = syncedAt.Format(time.RFC3339)
			out = append(out, r)
		}
	}
	return out
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
