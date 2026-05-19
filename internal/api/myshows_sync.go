package api

import (
	"context"
	"encoding/json"
	"fmt"
	"movies-api/db/models"
	"movies-api/db/postgres"
	"movies-api/db/store"
	"movies-api/internal/myshows"
	"movies-api/movies/tmdb"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ─── Rate limiting ────────────────────────────────────────────────────────────

var (
	syncMu    sync.Mutex
	syncState = map[int64]syncDay{} // userID → daily counter
)

type syncDay struct {
	Day   int // YearDay
	Count int
}

// ResetUserSyncCounter clears the in-memory daily sync counter for a user.
func ResetUserSyncCounter(userID int64) {
	syncMu.Lock()
	delete(syncState, userID)
	syncMu.Unlock()
}

// syncAllowed returns (allowed, waitSec). waitSec is seconds until midnight if denied.
func syncAllowed(userID int64, role string) (bool, int) {
	daily := store.GetSettingInt(context.Background(), role+"_myshows_daily")
	if daily == 0 { // 0 = unlimited
		return true, 0
	}

	now := time.Now()
	syncMu.Lock()
	defer syncMu.Unlock()

	e := syncState[userID]
	if e.Day != now.YearDay() {
		e = syncDay{Day: now.YearDay(), Count: 0}
	}
	if e.Count >= daily {
		midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		return false, int(time.Until(midnight).Seconds()) + 1
	}
	e.Count++
	syncState[userID] = e
	return true, 0
}

// ─── SSE helper ───────────────────────────────────────────────────────────────

type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newSSEWriter(w http.ResponseWriter) (*sseWriter, bool) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	return &sseWriter{w: w, flusher: f}, true
}

func (s *sseWriter) send(v any) {
	data, _ := json.Marshal(v)
	fmt.Fprintf(s.w, "data: %s\n\n", data)
	s.flusher.Flush()
}

func (s *sseWriter) status(msg string) {
	s.send(map[string]any{"type": "status", "message": msg})
}

func (s *sseWriter) errMsg(msg string) {
	s.send(map[string]any{"type": "error", "message": msg})
}

// ─── Card lookup ──────────────────────────────────────────────────────────────

type cardBasic struct {
	CardID    string
	TmdbID    int64
	OrigTitle string
	MediaType string
}

func findCardByTmdb(ctx context.Context, tmdbID int64, mediaType string) *cardBasic {
	if tmdbID == 0 {
		return nil
	}
	var c cardBasic
	err := postgres.Pool.QueryRow(ctx,
		`SELECT card_id, tmdb_id, COALESCE(original_title,''), media_type FROM media_cards
		 WHERE tmdb_id=$1 AND media_type=$2`, tmdbID, mediaType,
	).Scan(&c.CardID, &c.TmdbID, &c.OrigTitle, &c.MediaType)
	if err != nil {
		return nil
	}
	return &c
}

func findCardByImdb(ctx context.Context, imdbID string) *cardBasic {
	if imdbID == "" {
		return nil
	}
	var c cardBasic
	err := postgres.Pool.QueryRow(ctx,
		`SELECT card_id, tmdb_id, COALESCE(original_title,''), media_type FROM media_cards
		 WHERE imdb_id=$1 LIMIT 1`, imdbID,
	).Scan(&c.CardID, &c.TmdbID, &c.OrigTitle, &c.MediaType)
	if err != nil {
		return nil
	}
	return &c
}

