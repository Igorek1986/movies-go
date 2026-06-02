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
	syncMu      sync.Mutex
	syncState   = map[int64]syncDay{} // userID → daily counter
	syncRunning = map[int64]bool{}    // userID → sync currently in progress
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

// tryAcquireSync marks the user's sync as active. Returns false if one is
// already running — prevents a user from launching parallel syncs (double
// click, two tabs, SSE retry) that would double the load on MyShows/TMDB.
func tryAcquireSync(userID int64) bool {
	syncMu.Lock()
	defer syncMu.Unlock()
	if syncRunning[userID] {
		return false
	}
	syncRunning[userID] = true
	return true
}

// releaseSync clears the in-progress flag for a user.
func releaseSync(userID int64) {
	syncMu.Lock()
	delete(syncRunning, userID)
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

// findCardByTitle searches by original_title or title (case-insensitive).
// First tries exact lower-case match (uses functional index — fast).
// Falls back to punctuation-normalized regex match only if exact match fails.
// If year > 0, filters by release year.
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

	yearCond := func(yearVal int) string {
		if yearVal <= 0 {
			return ""
		}
		return fmt.Sprintf(` AND LEFT(COALESCE(release_date::text, first_air_date::text, ''), 4) = '%d'`, yearVal)
	}

	yearTries := []int{year}
	if year <= 0 {
		yearTries = []int{0}
	}

	scan := func(q string, args ...any) *cardBasic {
		var c cardBasic
		if err := postgres.Pool.QueryRow(ctx, q, args...).Scan(&c.CardID, &c.TmdbID, &c.OrigTitle, &c.MediaType); err == nil {
			return &c
		}
		return nil
	}

	const sel = `SELECT card_id, tmdb_id, COALESCE(original_title,''), media_type FROM media_cards`
	const normExpr = `REGEXP_REPLACE(LOWER(%s), '[^[:alpha:][:digit:][:space:]]', '', 'g')`

	for _, yearTry := range yearTries {
		yc := yearCond(yearTry)
		for _, t := range candidates {
			// Fast path: exact case-insensitive match — uses idx_media_cards_orig_title_low / _title_low
			if mediaType != "" {
				if c := scan(sel+` WHERE media_type=$1 AND (lower(original_title)=lower($2) OR lower(title)=lower($2))`+yc+` ORDER BY vote_count DESC NULLS LAST LIMIT 1`, mediaType, t); c != nil {
					return c
				}
			} else {
				if c := scan(sel+` WHERE (lower(original_title)=lower($1) OR lower(title)=lower($1))`+yc+` ORDER BY vote_count DESC NULLS LAST LIMIT 1`, t); c != nil {
					return c
				}
			}

			// Slow fallback: normalized match (strips punctuation) — full scan, rare case
			normT := normTitle(t)
			if normT == "" {
				continue
			}
			normCol := fmt.Sprintf(normExpr, "original_title") + ` = $%d OR ` + fmt.Sprintf(normExpr, "title") + ` = $%d`
			if mediaType != "" {
				q := fmt.Sprintf(sel+` WHERE media_type=$1 AND (`+normCol+`)`+yc+` ORDER BY vote_count DESC NULLS LAST LIMIT 1`, 2, 2)
				if c := scan(q, mediaType, normT); c != nil {
					return c
				}
			} else {
				q := fmt.Sprintf(sel+` WHERE (`+normCol+`)`+yc+` ORDER BY vote_count DESC NULLS LAST LIMIT 1`, 1, 1)
				if c := scan(q, normT); c != nil {
					return c
				}
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
	t0 := time.Now()

	// 1. Local lookups
	if c := findCardByTmdb(ctx, tmdbID, mediaType); c != nil {
		log.Printf("myshows findCard [db/tmdb] %q in %v", origTitle, time.Since(t0))
		return c
	}
	if c := findCardByImdb(ctx, imdbID); c != nil {
		log.Printf("myshows findCard [db/imdb] %q in %v", origTitle, time.Since(t0))
		return c
	}
	if c := findCardByTitle(ctx, origTitle, title, mediaType, year); c != nil {
		log.Printf("myshows findCard [db/title] %q in %v", origTitle, time.Since(t0))
		return c
	}

	log.Printf("myshows findCard [db miss] %q year=%d — going to TMDB", origTitle, year)

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
		log.Printf("myshows findCard [not found] %q in %v", origTitle, time.Since(t0))
		return nil
	}

	log.Printf("myshows findCard [tmdb] %q → tmdb_id=%d in %v", origTitle, ent.ID, time.Since(t0))
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

	// Reject a second concurrent sync for the same user (before consuming the
	// daily quota). defer guarantees the flag is cleared on any exit.
	if !tryAcquireSync(u.ID) {
		Error(w, http.StatusConflict, "Синхронизация уже выполняется")
		return
	}
	defer releaseSync(u.ID)

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

	// ── Prefetch movies and show list (fast — no per-show API calls yet) ─────
	sse.status("Загрузка фильмов…")
	movies, err := myshows.GetWatchedMovies(ctx, token)
	if err != nil {
		sse.errMsg("Ошибка получения фильмов: " + err.Error())
		return
	}

	sse.status("Загрузка списка сериалов…")
	showList, err := myshows.GetShowList(ctx, token)
	if err != nil {
		sse.errMsg("Ошибка получения сериалов: " + err.Error())
		return
	}

	// ── Movies — parallel card lookup, stream SSE as each completes ─────────
	var notFound []string
	moviesTotal := len(movies)

	type movieStreamResult struct {
		mv   myshows.WatchedMovie
		card *cardBasic
	}
	movieResultCh := make(chan movieStreamResult, moviesTotal)

	var movieWg sync.WaitGroup
	movieSem := make(chan struct{}, 10)
	for _, mv := range movies {
		if ctx.Err() != nil {
			break
		}
		movieWg.Add(1)
		movieSem <- struct{}{}
		go func(mv myshows.WatchedMovie) {
			defer movieWg.Done()
			defer func() { <-movieSem }()
			movieCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			card := findOrFetchCard(movieCtx, mv.TmdbID, mv.ImdbID, mv.OrigTitle, mv.Title, "movie", mv.Year)
			movieResultCh <- movieStreamResult{mv: mv, card: card}
		}(mv)
	}
	go func() {
		movieWg.Wait()
		close(movieResultCh)
	}()

	movieDone := 0
	for res := range movieResultCh {
		movieDone++
		if ctx.Err() != nil {
			return
		}
		sse.send(map[string]any{
			"type":    "stage",
			"stage":   "movies",
			"current": movieDone,
			"total":   moviesTotal,
			"name":    res.mv.Title,
		})
		if res.card == nil || res.card.MediaType != "movie" {
			if res.mv.Title != "" {
				notFound = append(notFound, res.mv.Title)
			}
			continue
		}
		if res.mv.RuntimeMin > 0 {
			postgres.Pool.Exec(ctx, //nolint:errcheck
				`UPDATE media_cards SET runtime = $1, updated_at = now() WHERE card_id = $2`,
				res.mv.RuntimeMin, res.card.CardID,
			)
		}
		item := mediaHash(res.card.OrigTitle)
		if item == "" {
			item = res.card.CardID
		}
		store.SetCardTimecodeWatched(ctx, deviceID, profileID, res.card.CardID, item, res.mv.WatchedAt) //nolint:errcheck
	}

	// ── Shows — parallel episode fetch + card lookup, stream SSE as each completes ─
	showsTotal := len(showList)

	type showStreamResult struct {
		sl   myshows.ShowListItem
		sh   *myshows.WatchedShow
		card *cardBasic
	}
	showResultCh := make(chan showStreamResult, showsTotal)

	var showWg sync.WaitGroup
	showSem := make(chan struct{}, 5)
	for _, sl := range showList {
		if ctx.Err() != nil {
			break
		}
		showWg.Add(1)
		showSem <- struct{}{}
		go func(sl myshows.ShowListItem) {
			defer showWg.Done()
			defer func() { <-showSem }()
			showCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			sh, err := myshows.FetchShowEpisodes(showCtx, token, sl)
			if err != nil {
				log.Printf("myshows sync: show %q: %v", sl.Title, err)
				showResultCh <- showStreamResult{sl: sl}
				return
			}
			var card *cardBasic
			if sh != nil {
				card = findOrFetchCard(ctx, sh.TmdbID, sh.ImdbID, sh.OrigTitle, sh.Title, "tv", sh.Year)
			}
			showResultCh <- showStreamResult{sl: sl, sh: sh, card: card}
		}(sl)
	}
	go func() {
		showWg.Wait()
		close(showResultCh)
	}()

	showDone := 0
	for res := range showResultCh {
		showDone++
		if ctx.Err() != nil {
			return
		}
		sse.send(map[string]any{
			"type":    "stage",
			"stage":   "shows",
			"current": showDone,
			"total":   showsTotal,
			"name":    res.sl.Title,
		})
		sh := res.sh
		if sh == nil {
			continue
		}
		card := res.card
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

		if len(sh.AllEpisodes) > 0 {
			rows := make([]store.EpisodeRow, 0, len(sh.AllEpisodes))
			var runtimes []int
			for _, e := range sh.AllEpisodes {
				var durSec *int
				if e.RuntimeMin > 0 {
					d := e.RuntimeMin * 60
					durSec = &d
					if !e.IsSpecial && e.Season > 0 && e.Episode > 0 {
						runtimes = append(runtimes, e.RuntimeMin)
					}
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
			if rt := medianRuntime(runtimes); rt > 0 {
				postgres.Pool.Exec(ctx, //nolint:errcheck
					`UPDATE media_cards SET episode_run_time = $1, updated_at = now() WHERE card_id = $2`,
					rt, card.CardID,
				)
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

// medianRuntime returns the median of a list of runtimes (in minutes), or 0 if empty.
func medianRuntime(runtimes []int) int {
	n := len(runtimes)
	if n == 0 {
		return 0
	}
	sorted := make([]int, n)
	copy(sorted, runtimes)
	for i := 1; i < n; i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}