// findCardByTitle searches by original_title or title (case-insensitive, punctuation-stripped).
// If year > 0, tries year-filtered query first, then retries without year.
func findCardByTitle(ctx context.Context, origTitle, title, mediaType string, year int) *cardBasic {
	candidates := []string{}
	seen := map[string]bool{}
	for _, t := range []string{origTitle, title} {
		if t != "" && !seen[t] {
			seen[t] = true
			candidates = append(candidates, t)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	const normExpr = `REGEXP_REPLACE(LOWER(%s), '[^[:alpha:][:digit:][:space:]]', '', 'g')`

	// yearCond adds an optional year filter on release_date / first_air_date.
	// LEFT(..., 4) extracts the year string without date casting (safe for NULL/empty).
	yearCond := func(yearVal int) string {
		if yearVal <= 0 {
			return ""
		}
		return fmt.Sprintf(` AND LEFT(COALESCE(release_date, first_air_date, ''), 4) = '%d'`, yearVal)
	}

	titleCond := func(col, param string) string {
		norm := func(s string) string { return fmt.Sprintf(normExpr, s) }
		return fmt.Sprintf(`LOWER(%s) = LOWER(%s) OR %s = %s`, col, param, norm(col), norm(param+"::text"))
	}

	// When year is known, only match that year; no fallback to avoid wrong duplicates.
	// When year is unknown (0), search without year filter.
	yearTries := []int{year}
	if year <= 0 {
		yearTries = []int{0}
	}

	for _, yearTry := range yearTries {
		yc := yearCond(yearTry)

		for _, t := range candidates {
			var c cardBasic
			var err error
			if mediaType != "" {
				err = postgres.Pool.QueryRow(ctx,
					`SELECT card_id, tmdb_id, COALESCE(original_title,''), media_type FROM media_cards
					 WHERE media_type=$1 AND (`+titleCond("original_title", "$2")+` OR `+titleCond("title", "$2")+`)
					 `+yc+`
					 ORDER BY vote_count DESC NULLS LAST LIMIT 1`,
					mediaType, t,
				).Scan(&c.CardID, &c.TmdbID, &c.OrigTitle, &c.MediaType)
			} else {
				err = postgres.Pool.QueryRow(ctx,
					`SELECT card_id, tmdb_id, COALESCE(original_title,''), media_type FROM media_cards
					 WHERE (`+titleCond("original_title", "$1")+` OR `+titleCond("title", "$1")+`)
					 `+yc+`
					 ORDER BY vote_count DESC NULLS LAST LIMIT 1`,
					t,
				).Scan(&c.CardID, &c.TmdbID, &c.OrigTitle, &c.MediaType)
			}
			if err == nil {
				return &c
			}
		}
	}
	return nil
}

var rePunct = regexp.MustCompile(`[^\p{L}\p{N}\s]`)

// normTitle normalises a title for comparison: lowercase + strip punctuation + collapse spaces.
func normTitle(s string) string {
	return strings.Join(strings.Fields(rePunct.ReplaceAllString(strings.ToLower(s), "")), " ")
}

// searchTMDBVerified searches TMDB by title (with optional year) and returns only a result whose
// title or original_title (normalised) exactly matches one of the query strings.
func searchTMDBVerified(isMovie bool, queries []string, year int) *models.Entity {
	norms := make([]string, 0, len(queries))
	for _, q := range queries {
		if n := normTitle(q); n != "" {
			norms = append(norms, n)
		}
	}
	if len(norms) == 0 {
		return nil
	}

	for _, q := range queries {
		if q == "" {
			continue
		}
		results := tmdb.SearchWithYear(isMovie, q, year)
		// If year-restricted search returns nothing, retry without year
		if len(results) == 0 && year > 0 {
			results = tmdb.Search(isMovie, q)
		}
		var best *models.Entity
		bestScore := 0
		for _, r := range results {
			score := 0
			rNormTitle := normTitle(r.Title)
			rNormOrig := normTitle(r.OriginalTitle)
			for _, n := range norms {
				if rNormOrig == n {
					score += 2 // original_title match is strongest signal
				} else if rNormTitle == n {
					score += 1
				}
			}
			if score > bestScore {
				bestScore = score
				best = r
			}
		}
		if best != nil && bestScore > 0 {
			return tmdb.GetVideoDetails(isMovie, best.ID)
		}
	}
	return nil
}

// findOrFetchCard looks up a card in local DB, and if not found, fetches from TMDB API and upserts.
func findOrFetchCard(ctx context.Context, tmdbID int64, imdbID, origTitle, title, mediaType string, year int) *cardBasic {
	isMovie := mediaType == "movie"

	// 1. Local lookups
	if c := findCardByTmdb(ctx, tmdbID, mediaType); c != nil {
		return c
	}
	if c := findCardByImdb(ctx, imdbID); c != nil {
		return c
	}
	if c := findCardByTitle(ctx, origTitle, title, mediaType, year); c != nil {
		return c
	}

	// 2. TMDB API lookup by ID
	var ent *models.Entity
	if imdbID != "" {
		ent = tmdb.FindByID(isMovie, imdbID, "imdb_id")
	}
	if ent == nil && tmdbID > 0 {
		ent = tmdb.GetVideoDetails(isMovie, tmdbID)
	}

	// 3. TMDB title search with exact-match verification
	if ent == nil {
		ent = searchTMDBVerified(isMovie, []string{origTitle, title}, year)
	}

	if ent == nil {
		return nil
	}

	store.UpsertMediaCard(ent, &models.TorrentDetails{})

	cardID := fmt.Sprintf("%d_%s", ent.ID, ent.MediaType)
	return &cardBasic{
		CardID:    cardID,
		OrigTitle: ent.OriginalTitle,
		MediaType: ent.MediaType,
	}
}

// ─── Handler ──────────────────────────────────────────────────────────────────

// POST /myshows/sync
func handleMyshowsSync(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if u.Role == "simple" {
		Error(w, http.StatusForbidden, "premium required")
		return
	}

	if err := r.ParseMultipartForm(1 << 20); err != nil {
		r.ParseForm() //nolint:errcheck
	}

	deviceID, _ := strconv.ParseInt(r.FormValue("device_id"), 10, 64)
	profileID := r.FormValue("profile_id")
	login := r.FormValue("login")
	password := r.FormValue("password")

	if deviceID == 0 || login == "" || password == "" {
		log.Printf("myshows/sync 400: device_id=%q login=%q password_len=%d ct=%q",
			r.FormValue("device_id"), login, len(password), r.Header.Get("Content-Type"))
		Error(w, http.StatusBadRequest, "device_id, login and password required")
		return
	}

	if !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}

	go store.TrackMyShowsUser(login)

	allowed, waitSec := syncAllowed(u.ID, u.Role)
	if !allowed {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
			"detail": map[string]any{
				"message":  "Лимит синхронизации исчерпан.",
				"wait_sec": waitSec,
			},
		})
		return
	}

	sse, ok := newSSEWriter(w)
	if !ok {
		Error(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	ctx := r.Context()

	sse.status("Авторизация в MyShows…")
	token, err := myshows.LoginUser(ctx, login, password)
	if err != nil {
		sse.errMsg("Ошибка авторизации: " + err.Error())
		return
	}

	// ── Movies ───────────────────────────────────────────────────────────────
	sse.status("Загрузка фильмов…")
	movies, err := myshows.GetWatchedMovies(ctx, token)
	if err != nil {
		sse.errMsg("Ошибка получения фильмов: " + err.Error())
		return
	}

	var notFound []string
	moviesTotal := len(movies)

	for i, mv := range movies {
		select {
		case <-ctx.Done():
			return
		default:
		}

		sse.send(map[string]any{
			"type":    "stage",
			"stage":   "movies",
			"current": i + 1,
			"total":   moviesTotal,
			"name":    mv.Title,
		})

		card := findOrFetchCard(ctx, mv.TmdbID, mv.ImdbID, mv.OrigTitle, mv.Title, "movie", mv.Year)
		if card == nil || card.MediaType != "movie" {
			if mv.Title != "" {
				notFound = append(notFound, mv.Title)
			}
			continue
		}

		item := mediaHash(card.OrigTitle)
		if item == "" {
			item = card.CardID
		}
		store.SetCardTimecodeWatched(ctx, deviceID, profileID, card.CardID, item, mv.WatchedAt) //nolint:errcheck
	}

	// ── Shows ────────────────────────────────────────────────────────────────
	sse.status("Загрузка сериалов…")
	shows, err := myshows.GetWatchedShows(ctx, token)
	if err != nil {
		sse.errMsg("Ошибка получения сериалов: " + err.Error())
		return
	}

	showsTotal := len(shows)
	for i, sh := range shows {
		select {
		case <-ctx.Done():
			return
		default:
		}

		sse.send(map[string]any{
			"type":    "stage",
			"stage":   "shows",
			"current": i + 1,
			"total":   showsTotal,
			"name":    sh.Title,
		})

		card := findOrFetchCard(ctx, sh.TmdbID, sh.ImdbID, sh.OrigTitle, sh.Title, "tv", sh.Year)
		if card == nil || card.MediaType != "tv" {
			if sh.Title != "" {
				notFound = append(notFound, sh.Title)
			}
			continue
		}

		origTitle := card.OrigTitle
		if origTitle == "" {
			origTitle = sh.OrigTitle
		}

		// Upsert episodes table with runtime data from MyShows (no extra API call).
		if len(sh.AllEpisodes) > 0 {
			rows := make([]store.EpisodeRow, 0, len(sh.AllEpisodes))
			for _, e := range sh.AllEpisodes {
				var durSec *int
				if e.RuntimeMin > 0 {
					d := e.RuntimeMin * 60
					durSec = &d
				}
				var airDate *time.Time
				if e.AirDate != "" {
					if t, err := time.Parse("2006-01-02", e.AirDate); err == nil {
						airDate = &t
					}
				}
				rows = append(rows, store.EpisodeRow{
					MyshowsEpID: e.MyshowsEpID,
					Season:      int16(e.Season),
					Episode:     int16(e.Episode),
					Title:       &e.Title,
					DurationSec: durSec,
					IsSpecial:   e.IsSpecial,
					Hash:        myshows.EpisodeHash(e.Season, e.Episode, origTitle),
					AirDate:     airDate,
				})
			}
			store.UpsertEpisodes(ctx, card.TmdbID, rows) //nolint:errcheck
			if sh.MyshowsID > 0 {
				store.SetMyshowsID(ctx, card.CardID, sh.MyshowsID) //nolint:errcheck
			}
		}

		for _, ep := range sh.Episodes {
			h := myshows.EpisodeHash(ep.Season, ep.Episode, origTitle)
			store.SetCardTimecodeWatched(ctx, deviceID, profileID, card.CardID, h, ep.WatchedAt) //nolint:errcheck
		}
	}

	// ── Trim and done ─────────────────────────────────────────────────────────
	trimmed := store.TrimToLimitCount(ctx, deviceID, profileID, u.Role)

	msg := fmt.Sprintf("Синхронизировано: %d фильмов, %d сериалов.", moviesTotal-len(notFound), showsTotal)
	sse.send(map[string]any{
		"type":      "done",
		"message":   msg,
		"not_found": notFound,
		"trimmed":   trimmed,
	})
}
