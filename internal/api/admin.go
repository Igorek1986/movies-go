package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"movies-api/config"
	"movies-api/db/postgres"
	"movies-api/db/store"
	"movies-api/internal/bot"
	tasks "movies-api/internal/tasks"
	"movies-api/movies/tmdb"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

var appStartTime = time.Now()

// ─── Parser-mode admin session ────────────────────────────────────────────────

var parserSession struct {
	sync.Mutex
	token string
}

func newParserToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func setParserSession(w http.ResponseWriter) {
	tok := newParserToken()
	parserSession.Lock()
	parserSession.token = tok
	parserSession.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     "parser_admin",
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func checkParserSession(r *http.Request) bool {
	c, err := r.Cookie("parser_admin")
	if err != nil {
		return false
	}
	parserSession.Lock()
	tok := parserSession.token
	parserSession.Unlock()
	return tok != "" && c.Value == tok
}

// requireAnyAdmin returns a middleware that works in both run modes:
// in "all" mode it requires a web session with is_admin=true;
// in "parser" mode it falls back to the parser-mode cookie auth.
func requireAnyAdmin(mode string) func(http.Handler) http.Handler {
	if mode == "all" {
		return requireAdmin
	}
	return requireParserAdmin
}

func requireParserAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store") // не кэшировать админ-данные на прокси
		if !checkParserSession(r) && !adminAPIKeyValid(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleAdminStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var users, usersToday, devices, devicesToday, cards, cardsToday, timecodes, timecodesToday int
	var noRuntimeMovies, noRuntimeTV int
	var tmdbRefreshedToday, tmdbNotFound int
	var actorCount, directorCount int
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&users)                                                                                             //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE created_at::date = CURRENT_DATE`).Scan(&usersToday)                                                  //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices`).Scan(&devices)                                                                                         //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices WHERE created_at::date = CURRENT_DATE`).Scan(&devicesToday)                                              //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM media_cards`).Scan(&cards)                                                                                       //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM media_cards WHERE created_at::date = CURRENT_DATE`).Scan(&cardsToday)                                            //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM timecodes`).Scan(&timecodes)                                                                                     //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM timecodes WHERE created_at::date = CURRENT_DATE`).Scan(&timecodesToday)                                          //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM media_cards WHERE media_type='movie' AND (runtime IS NULL OR runtime=0)`).Scan(&noRuntimeMovies)                 //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM media_cards WHERE media_type='tv' AND (episode_run_time IS NULL OR episode_run_time=0)`).Scan(&noRuntimeTV)      //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM media_cards WHERE tmdb_updated_at::date = CURRENT_DATE AND tmdb_not_found_at IS NULL`).Scan(&tmdbRefreshedToday) //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM media_cards WHERE tmdb_not_found_at IS NOT NULL`).Scan(&tmdbNotFound)                                            //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(DISTINCT person_id) FROM media_card_cast`).Scan(&actorCount)                                                             //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(DISTINCT person_id) FROM media_card_crew WHERE job='Director'`).Scan(&directorCount)                                     //nolint:errcheck

	popularDays := store.GetSettingInt(ctx, "popular_period_days")
	popularCards := store.CountPopularCards(ctx, popularDays)

	// External popular source (if configured): show its card count too.
	popularSourceURL := getPopularSourceURL(ctx)
	popularSourceCount := -1 // -1 = unknown/unreachable
	if popularSourceURL != "" {
		sctx, scancel := context.WithTimeout(ctx, 4*time.Second)
		if resp, err := fetchPopularSource(sctx, 1, 1); err == nil {
			popularSourceCount = resp.TotalResults
		}
		scancel()
	}

	type newUser struct {
		Username  string `json:"username"`
		CreatedAt string `json:"created_at"`
	}
	var newUsersToday []newUser
	if rows, err := postgres.Pool.Query(ctx,
		`SELECT username, created_at FROM users WHERE created_at::date = CURRENT_DATE ORDER BY created_at DESC`,
	); err == nil {
		defer rows.Close()
		for rows.Next() {
			var u newUser
			var t time.Time
			if rows.Scan(&u.Username, &t) == nil {
				u.CreatedAt = t.Format("15:04:05")
				newUsersToday = append(newUsersToday, u)
			}
		}
	}
	if newUsersToday == nil {
		newUsersToday = []newUser{}
	}

	apiToday, apiIPsToday, apiReqsToday := store.GetAPIUserStats(true)
	apiTotal, _, _ := store.GetAPIUserStats(false)
	catsToday := store.GetCategoryStats(true)
	catsTotal := store.GetCategoryStats(false)
	myshowsToday := store.GetMyShowsStats(true)
	myshowsTotal := store.GetMyShowsStats(false)

	if apiToday == nil {
		apiToday = []store.StatRow{}
	}
	if apiTotal == nil {
		apiTotal = []store.StatRow{}
	}
	if catsToday == nil {
		catsToday = []store.StatRow{}
	}
	if catsTotal == nil {
		catsTotal = []store.StatRow{}
	}
	if myshowsToday == nil {
		myshowsToday = []store.StatRow{}
	}
	if myshowsTotal == nil {
		myshowsTotal = []store.StatRow{}
	}

	JSON(w, http.StatusOK, map[string]any{
		"users":                users,
		"users_today":          usersToday,
		"devices":              devices,
		"devices_today":        devicesToday,
		"media_cards":          cards,
		"media_cards_today":    cardsToday,
		"no_runtime_movies":    noRuntimeMovies,
		"no_runtime_tv":        noRuntimeTV,
		"timecodes":            timecodes,
		"timecodes_today":      timecodesToday,
		"new_users_today":      newUsersToday,
		"api_ips_today":        apiIPsToday,
		"api_reqs_today":       apiReqsToday,
		"api_today":            apiToday,
		"api_total":            apiTotal,
		"cats_today":           catsToday,
		"cats_total":           catsTotal,
		"myshows_today":        myshowsToday,
		"myshows_total":        myshowsTotal,
		"tmdb_refreshed_today": tmdbRefreshedToday,
		"tmdb_not_found":       tmdbNotFound,
		"actor_count":          actorCount,
		"director_count":       directorCount,
		"popular_cards":        popularCards,
		"popular_source_url":   popularSourceURL,
		"popular_source_count": popularSourceCount,
	})
}

// handleAPIAdminPopular returns popularity stats: per-day dynamics and a
// per-card ranking by unique viewers, within the popular_period_days window.
func handleAPIAdminPopular(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	days := store.GetSettingInt(ctx, "popular_period_days")
	JSON(w, http.StatusOK, map[string]any{
		"days":  days,
		"daily": store.GetPopularDaily(ctx, days),
		"cards": store.GetPopularCards(ctx, days, 500),
	})
}

func handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := postgres.Pool.Query(r.Context(), `
		SELECT u.id, u.username, u.role, u.is_admin, u.created_at,
		       u.blocked_at, u.block_reason, u.premium_until,
		       COUNT(d.id) AS device_count
		FROM users u
		LEFT JOIN devices d ON d.user_id = u.id
		GROUP BY u.id
		ORDER BY u.id`)
	if err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	type userView struct {
		ID           int64   `json:"id"`
		Username     string  `json:"username"`
		Role         string  `json:"role"`
		IsAdmin      bool    `json:"is_admin"`
		CreatedAt    string  `json:"created_at"`
		BlockedAt    *string `json:"blocked_at"`
		BlockReason  *string `json:"block_reason"`
		PremiumUntil *string `json:"premium_until"`
		DeviceCount  int     `json:"device_count"`
	}
	var result []userView
	for rows.Next() {
		var u userView
		var createdAt time.Time
		var blockedAt, premiumUntil *time.Time
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.IsAdmin, &createdAt,
			&blockedAt, &u.BlockReason, &premiumUntil, &u.DeviceCount); err == nil {
			u.CreatedAt = createdAt.Format("2006-01-02T15:04:05Z")
			if blockedAt != nil {
				s := blockedAt.Format("2006-01-02T15:04:05Z")
				u.BlockedAt = &s
			}
			if premiumUntil != nil {
				s := premiumUntil.Format("2006-01-02")
				u.PremiumUntil = &s
			}
			result = append(result, u)
		}
	}
	if result == nil {
		result = []userView{}
	}
	JSON(w, http.StatusOK, result)
}

// ─── Web history (session auth) ───────────────────────────────────────────────

// handleWebHistoryAll returns all history entries as a flat JSON array.
// Used by the legacy history.js template page which does client-side filtering.
func handleWebHistoryAll(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	q := r.URL.Query()
	deviceID, _ := strconv.ParseInt(q.Get("device_id"), 10, 64)

	entries, _, _ := store.GetHistoryFiltered(r.Context(), store.HistoryFilter{
		UserID:    u.ID,
		DeviceID:  deviceID,
		ProfileID: q.Get("profile_id"),
		Page:      1,
		PerPage:   10000,
	})
	if entries == nil {
		entries = []store.HistoryEntry{}
	}
	JSON(w, http.StatusOK, entries)
}

func handleWebHistory(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 24
	}
	deviceID, _ := strconv.ParseInt(q.Get("device_id"), 10, 64)

	f := store.HistoryFilter{
		UserID:     u.ID,
		DeviceID:   deviceID,
		ProfileID:  q.Get("profile_id"),
		MediaType:  q.Get("media_type"),
		InProgress: q.Get("in_progress") == "1",
		Search:     q.Get("search"),
		Sort:       q.Get("sort"),
		Page:       page,
		PerPage:    perPage,
	}
	entries, counts, total := store.GetHistoryFiltered(r.Context(), f)
	if entries == nil {
		entries = []store.HistoryEntry{}
	}
	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	JSON(w, http.StatusOK, map[string]any{
		"page":          page,
		"total_pages":   totalPages,
		"total_results": total,
		"counts":        counts,
		"results":       entries,
	})
}

// GET /api/web/card-timecodes?device_id=&card_id=
func handleWebCardTimecodes(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	q := r.URL.Query()
	deviceID, _ := strconv.ParseInt(q.Get("device_id"), 10, 64)
	cardID := q.Get("card_id")
	if deviceID == 0 || cardID == "" {
		JSON(w, http.StatusOK, []any{})
		return
	}
	var ownerID int64
	if err := postgres.Pool.QueryRow(r.Context(),
		`SELECT user_id FROM devices WHERE id=$1`, deviceID,
	).Scan(&ownerID); err != nil || ownerID != u.ID {
		JSON(w, http.StatusOK, []any{})
		return
	}
	rows := store.GetCardTimecodes(r.Context(), deviceID, cardID)
	if rows == nil {
		rows = []store.CardTimecodeRow{}
	}
	JSON(w, http.StatusOK, rows)
}

type setTimecodeBody struct {
	DeviceID  int64   `json:"device_id"`
	CardID    string  `json:"card_id"`
	Item      string  `json:"item"`
	Percent   float64 `json:"percent"`
	ProfileID string  `json:"profile_id"`
}

// POST /api/web/set-timecode
func handleWebSetTimecode(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body setTimecodeBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}
	var ownerID int64
	if err := postgres.Pool.QueryRow(r.Context(),
		`SELECT user_id FROM devices WHERE id=$1`, body.DeviceID,
	).Scan(&ownerID); err != nil || ownerID != u.ID {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := store.SetCardTimecode(r.Context(), body.DeviceID, body.ProfileID, body.CardID, body.Item, body.Percent); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DELETE /api/web/card-timecodes?device_id=&card_id=
func handleWebDeleteCardTimecodes(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	q := r.URL.Query()
	deviceID, _ := strconv.ParseInt(q.Get("device_id"), 10, 64)
	cardID := q.Get("card_id")
	if deviceID == 0 || cardID == "" {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}
	var ownerID int64
	if err := postgres.Pool.QueryRow(r.Context(),
		`SELECT user_id FROM devices WHERE id=$1`, deviceID,
	).Scan(&ownerID); err != nil || ownerID != u.ID {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	store.DeleteCardTimecodes(r.Context(), deviceID, cardID)
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /api/web/card-progress?card_id=&device_id=&profile_id=
func handleWebCardProgress(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	q := r.URL.Query()
	cardID := q.Get("card_id")
	profileID := q.Get("profile_id")
	deviceID, _ := strconv.ParseInt(q.Get("device_id"), 10, 64)

	if cardID == "" || deviceID == 0 {
		JSON(w, http.StatusOK, store.CardProgress{})
		return
	}

	// Verify device belongs to this user
	var ownerID int64
	err := postgres.Pool.QueryRow(r.Context(),
		`SELECT user_id FROM devices WHERE id = $1`, deviceID,
	).Scan(&ownerID)
	if err != nil || ownerID != u.ID {
		JSON(w, http.StatusOK, store.CardProgress{})
		return
	}

	p := store.GetCardProgress(r.Context(), deviceID, profileID, cardID)
	JSON(w, http.StatusOK, p)
}

// POST /api/web/mark-special
func handleWebMarkSpecial(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		DeviceID  int64  `json:"device_id"`
		CardID    string `json:"card_id"`
		Item      string `json:"item"`
		ProfileID string `json:"profile_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Item == "" {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}
	var ownerID int64
	if err := postgres.Pool.QueryRow(r.Context(),
		`SELECT user_id FROM devices WHERE id=$1`, body.DeviceID,
	).Scan(&ownerID); err != nil || ownerID != u.ID {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := store.MarkSpecialTimecode(r.Context(), body.DeviceID, body.ProfileID, body.CardID, body.Item); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/web/unmark-special
func handleWebUnmarkSpecial(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		DeviceID  int64  `json:"device_id"`
		CardID    string `json:"card_id"`
		Item      string `json:"item"`
		ProfileID string `json:"profile_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Item == "" {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}
	var ownerID int64
	if err := postgres.Pool.QueryRow(r.Context(),
		`SELECT user_id FROM devices WHERE id=$1`, body.DeviceID,
	).Scan(&ownerID); err != nil || ownerID != u.ID {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := store.UnmarkSpecialTimecode(r.Context(), body.DeviceID, body.ProfileID, body.CardID, body.Item); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /api/card-views?card_id=&device_id=&profile_id=
func handleCardViews(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	q := r.URL.Query()
	cardID := q.Get("card_id")
	profileID := q.Get("profile_id")
	deviceID, _ := strconv.ParseInt(q.Get("device_id"), 10, 64)

	if cardID == "" || deviceID == 0 {
		JSON(w, http.StatusOK, map[string]any{"completed_count": 0})
		return
	}
	if !userOwnsDevice(r, u.ID, deviceID) {
		JSON(w, http.StatusOK, map[string]any{"completed_count": 0})
		return
	}

	if strings.HasSuffix(cardID, "_movie") {
		var total int
		_ = postgres.Pool.QueryRow(r.Context(),
			`SELECT COALESCE(SUM(view_count),0) FROM timecodes
			  WHERE device_id=$1 AND profile_id=$2 AND card_id=$3`,
			deviceID, profileID, cardID,
		).Scan(&total)
		if total == 0 {
			JSON(w, http.StatusOK, map[string]any{"completed_count": 0})
			return
		}
		JSON(w, http.StatusOK, map[string]any{"completed_count": total, "media_type": "movie"})
		return
	}

	// TV: episodes watched (counted_at IS NOT NULL)
	var epCount int
	_ = postgres.Pool.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM timecodes
		  WHERE device_id=$1 AND profile_id=$2 AND card_id=$3 AND counted_at IS NOT NULL`,
		deviceID, profileID, cardID,
	).Scan(&epCount)
	if epCount == 0 {
		JSON(w, http.StatusOK, map[string]any{"completed_count": 0})
		return
	}
	// total episodes from media_cards
	var nEp int
	_ = postgres.Pool.QueryRow(r.Context(),
		`SELECT COALESCE(number_of_episodes, 0) FROM media_cards WHERE card_id=$1`, cardID,
	).Scan(&nEp)
	if nEp == 0 {
		_ = postgres.Pool.QueryRow(r.Context(),
			`SELECT COUNT(DISTINCT item) FROM timecodes WHERE card_id=$1 AND view_count > 0`, cardID,
		).Scan(&nEp)
	}
	var completed any
	if nEp > 0 {
		completed = math.Round(float64(epCount)/float64(nEp)*10000) / 10000
	}
	JSON(w, http.StatusOK, map[string]any{
		"completed_count":  completed,
		"media_type":       "tv",
		"watched_episodes": epCount,
		"total_episodes":   nEp,
	})
}

// DELETE /api/episode-timecode?device_id=&card_id=&item=&profile_id=
func handleDeleteEpisodeTimecode(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	q := r.URL.Query()
	cardID := q.Get("card_id")
	item := q.Get("item")
	profileID := q.Get("profile_id")
	deviceID, _ := strconv.ParseInt(q.Get("device_id"), 10, 64)

	if cardID == "" || item == "" || deviceID == 0 {
		Error(w, http.StatusBadRequest, "device_id, card_id, item required")
		return
	}
	if !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	store.DeleteTimecode(r.Context(), deviceID, profileID, cardID, item)
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func handleAdminSetRole(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	switch req.Role {
	case "simple", "premium", "super":
	default:
		Error(w, http.StatusBadRequest, "invalid role")
		return
	}
	if req.Role == "premium" {
		days := store.GetSettingInt(r.Context(), "premium_duration_days")
		if days <= 0 {
			days = 30
		}
		_, err = postgres.Pool.Exec(r.Context(),
			`UPDATE users SET role = $1, premium_until = now() + ($2 || ' days')::interval WHERE id = $3`,
			req.Role, strconv.Itoa(days), id)
	} else {
		_, err = postgres.Pool.Exec(r.Context(),
			`UPDATE users SET role = $1, premium_until = NULL WHERE id = $2`, req.Role, id)
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := store.DeleteUser(r.Context(), id); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ─── JSON API admin user actions ──────────────────────────────────────────────

func handleAPIAdminToggleAdmin(w http.ResponseWriter, r *http.Request) {
	me := userFromCtx(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || (me != nil && id == me.ID) {
		Error(w, http.StatusBadRequest, "недопустимая операция")
		return
	}
	postgres.Pool.Exec(r.Context(), `UPDATE users SET is_admin = NOT is_admin WHERE id = $1`, id) //nolint:errcheck
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleAPIAdminBlock(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
	now := time.Now()
	if body.Reason != "" {
		postgres.Pool.Exec(r.Context(), //nolint:errcheck
			`UPDATE users SET blocked_at = $1, block_reason = $2 WHERE id = $3 AND is_admin = false`, now, body.Reason, id)
	} else {
		postgres.Pool.Exec(r.Context(), //nolint:errcheck
			`UPDATE users SET blocked_at = $1, block_reason = NULL WHERE id = $2 AND is_admin = false`, now, id)
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleAPIAdminUnblock(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	postgres.Pool.Exec(r.Context(), //nolint:errcheck
		`UPDATE users SET blocked_at = NULL, block_reason = NULL WHERE id = $1`, id)
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleAPIAdminResetSync(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	ResetUserSyncCounter(id)
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleAPIAdminCleanupLimits(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var role string
	postgres.Pool.QueryRow(r.Context(), `SELECT role FROM users WHERE id = $1`, id).Scan(&role) //nolint:errcheck
	deleted := store.CleanupUserOverlimit(r.Context(), id, role)
	JSON(w, http.StatusOK, map[string]any{"status": "ok", "deleted_devices": deleted})
}

// ─── JSON API global admin actions ────────────────────────────────────────────

func handleAPIAdminRunExpiryCheck(w http.ResponseWriter, r *http.Request) {
	go func() {
		postgres.Pool.Exec(r.Context(), //nolint:errcheck
			`UPDATE users SET role = 'simple', premium_until = NULL
			 WHERE role = 'premium' AND premium_until IS NOT NULL AND premium_until < now()`)
	}()
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleAPIAdminExtendAllPremium(w http.ResponseWriter, r *http.Request) {
	days := store.GetSettingInt(r.Context(), "premium_extend_all_days")
	if days <= 0 {
		days = 3
	}
	postgres.Pool.Exec(r.Context(), //nolint:errcheck
		`UPDATE users SET premium_until = COALESCE(premium_until, now()) + ($1 || ' days')::interval
		 WHERE role = 'premium'`, strconv.Itoa(days))
	JSON(w, http.StatusOK, map[string]any{"status": "ok", "days": days})
}

func handleAPIAdminEpisodesRefresh(w http.ResponseWriter, r *http.Request) {
	go tasks.RunRefreshOngoingEpisodes(tasks.AppCtx())
	JSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "Обновление эпизодов запущено"})
}

func handleAPIAdminCardsToday(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, store.GetNewTodayCards(r.Context()))
}

func handleAPIAdminAllCards(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p := store.AllCardsParams{
		Search:          q.Get("search"),
		SortBy:          q.Get("sort_by"),
		SortDir:         q.Get("sort_dir"),
		MediaType:       q["media_type"],
		Year:            q["year"],
		Language:        q["language"],
		Trackers:        q["trackers"],
		TorrentDateFrom: q.Get("torrent_date_from"),
		TorrentDateTo:   q.Get("torrent_date_to"),
		ReleaseDateFrom: q.Get("release_date_from"),
		ReleaseDateTo:   q.Get("release_date_to"),
		NoRuntime:       q.Get("no_runtime"),
	}
	if v, err := strconv.Atoi(q.Get("page")); err == nil && v > 0 {
		p.Page = v
	}
	if v, err := strconv.Atoi(q.Get("per_page")); err == nil && v > 0 {
		p.PerPage = v
	}
	if s := q.Get("runtime_min"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			p.RuntimeMin = &v
		}
	}
	if s := q.Get("runtime_max"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			p.RuntimeMax = &v
		}
	}
	JSON(w, http.StatusOK, store.GetAllCards(r.Context(), p))
}

func handleAPIAdminAllCardsMeta(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, store.GetAllCardsDistinct(r.Context()))
}

func handleAPIAdminPatchCardDates(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "card_id")
	var body struct {
		LatestTorrentDate string `json:"latest_torrent_date"`
		ReleaseDate       string `json:"release_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}
	ctx := r.Context()
	if body.LatestTorrentDate != "" {
		postgres.Pool.Exec(ctx, //nolint:errcheck
			`UPDATE media_cards SET latest_torrent_date = $2 WHERE card_id = $1`,
			cardID, body.LatestTorrentDate)
	}
	if body.ReleaseDate != "" {
		postgres.Pool.Exec(ctx, //nolint:errcheck
			`UPDATE media_cards SET release_date = $2, first_air_date = CASE WHEN media_type = 'tv' THEN $2 ELSE first_air_date END WHERE card_id = $1`,
			cardID, body.ReleaseDate)
	}
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleAPIAdminDeleteCards(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CardIDs []string `json:"card_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.CardIDs) == 0 {
		Error(w, http.StatusBadRequest, "card_ids required")
		return
	}
	deleted := 0
	for _, id := range body.CardIDs {
		if store.DeleteCard(r.Context(), id) == nil {
			deleted++
		}
	}
	JSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

func handleAPIAdminTMDBMissing(w http.ResponseWriter, r *http.Request) {
	cards := store.GetTMDBMissingCards(r.Context())
	if cards == nil {
		cards = []store.TMDBMissingCard{}
	}
	JSON(w, http.StatusOK, cards)
}

func handleAPIAdminTMDBMissingDelete(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "cardID")
	if err := store.DeleteCard(r.Context(), cardID); err != nil {
		Error(w, http.StatusInternalServerError, "delete failed")
		return
	}
	JSON(w, http.StatusOK, map[string]any{"deleted": cardID})
}

func handleAPIAdminRefreshCards(w http.ResponseWriter, r *http.Request) {
	if tasks.GetRefreshCardsStatus().Running {
		JSON(w, http.StatusOK, map[string]any{"status": "already_running"})
		return
	}
	go tasks.RunRefreshCards(tasks.AppCtx())
	JSON(w, http.StatusOK, map[string]any{"status": "started"})
}

func handleAPIAdminRefreshCardsStop(w http.ResponseWriter, r *http.Request) {
	tasks.StopRefreshCards()
	JSON(w, http.StatusOK, map[string]any{"status": "stopped"})
}

func handleAPIAdminRefreshCardsStatus(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, tasks.GetRefreshCardsStatus())
}

func handleAPIAdminFixRuntime(w http.ResponseWriter, r *http.Request) {
	if tasks.GetFixRuntimeStatus().Running {
		JSON(w, http.StatusOK, map[string]any{"status": "already_running"})
		return
	}
	go tasks.RunFixZeroRuntime(tasks.AppCtx())
	JSON(w, http.StatusOK, map[string]any{"status": "started"})
}

func handleAPIAdminFixRuntimeStop(w http.ResponseWriter, r *http.Request) {
	tasks.StopFixZeroRuntime()
	JSON(w, http.StatusOK, map[string]any{"status": "stopped"})
}

func handleAPIAdminFixRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, tasks.GetFixRuntimeStatus())
}

type personListItem struct {
	PersonID    int64   `json:"person_id"`
	PersonName  string  `json:"person_name"`
	ProfilePath string  `json:"profile_path"`
	CardCount   int     `json:"card_count"`
	AvgRating   float64 `json:"avg_rating"`
}

func handleAPIAdminPersonList(table, jobFilter string, w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	const perPage = 50
	offset := (page - 1) * perPage

	// Build ORDER BY from sort param: "cards_desc,rating_asc"
	orderBy := "cnt DESC, MAX(mc.popularity) DESC" // default
	if sortParam := q.Get("sort"); sortParam != "" {
		var parts []string
		for _, token := range strings.Split(sortParam, ",") {
			token = strings.TrimSpace(token)
			switch token {
			case "cards_desc":
				parts = append(parts, "cnt DESC")
			case "cards_asc":
				parts = append(parts, "cnt ASC")
			case "rating_desc":
				parts = append(parts, "avg_r DESC")
			case "rating_asc":
				parts = append(parts, "avg_r ASC")
			}
		}
		if len(parts) > 0 {
			orderBy = strings.Join(parts, ", ")
		}
	}

	var whereClause string
	var args []any
	if jobFilter != "" {
		whereClause = "WHERE mc.job = $3"
		args = []any{perPage, offset, jobFilter}
	} else {
		args = []any{perPage, offset}
	}

	var totalArg []any
	totalCountSQL := fmt.Sprintf(`SELECT COUNT(DISTINCT mc.person_id) FROM %s mc %s`, table, whereClause)
	if jobFilter != "" {
		totalArg = []any{jobFilter}
		totalCountSQL = fmt.Sprintf(`SELECT COUNT(DISTINCT mc.person_id) FROM %s mc WHERE mc.job = $1`, table)
	}
	var total int
	postgres.Pool.QueryRow(ctx, totalCountSQL, totalArg...).Scan(&total) //nolint:errcheck

	dataSQL := fmt.Sprintf(`
		SELECT mc.person_id, mc.person_name, COALESCE(mc.profile_path,''),
		       COUNT(DISTINCT mc.card_id) as cnt,
		       ROUND(AVG(m.vote_average)::numeric, 1) as avg_r
		FROM %s mc
		JOIN media_cards m ON mc.card_id = m.card_id
		%s
		GROUP BY mc.person_id, mc.person_name, mc.profile_path
		ORDER BY %s
		LIMIT $1 OFFSET $2`, table, whereClause, orderBy)

	rows, err := postgres.Pool.Query(ctx, dataSQL, args...)
	if err != nil {
		JSON(w, http.StatusOK, map[string]any{"items": []any{}, "total": 0, "page": page})
		return
	}
	defer rows.Close()
	var list []personListItem
	for rows.Next() {
		var p personListItem
		if rows.Scan(&p.PersonID, &p.PersonName, &p.ProfilePath, &p.CardCount, &p.AvgRating) == nil {
			list = append(list, p)
		}
	}
	if list == nil {
		list = []personListItem{}
	}
	JSON(w, http.StatusOK, map[string]any{
		"items":    list,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

func handleAPIAdminActorList(w http.ResponseWriter, r *http.Request) {
	handleAPIAdminPersonList("media_card_cast", "", w, r)
}

func handleAPIAdminDirectorList(w http.ResponseWriter, r *http.Request) {
	handleAPIAdminPersonList("media_card_crew", "Director", w, r)
}

func handleAPIAdminBackfillCast(w http.ResponseWriter, r *http.Request) {
	if tasks.GetBackfillCastStatus().Running {
		JSON(w, http.StatusOK, map[string]any{"status": "already_running"})
		return
	}
	go tasks.RunBackfillCast(tasks.AppCtx())
	JSON(w, http.StatusOK, map[string]any{"status": "started"})
}

func handleAPIAdminBackfillCastStop(w http.ResponseWriter, r *http.Request) {
	tasks.StopBackfillCast()
	JSON(w, http.StatusOK, map[string]any{"status": "stopped"})
}

func handleAPIAdminBackfillCastStatus(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, tasks.GetBackfillCastStatus())
}

func handleAPIAdminRefreshCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "card_id")
	m := cardIDRe.FindStringSubmatch(cardID)
	if m == nil {
		Error(w, http.StatusBadRequest, "invalid card_id")
		return
	}
	tmdbID, _ := strconv.ParseInt(m[1], 10, 64)
	isMovie := m[2] == "movie"

	ctx := r.Context()
	ent := tmdb.GetVideoDetails(isMovie, tmdbID)
	if ent == nil {
		Error(w, http.StatusNotFound, "TMDB не вернул данные")
		return
	}
	store.RefreshCardTMDB(ctx, cardID, ent)
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ─── Admin settings ───────────────────────────────────────────────────────────

// textareaKeys and checkboxKeys mirror FastAPI TEXTAREA_KEYS / CHECKBOX_KEYS.
var textareaSettingKeys = map[string]bool{
	"privacy_policy_content": true,
	"consent_content":        true,
}
var checkboxSettingKeys = map[string]string{
	"yandex_metrika_enabled":   "yandex_metrika_id",
	"google_analytics_enabled": "google_analytics_id",
}
var boolSettingKeys = map[string]bool{
	"catalog_require_poster": true,
	"images_via_server":      true,
}

// settingsGroupDefs mirrors FastAPI GROUPS.
var settingsGroupDefs = []struct {
	Name string
	Keys []string
}{
	{"Лимиты Simple", []string{
		"simple_device_limit", "simple_profile_limit", "simple_timecode_limit",
		"simple_favorite_limit", "simple_import_daily",
	}},
	{"Лимиты Premium", []string{
		"premium_device_limit", "premium_profile_limit", "premium_timecode_limit",
		"premium_favorite_limit", "premium_import_daily",
		"premium_myshows_daily", "premium_duration_days",
	}},
	{"Лимиты Super (0 = без ограничений)", []string{
		"super_device_limit", "super_profile_limit", "super_timecode_limit",
		"super_favorite_limit", "super_import_daily", "super_myshows_daily",
	}},
	{"Обновление эпизодов", []string{
		"episodes_future_threshold", "episodes_refresh_batch", "episodes_refresh_delay",
	}},
	{"Общие настройки", []string{
		"inactive_delete_days", "inactive_warn_days", "timecode_grace_days",
		"premium_warn_days", "premium_extend_all_days", "watched_threshold",
		"popular_period_days", "daily_task_hour",
		"session_ttl_days", "session_renew_days", "device_token_ttl_days",
		"device_code_ttl_minutes", "telegram_link_ttl_minutes",
		"reset_code_ttl_minutes", "pending_2fa_ttl_sec",
	}},
	{"Уведомления", []string{
		"default_timezone",
	}},
	{"Аналитика", []string{
		"yandex_metrika_enabled", "yandex_metrika_id",
		"google_analytics_enabled", "google_analytics_id",
	}},
	{"Сайт", []string{
		"base_url", "plugin_url", "donate_url", "popular_source_url",
	}},
	{"Телеграм бот", []string{
		"telegram_bot_token", "telegram_bot_name", "telegram_admin_ids", "telegram_use_polling",
	}},
	{"Юридические", []string{
		"site_name", "contact_email",
		"privacy_policy_content", "consent_content",
	}},
	{"Rate Limits", []string{
		"rate_login_max", "rate_login_window_sec",
		"rate_register_max", "rate_register_window_sec",
		"rate_forgot_max", "rate_forgot_window_sec",
		"rate_2fa_max", "rate_2fa_window_sec",
		"sync_cooldown_sec",
	}},
	{"MyShows", []string{
		"myshows_api_url", "myshows_auth_url",
	}},
	{"TMDB обновление карточек", []string{
		"tmdb_refresh_new_year_delta", "tmdb_refresh_old_batch", "tmdb_refresh_age_days",
	}},
	{"Парсер", []string{
		"rutor_host", "kinozal_host", "nnmclub_host",
	}},
	{"Категории парсера (требует перезапуска)", []string{
		"movies_new_year_delta", "movies_new_min_quality", "movies_4k_year_delta",
	}},
	{"Настройки каталога", []string{
		"catalog_require_poster",
		"images_via_server",
		"catalog_actor_count",
		"catalog_actor_ru_count",
		"catalog_director_count",
	}},
	{"Режим работы (требует перезапуска)", []string{
		"app_mode",
	}},
}

// GET /api/admin/settings
func handleAPIAdminSettingsGet(w http.ResponseWriter, r *http.Request) {
	all := store.GetAllSettings(r.Context())
	JSON(w, http.StatusOK, all)
}

// POST /api/admin/settings
func handleAPIAdminSettingsSave(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}
	for _, gd := range settingsGroupDefs {
		for _, key := range gd.Keys {
			val, ok := body[key]
			if !ok {
				continue
			}
			if boolSettingKeys[key] || checkboxSettingKeys[key] != "" {
				v := "0"
				if val == "1" || val == "true" {
					v = "1"
				}
				store.SetSetting(r.Context(), key, v)
				continue
			}
			if textareaSettingKeys[key] {
				store.SetSetting(r.Context(), key, val)
			} else if val != "" {
				store.SetSetting(r.Context(), key, val)
			}
		}
	}
	store.InvalidateLimitsCache()
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /api/admin/restart
func handleAPIAdminRestart(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
}

func handleAPIAdminBotStatus(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, map[string]any{
		"enabled":  bot.Enabled(),
		"username": bot.Username(),
	})
}

func handleAPIAdminBotRestart(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	if err := bot.Restart(ctx); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rawBaseURL, _ := store.GetSetting(ctx, "base_url"); rawBaseURL != "" && bot.Enabled() {
		baseURL := strings.TrimRight(rawBaseURL, "/")
		usePolling, _ := store.GetSetting(ctx, "telegram_use_polling")
		if usePolling != "1" {
			if err := bot.SetWebhook(baseURL + "/bot/webhook"); err != nil {
				log.Printf("bot restart: webhook error: %v", err)
			}
		}
		if err := bot.SetMenuButton(baseURL + "/tg-app"); err != nil {
			log.Printf("bot restart: menu button error: %v", err)
		}
	}
	JSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": bot.Enabled()})
}

// GET /admin — parser-mode only
func handleParserModeAdmin(w http.ResponseWriter, r *http.Request) {
	// no-store: иначе кэширующий прокси (nginx) закэширует HTML авторизованной
	// панели и отдаст его всем — ключ кэша не знает про cookie.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if !checkParserSession(r) {
		errMsg := ""
		if r.URL.Query().Get("err") == "1" {
			errMsg = `<p class="err">Неверный логин или пароль</p>`
		}
		fmt.Fprintf(w, parserLoginHTML, errMsg) //nolint:errcheck
		return
	}
	w.Write([]byte(parserAdminHTML)) //nolint:errcheck
}

// POST /admin/login — parser-mode only: verify credentials, set session
func handleParserModeLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin?err=1", http.StatusSeeOther)
		return
	}
	cfg := config.Get()
	if cfg.SuperUsername == "" || cfg.SuperPassword == "" ||
		r.FormValue("username") != cfg.SuperUsername ||
		r.FormValue("password") != cfg.SuperPassword {
		http.Redirect(w, r, "/admin?err=1", http.StatusSeeOther)
		return
	}
	setParserSession(w)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// POST /admin/mode — parser-mode only: switch mode and restart
func handleParserModeSwitch(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	mode := r.FormValue("mode")
	if mode != "all" {
		mode = "parser"
	}
	store.SetSetting(r.Context(), "app_mode", mode)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(parserRestartHTML)) //nolint:errcheck
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
}

var parserLoginHTML = `<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Админ</title>
<style>
*{box-sizing:border-box}
body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;
     min-height:100vh;margin:0;background:#111;color:#eee}
form{background:#1a1a1a;padding:2rem;border-radius:8px;width:320px;display:flex;flex-direction:column;gap:.875rem}
h2{margin:0;font-size:1rem;text-align:center;color:#ccc}
label{font-size:.8rem;color:#aaa;margin-bottom:-4px}
input[type=text],input[type=password]{width:100%%;padding:.5rem;border-radius:4px;border:1px solid #333;
  background:#222;color:#eee;font-size:1rem}
button{padding:.6rem;border-radius:4px;border:none;background:#4a90e2;color:#fff;font-size:.95rem;cursor:pointer}
button:hover{background:#357abd}
.err{background:#3a1a1a;color:#f44336;padding:.5rem;border-radius:4px;text-align:center;font-size:.85rem}
</style>
</head>
<body>
<form method="POST" action="/admin/login">
  <h2>Вход в панель управления</h2>
  %s
  <label>Логин</label>
  <input type="text" name="username" required autocomplete="username">
  <label>Пароль</label>
  <input type="password" name="password" required autocomplete="current-password">
  <button type="submit">Войти</button>
</form>
</body></html>`

var parserAdminHTML = `<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Админ (parser)</title>
<style>
*{box-sizing:border-box}
body{font-family:system-ui,sans-serif;margin:0;background:#111;color:#eee;padding:2rem 1rem}
.wrap{max-width:560px;margin:0 auto;display:flex;flex-direction:column;gap:1.5rem}
h1{margin:0;font-size:1.1rem;color:#ccc}
section{background:#1a1a1a;border-radius:8px;padding:1.5rem;display:flex;flex-direction:column;gap:1rem}
h2{margin:0;font-size:.95rem;color:#bbb;border-bottom:1px solid #2a2a2a;padding-bottom:.5rem}
label{font-size:.82rem;color:#aaa}
.row{display:flex;gap:.5rem;align-items:center;flex-wrap:wrap}
.row .btn{flex-shrink:0}
@media(max-width:480px){.row .btn{width:100%}}
select,input:not([type=checkbox]){flex:1;padding:.45rem .6rem;border-radius:4px;border:1px solid #333;
  background:#222;color:#eee;font-size:.9rem}
input[type=number]{flex:none}
.eye-btn{position:absolute;right:6px;top:50%;transform:translateY(-50%);background:none;border:none;
  cursor:pointer;padding:2px 4px;color:#666;line-height:1;display:flex;align-items:center}
.eye-btn:hover{color:#ccc}
.btn{padding:.45rem 1rem;border-radius:4px;border:none;font-size:.9rem;cursor:pointer;white-space:nowrap}
.btn-primary{background:#4a90e2;color:#fff}.btn-primary:hover{background:#357abd}
.btn-danger{background:#c0392b;color:#fff}.btn-danger:hover{background:#a93226}
.btn-ghost{background:#2a2a2a;color:#ccc}.btn-ghost:hover{background:#333}
.tags{display:flex;flex-wrap:wrap;gap:6px}
.tag{display:inline-flex;align-items:center;gap:4px;background:#c0392b;color:#fff;
     border-radius:4px;padding:3px 8px;font-size:.8rem}
.tag button{background:none;border:none;color:#fff;cursor:pointer;padding:0 2px;line-height:1;font-size:.9rem}
.empty{color:#555;font-size:.82rem}
#status{font-size:.82rem;color:#4a90e2;min-height:1.2em}
.hint{font-size:.82rem;color:#888;margin:0}
.status-line{font-size:.82rem;color:#4a90e2;min-height:1.2em}
.kw-dropdown{display:none;position:absolute;top:100%;left:0;right:0;z-index:100;
  background:#1a1a1a;border:1px solid #333;border-radius:4px;margin-top:2px;
  max-height:220px;overflow-y:auto;box-shadow:0 4px 16px rgba(0,0,0,.6)}
.kw-dropdown button{display:flex;width:100%;justify-content:space-between;padding:8px 12px;
  background:none;border:none;border-bottom:1px solid #222;font-size:.85rem;text-align:left;gap:12px}
.kw-dropdown button:hover:not([disabled]){background:#222}
</style>
</head>
<body>
<div class="wrap">
  <h1>Панель управления <span style="color:#555;font-size:.8rem">(режим: parser)</span></h1>

  <section>
    <h2>Режим работы</h2>
    <form method="POST" action="/admin/mode" onsubmit="return confirmMode()">
      <div class="row">
        <select name="mode" id="modeSelect">
          <option value="parser">parser — только парсер</option>
          <option value="all">all — полный режим (веб + авторизация)</option>
        </select>
        <button type="submit" class="btn btn-primary">Сохранить и перезапустить</button>
      </div>
    </form>
  </section>

  <section>
    <h2>Заблокированные домены</h2>
    <div class="row">
      <input type="text" id="patInput" placeholder="example.ru example — через пробел или запятую. example заблокирует всё содержащее это слово"
             onkeydown="if(event.key==='Enter'){event.preventDefault();addPat()}">
      <button class="btn btn-primary" onclick="addPat()">Добавить</button>
    </div>
    <div id="tags" class="tags"><span class="empty">Загрузка…</span></div>
    <div id="status"></div>
    <div id="clearWrap" style="display:none">
      <button class="btn btn-ghost" onclick="clearAll()">Очистить список</button>
    </div>
  </section>

  <section>
    <h2>Коды TMDB (детский режим)</h2>
    <p class="hint">Карточки с этими TMDB-тегами скрываются в детских профилях. Поиск только на английском.</p>
    <div>
      <div style="font-size:.8rem;color:#aaa;margin-bottom:.4rem">Быстрое добавление:</div>
      <div id="kwSuggested" style="display:flex;flex-wrap:wrap;gap:.4rem"></div>
    </div>
    <div style="position:relative">
      <input type="text" id="kwSearch" placeholder="Поиск: nudity, violence, drug use…" oninput="kwOnInput(this.value)" autocomplete="off">
      <div class="kw-dropdown" id="kwDropdown"></div>
    </div>
    <div id="kwTags" class="tags"><span class="empty">Загрузка…</span></div>
    <div class="status-line" id="kwStatus"></div>
    <button class="btn btn-ghost" onclick="kwReset()">Сбросить к умолчаниям</button>
  </section>

  <section>
    <h2>Слова в названии (детский режим)</h2>
    <p class="hint">Карточки, в названии или описании которых встречается слово, скрываются.</p>
    <div>
      <div style="font-size:.8rem;color:#aaa;margin-bottom:.4rem">Применять для возрастных групп:</div>
      <div id="ageGroups" style="display:flex;flex-wrap:wrap;gap:.6rem"></div>
    </div>
    <div class="row">
      <input type="text" id="twInput" placeholder="Слово или фраза — через запятую" onkeydown="if(event.key==='Enter'){event.preventDefault();twAdd()}">
      <button class="btn btn-primary" onclick="twAdd()">Добавить</button>
    </div>
    <div id="twTags" class="tags"><span class="empty">Загрузка…</span></div>
    <div class="status-line" id="twStatus"></div>
  </section>

  <section>
    <h2>Парсеры</h2>
    <div id="parsersStatus" style="font-size:.82rem;color:#4a90e2;min-height:1.2em"></div>
    <div id="parsersList"><span class="empty">Загрузка…</span></div>
    <div class="row" style="margin-top:.5rem;flex-wrap:wrap;gap:.4rem">
      <button class="btn btn-primary" id="btnRunAll" onclick="parsersRunAll()">▶ Запустить все</button>
      <button class="btn btn-danger" id="btnStopAll" onclick="parsersStop()" style="display:none">■ Остановить</button>
    </div>
    <hr style="border-color:#2a2a2a;margin:.5rem 0">
    <div style="display:flex;flex-direction:column;gap:.5rem">
      <label>Хост Rutor
        <div class="row" style="margin-top:4px">
          <input type="text" id="rutorHostInput" placeholder="http://rutor.info">
        </div>
      </label>
      <label>Хост Kinozal
        <div class="row" style="margin-top:4px">
          <input type="text" id="kinozalHostInput" placeholder="https://kinozal.tv">
        </div>
      </label>
      <label>Хост NNMClub
        <div class="row" style="margin-top:4px">
          <input type="text" id="nnmclubHostInput" placeholder="https://nnmclub.to">
        </div>
      </label>
      <div class="row">
        <button class="btn btn-primary" onclick="saveParserHosts()">Сохранить хосты</button>
        <span id="rutorHostStatus" style="font-size:.82rem;color:#4a90e2"></span>
      </div>
    </div>
  </section>

  <section>
    <h2>Прокси</h2>
    <div id="proxyList"><span class="empty">Загрузка…</span></div>
    <hr style="border-color:#2a2a2a;margin:.5rem 0">
    <div id="proxyForm" style="display:flex;flex-direction:column;gap:.6rem">
      <label>Название <input type="text" id="pxName" placeholder="Мой прокси" style="margin-top:4px"></label>
      <div class="row" style="gap:.5rem;align-items:flex-end">
        <label style="flex:1">Хост <input type="text" id="pxHost" placeholder="host.example.com" style="margin-top:4px;width:100%"></label>
        <label>Порт <input type="number" id="pxPort" value="1080" style="margin-top:4px;width:80px" min="1" max="65535"></label>
      </div>
      <div class="row" style="gap:.5rem;align-items:flex-end">
        <label style="flex:1">Логин <input type="text" id="pxLogin" placeholder="необязательно" style="margin-top:4px;width:100%" autocomplete="off"></label>
        <label style="flex:1">Пароль
          <div style="position:relative;margin-top:4px">
            <input type="password" id="pxPassword" placeholder="необязательно" style="width:100%;box-sizing:border-box;padding-right:44px" autocomplete="new-password">
            <button type="button" onclick="togglePxPw()" class="eye-btn" id="pxPwEye"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg></button>
          </div>
        </label>
      </div>
      <label>Приоритет <input type="number" id="pxPriority" value="0" style="margin-top:4px;width:80px"></label>
      <label style="flex-direction:row;gap:.5rem;align-items:center"><input type="checkbox" id="pxEnabled" checked> Включён</label>
      <input type="hidden" id="pxEditId" value="">
      <div class="row">
        <button class="btn btn-primary" onclick="saveProxy()">Сохранить</button>
        <button class="btn btn-ghost" onclick="resetProxyForm()">Отмена</button>
        <span id="pxStatus" style="font-size:.82rem;color:#4a90e2"></span>
      </div>
    </div>
  </section>

  <section>
    <h2>Маршрутизация прокси</h2>
    <p style="font-size:.82rem;color:#888;margin:0">Для каких запросов использовать прокси</p>
    <div id="routingList"><span class="empty">Загрузка…</span></div>
    <div class="row" style="margin-top:.5rem">
      <button class="btn btn-primary" onclick="saveRouting()">Сохранить маршруты</button>
      <span id="rtStatus" style="font-size:.82rem;color:#4a90e2"></span>
    </div>
  </section>
</div>

<script>
function confirmMode(){
  var m=document.getElementById('modeSelect').value;
  return confirm('Переключить в режим "'+m+'" и перезапустить?');
}

var list=[];
function render(){
  var t=document.getElementById('tags');
  var cw=document.getElementById('clearWrap');
  if(!list.length){t.innerHTML='<span class="empty">Список пуст</span>';cw.style.display='none';return;}
  cw.style.display='';
  t.innerHTML=list.map(function(p){
    return '<span class="tag">'+p+'<button onclick="delPat(\''+p+'\')">×</button></span>';
  }).join('');
}
function setStatus(msg,err){
  var s=document.getElementById('status');
  s.style.color=err?'#e74c3c':'#4a90e2';
  s.textContent=msg;
  if(!err)setTimeout(function(){s.textContent=''},2000);
}
function loadPats(){
  fetch('/api/admin/banned-patterns').then(function(r){return r.json();}).then(function(d){list=d;render();}).catch(function(){setStatus('Ошибка загрузки',true);});
}
function addPat(){
  var v=document.getElementById('patInput').value.trim();
  if(!v)return;
  fetch('/api/admin/banned-patterns',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({patterns:v})})
    .then(function(r){return r.json();}).then(function(d){list=d;render();document.getElementById('patInput').value='';setStatus('Добавлено');})
    .catch(function(){setStatus('Ошибка',true);});
}
function delPat(p){
  if(!confirm('Удалить «'+p+'»?'))return;
  fetch('/api/admin/banned-patterns',{method:'DELETE',headers:{'Content-Type':'application/json'},body:JSON.stringify({pattern:p})})
    .then(function(r){return r.json();}).then(function(d){list=d;render();setStatus('Удалено');})
    .catch(function(){setStatus('Ошибка',true);});
}
function clearAll(){
  if(!confirm('Очистить весь список?'))return;
  Promise.all(list.map(function(p){
    return fetch('/api/admin/banned-patterns',{method:'DELETE',headers:{'Content-Type':'application/json'},body:JSON.stringify({pattern:p})});
  })).then(function(){list=[];render();setStatus('Список очищен');}).catch(function(){setStatus('Ошибка',true);});
}
// ── Parsers ───────────────────────────────────────────────────────────────────
var parsersData={};

function setParsersStatus(msg,err){
  var s=document.getElementById('parsersStatus');
  s.style.color=err?'#e74c3c':'#4a90e2';
  s.textContent=msg;
  if(!err)setTimeout(function(){s.textContent=''},2000);
}

function renderParsers(){
  var d=parsersData;
  var el=document.getElementById('parsersList');
  if(!d.parsers){return;}
  var running=d.running;
  document.getElementById('btnRunAll').style.display=running?'none':'';
  document.getElementById('btnStopAll').style.display=running?'':'none';
  el.innerHTML=d.parsers.map(function(p){
    var last=p.last_parsed_at?new Date(p.last_parsed_at).toLocaleString('ru'):'никогда';
    var isCurrent=running&&d.current_tracker===p.name;
    var rowStyle='padding:.35rem 0;border-bottom:1px solid #222;display:flex;flex-direction:column;gap:.3rem';
    return '<div style="'+rowStyle+'">'
      +'<div class="row" style="flex-wrap:wrap;gap:.4rem">'
      +'<label style="display:flex;align-items:center;gap:.4rem;font-size:.88rem;min-width:90px">'
      +'<input type="checkbox" onchange="toggleParser(\''+p.name+'\',this)"'+(p.enabled?' checked':'')+'>'
      +'<b>'+p.name+'</b>'
      +(isCurrent?'<span style="font-size:.72rem;color:#f0ad4e">▶ работает</span>':'')
      +'</label>'
      +'<span style="font-size:.75rem;color:#666;flex:1">последний: '+last+'</span>'
      +'<button class="btn btn-ghost" style="padding:2px 8px;font-size:.78rem" onclick="parsersRunOne(\''+p.name+'\')">▶</button>'
      +'<button class="btn btn-ghost" style="padding:2px 8px;font-size:.78rem" onclick="parsersResetOne(\''+p.name+'\')">Сброс даты</button>'
      +'</div>'
      +'</div>';
  }).join('');
}

function loadParsers(){
  fetch('/api/admin/parsers/').then(function(r){return r.json();}).then(function(d){
    parsersData=d;
    renderParsers();
    document.getElementById('rutorHostInput').value=d.rutor_host||'';
    document.getElementById('kinozalHostInput').value=d.kinozal_host||'';
    document.getElementById('nnmclubHostInput').value=d.nnmclub_host||'';
  }).catch(function(){setParsersStatus('Ошибка загрузки',true);});
}

function toggleParser(name,cb){
  var body={};
  body[name+'_enabled']=cb.checked;
  fetch('/api/admin/parsers/settings',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
    .then(function(r){if(!r.ok)throw new Error();setParsersStatus('Сохранено');})
    .catch(function(){setParsersStatus('Ошибка',true);cb.checked=!cb.checked;});
}

function parsersRunAll(){
  fetch('/api/admin/parsers/run',{method:'POST'})
    .then(function(r){return r.json();})
    .then(function(d){setParsersStatus(d.status==='started'?'Запущен':'Уже работает');setTimeout(loadParsers,500);})
    .catch(function(){setParsersStatus('Ошибка',true);});
}

function parsersStop(){
  fetch('/api/admin/parsers/stop',{method:'POST'})
    .then(function(){setParsersStatus('Запрос остановки отправлен');setTimeout(loadParsers,1000);})
    .catch(function(){setParsersStatus('Ошибка',true);});
}

function parsersRunOne(name){
  fetch('/api/admin/parsers/'+name+'/run',{method:'POST'})
    .then(function(r){return r.json();})
    .then(function(d){setParsersStatus(name+': '+(d.status==='started'?'запущен':'уже работает'));setTimeout(loadParsers,500);})
    .catch(function(){setParsersStatus('Ошибка',true);});
}

function parsersResetOne(name){
  if(!confirm('Сбросить дату для '+name+'? Следующий запуск будет полным.'))return;
  fetch('/api/admin/parsers/'+name+'/reset',{method:'POST'})
    .then(function(){setParsersStatus(name+': дата сброшена');})
    .catch(function(){setParsersStatus('Ошибка',true);});
}

// ── Parser hosts ──────────────────────────────────────────────────────────────
function saveParserHosts(){
  var s=document.getElementById('rutorHostStatus');
  var body={
    rutor_host:document.getElementById('rutorHostInput').value.trim(),
    kinozal_host:document.getElementById('kinozalHostInput').value.trim(),
    nnmclub_host:document.getElementById('nnmclubHostInput').value.trim()
  };
  fetch('/api/admin/settings/',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
    .then(function(r){if(!r.ok)throw new Error();s.style.color='#4a90e2';s.textContent='Сохранено';setTimeout(function(){s.textContent=''},2000);})
    .catch(function(){s.style.color='#e74c3c';s.textContent='Ошибка';});
}

// ── TMDB child keywords ───────────────────────────────────────────────────────
var SUGGESTED_KW=[
  {id:281741,name:'nudity'},{id:354470,name:'sex scene'},{id:329280,name:'sexual content'},
  {id:570,name:'rape'},{id:312898,name:'violence'},{id:10292,name:'gore'},
  {id:13006,name:'torture'},{id:11494,name:'drug use'},{id:919,name:'smoking'},
  {id:567,name:'alcohol'},{id:9826,name:'murder'},{id:158718,name:'lgbt'}
];
var kwList=[],kwTimer=null;

function kwSetStatus(msg,err){
  var s=document.getElementById('kwStatus');
  s.style.color=err?'#e74c3c':'#4a90e2';s.textContent=msg;
  if(!err)setTimeout(function(){s.textContent=''},2000);
}

function kwLoad(){
  fetch('/api/admin/child-keywords/resolve').then(function(r){return r.json();}).then(function(d){
    kwList=d||[];kwRender();
  }).catch(function(){kwSetStatus('Ошибка загрузки',true);});
}

function kwRender(){
  var ids=new Set(kwList.map(function(k){return k.id;}));
  // tags
  var t=document.getElementById('kwTags');
  t.innerHTML=kwList.length?kwList.map(function(kw){
    return '<span class="tag" style="background:#c07d00">'+(kw.name||'ID '+kw.id)+
      '<button onclick="kwDel('+kw.id+')">×</button></span>';
  }).join(''):'<span class="empty">Список пуст</span>';
  // suggested chips
  document.getElementById('kwSuggested').innerHTML=SUGGESTED_KW.map(function(kw){
    var a=ids.has(kw.id);
    return '<button class="btn '+(a?'btn-ghost':'btn-primary')+'" style="padding:3px 10px;font-size:.8rem"'
      +(a?' disabled':' onclick="kwAdd({id:'+kw.id+',name:\\\''+kw.name+'\\\'})"')+'>'
      +(a?'✓ ':'+ ')+kw.name+'</button>';
  }).join('');
}

function kwAdd(kw){
  fetch('/api/admin/child-keywords',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({ids:String(kw.id)})})
    .then(function(){
      document.getElementById('kwSearch').value='';
      document.getElementById('kwDropdown').style.display='none';
      kwLoad();
    }).catch(function(){kwSetStatus('Ошибка',true);});
}

function kwDel(id){
  if(!confirm('Удалить?'))return;
  fetch('/api/admin/child-keywords',{method:'DELETE',headers:{'Content-Type':'application/json'},body:JSON.stringify({id:id})})
    .then(function(){kwLoad();}).catch(function(){kwSetStatus('Ошибка',true);});
}

function kwReset(){
  if(!confirm('Сбросить к значениям по умолчанию?'))return;
  fetch('/api/admin/child-keywords/reset',{method:'POST'}).then(function(){kwLoad();}).catch(function(){kwSetStatus('Ошибка',true);});
}

function kwOnInput(v){
  clearTimeout(kwTimer);
  var dd=document.getElementById('kwDropdown');
  if(!v||v.trim().length<2){dd.style.display='none';return;}
  kwTimer=setTimeout(function(){
    dd.innerHTML='<div style="padding:8px 12px;font-size:.82rem;color:#888">Поиск…</div>';
    dd.style.display='';
    fetch('/api/admin/child-keywords/search?q='+encodeURIComponent(v.trim()))
      .then(function(r){return r.json();})
      .then(function(results){
        var ids=new Set(kwList.map(function(k){return k.id;}));
        if(!results.length){dd.innerHTML='<div style="padding:8px 12px;font-size:.82rem;color:#555">Ничего не найдено</div>';return;}
        dd.innerHTML=results.map(function(kw){
          var a=ids.has(kw.id);
          var n=kw.name.replace(/'/g,"\\'").replace(/</g,'&lt;');
          return '<button style="color:'+(a?'#555':'#eee')+';cursor:'+(a?'default':'pointer')+'"'
            +(a?'':' onclick="kwAdd({id:'+kw.id+',name:\\\''+n+'\\\'})"')+'>'
            +'<span>'+kw.name+'</span>'
            +'<span style="color:'+(a?'#555':'#7c8cf8')+';font-size:.78rem">'+(a?'✓ в списке':'+ добавить')+'</span>'
            +'</button>';
        }).join('');
      }).catch(function(){dd.style.display='none';});
  },400);
}

document.addEventListener('click',function(e){
  var dd=document.getElementById('kwDropdown');
  var sr=document.getElementById('kwSearch');
  if(dd&&sr&&!dd.contains(e.target)&&e.target!==sr)dd.style.display='none';
});

kwLoad();

// ── Text keywords (adult content) ─────────────────────────────────────────────
var AGE_GROUPS=[
  {age:0,label:'0–5 лет'},{age:6,label:'6–11 лет'},
  {age:12,label:'12–15 лет'},{age:16,label:'16+ (дети)'},{age:99,label:'Взрослые'}
];
var twList=[],twAges=[];

function twLoad(){
  fetch('/api/admin/child-text-keywords').then(function(r){return r.json();}).then(function(d){twList=d||[];twRender();}).catch(function(){});
  fetch('/api/admin/child-text-keyword-ages').then(function(r){return r.json();}).then(function(d){twAges=d||[];twRenderAges();}).catch(function(){});
}

function twRender(){
  var t=document.getElementById('twTags');
  t.innerHTML=twList.length?twList.map(function(w){
    var esc=w.replace(/'/g,"\\'").replace(/</g,'&lt;');
    return '<span class="tag">'+w+'<button onclick="twDel(\''+esc+'\')">×</button></span>';
  }).join(''):'<span class="empty">Список пуст</span>';
}

function twRenderAges(){
  document.getElementById('ageGroups').innerHTML=AGE_GROUPS.map(function(g){
    var chk=twAges.indexOf(g.age)>=0?' checked':'';
    return '<label style="display:flex;align-items:center;gap:4px;font-size:.85rem;cursor:pointer">'
      +'<input type="checkbox"'+chk+' onchange="twToggleAge('+g.age+',this.checked)"> '+g.label+'</label>';
  }).join('');
}

function twAdd(){
  var v=document.getElementById('twInput').value.trim();
  if(!v)return;
  fetch('/api/admin/child-text-keywords',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({words:v})})
    .then(function(r){return r.json();}).then(function(d){twList=d||[];twRender();document.getElementById('twInput').value='';})
    .catch(function(){});
}

function twDel(w){
  twList=twList.filter(function(x){return x!==w;});twRender();
  fetch('/api/admin/child-text-keywords',{method:'DELETE',headers:{'Content-Type':'application/json'},body:JSON.stringify({word:w})})
    .then(function(r){return r.json();}).then(function(d){twList=d||[];twRender();}).catch(function(){twLoad();});
}

function twToggleAge(age,checked){
  twAges=checked?twAges.concat([age]):twAges.filter(function(a){return a!==age;});
  fetch('/api/admin/child-text-keyword-ages',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({ages:twAges})})
    .catch(function(){twLoad();});
}

twLoad();

loadParsers();
loadPats();

// ── Proxy management ──────────────────────────────────────────────────────────
var pxConfigs=[], pxRoutes=[];


function setPxStatus(msg,err){
  var s=document.getElementById('pxStatus');
  s.style.color=err?'#e74c3c':'#4a90e2';
  s.textContent=msg;
  if(!err)setTimeout(function(){s.textContent=''},2000);
}
function setRtStatus(msg,err){
  var s=document.getElementById('rtStatus');
  s.style.color=err?'#e74c3c':'#4a90e2';
  s.textContent=msg;
  if(!err)setTimeout(function(){s.textContent=''},2000);
}

function renderProxies(){
  var el=document.getElementById('proxyList');
  if(!pxConfigs.length){el.innerHTML='<span class="empty">Прокси не настроены</span>';return;}
  el.innerHTML=pxConfigs.map(function(c){
    return '<div class="row" style="padding:.4rem 0;border-bottom:1px solid #222;flex-wrap:wrap;gap:.4rem">'
      +'<span style="font-weight:600;font-size:.88rem">'+c.name+'</span>'
      +'<span style="font-size:.75rem;background:#1e3a5f;color:#6baed6;border-radius:3px;padding:1px 6px">'+c.type+'</span>'
      +'<span style="font-size:.78rem;color:#666;font-family:monospace;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">'+c.config+'</span>'
      +(c.enabled?'':'<span style="font-size:.75rem;color:#888">[выкл]</span>')
      +'<span style="font-size:.75rem;color:#666">p:'+c.priority+'</span>'
      +'<button class="btn btn-ghost" style="padding:2px 8px;font-size:.78rem" onclick="testProxy('+c.id+')">Тест</button>'
      +'<button class="btn btn-ghost" style="padding:2px 8px;font-size:.78rem" onclick="editProxy('+c.id+')">Изм.</button>'
      +'<button class="btn btn-danger" style="padding:2px 8px;font-size:.78rem" onclick="delProxy('+c.id+')">✕</button>'
      +'</div>';
  }).join('');
}

function renderRouting(){
  var el=document.getElementById('routingList');
  if(!pxRoutes.length){el.innerHTML='<span class="empty">Нет маршрутов</span>';return;}
  el.innerHTML=pxRoutes.map(function(rt){
    var ids=rt.proxy_ids||[];
    var allChk=ids.length===0?' checked':'';
    var itemStyle='display:inline-flex;align-items:center;gap:4px;font-size:.82rem;cursor:pointer;padding:2px 7px;border:1px solid #333;border-radius:4px;background:#1a1a1a';
    var btnLabel=(ids.length===pxConfigs.length&&pxConfigs.length>0)?'Снять все':'Выбрать все';
    var checks=pxConfigs.length===0
      ?'<span style="font-size:.78rem;color:#666">нет прокси</span>'
      :'<button class="btn btn-ghost" style="padding:2px 10px;font-size:.78rem;width:96px;flex-shrink:0;text-align:center" onclick="toggleRtAll(\''+rt.route+'\')" id="rt_all_'+rt.route+'">'+btnLabel+'</button>'
        +pxConfigs.map(function(c){
          var chk=ids.indexOf(c.id)>=0?' checked':'';
          var dis=c.enabled?'':' disabled';
          var st=itemStyle+(c.enabled?'':';opacity:.4;cursor:not-allowed');
          return '<label style="'+st+'">'
            +'<input type="checkbox" id="rt_px_'+rt.route+'_'+c.id+'"'+chk+dis+' onchange="updateRtBtn(\''+rt.route+'\')"> '+c.name+'</label>';
        }).join('');
    return '<div style="padding:.35rem 0;border-bottom:1px solid #222">'
      +'<div class="row" style="flex-wrap:wrap;gap:.4rem;margin-bottom:.3rem">'
      +'<label style="display:flex;align-items:center;gap:.4rem;font-size:.88rem;min-width:160px">'
      +'<input type="checkbox" id="rt_en_'+rt.route+'"'+(rt.enabled?' checked':'')+'>'
      +'<span>'+rt.label+'</span>'
      +'</label>'
      +'</div>'
      +'<div class="row" style="flex-wrap:wrap;gap:.3rem;margin-left:1rem">'+checks+'</div>'
      +'</div>';
  }).join('');
}

function toggleRtAll(route){
  var btn=document.getElementById('rt_all_'+route);
  var enabled=pxConfigs.filter(function(c){return c.enabled;});
  var allSelected=enabled.length>0&&enabled.every(function(c){
    var el=document.getElementById('rt_px_'+route+'_'+c.id);
    return el&&el.checked;
  });
  var selectAll=!allSelected;
  enabled.forEach(function(c){
    var el=document.getElementById('rt_px_'+route+'_'+c.id);
    if(el)el.checked=selectAll;
  });
  if(btn)btn.textContent=selectAll?'Снять все':'Выбрать все';
}

function updateRtBtn(route){
  var btn=document.getElementById('rt_all_'+route);
  if(!btn)return;
  var enabled=pxConfigs.filter(function(c){return c.enabled;});
  var allSelected=enabled.length>0&&enabled.every(function(c){
    var el=document.getElementById('rt_px_'+route+'_'+c.id);
    return el&&el.checked;
  });
  btn.textContent=allSelected?'Снять все':'Выбрать все';
}

function loadProxies(){
  fetch('/api/admin/proxies/').then(function(r){return r.json();}).then(function(d){
    pxConfigs=d.configs||[];
    pxRoutes=d.routes||[];
    renderProxies();
    renderRouting();
  }).catch(function(){setPxStatus('Ошибка загрузки',true);});
}

var svgEye='<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>';
var svgEyeOff='<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/><line x1="1" y1="1" x2="23" y2="23"/></svg>';

function togglePxPw(){
  var inp=document.getElementById('pxPassword');
  var eye=document.getElementById('pxPwEye');
  if(inp.type==='password'){inp.type='text';eye.innerHTML=svgEyeOff;}
  else{inp.type='password';eye.innerHTML=svgEye;}
}

function buildSocks5Url(host,port,login,password){
  var hp=host+':'+port;
  if(!login)return 'socks5://'+hp;
  return 'socks5://'+encodeURIComponent(login)+':'+encodeURIComponent(password)+'@'+hp;
}

function parseSocks5(config){
  try{
    var u=new URL(config.replace('socks5h://','socks5://'));
    return{host:u.hostname,port:u.port||'1080',login:u.username?decodeURIComponent(u.username):'',password:u.password?decodeURIComponent(u.password):''};
  }catch(e){
    var bare=config.replace(/^socks5h?:\/\//,'');
    var lc=bare.lastIndexOf(':');
    return lc>0?{host:bare.slice(0,lc),port:bare.slice(lc+1),login:'',password:''}:{host:bare,port:'1080',login:'',password:''};
  }
}

function resetProxyForm(){
  document.getElementById('pxName').value='';
  document.getElementById('pxHost').value='';
  document.getElementById('pxPort').value='1080';
  document.getElementById('pxLogin').value='';
  document.getElementById('pxPassword').value='';
  document.getElementById('pxPassword').type='password';
  document.getElementById('pxPwEye').innerHTML=svgEye;
  document.getElementById('pxPriority').value='0';
  document.getElementById('pxEnabled').checked=true;
  document.getElementById('pxEditId').value='';
}

function editProxy(id){
  var c=pxConfigs.find(function(x){return x.id===id;});
  if(!c)return;
  var p=parseSocks5(c.config);
  document.getElementById('pxName').value=c.name;
  document.getElementById('pxHost').value=p.host;
  document.getElementById('pxPort').value=p.port;
  document.getElementById('pxLogin').value=p.login;
  document.getElementById('pxPassword').value=p.password;
  document.getElementById('pxPassword').type='password';
  document.getElementById('pxPwEye').innerHTML=svgEye;
  document.getElementById('pxPriority').value=c.priority;
  document.getElementById('pxEnabled').checked=c.enabled;
  document.getElementById('pxEditId').value=c.id;
  document.getElementById('proxyForm').scrollIntoView({behavior:'smooth'});
}

function saveProxy(){
  var id=document.getElementById('pxEditId').value;
  var host=document.getElementById('pxHost').value.trim();
  var port=document.getElementById('pxPort').value.trim()||'1080';
  var login=document.getElementById('pxLogin').value.trim();
  var password=document.getElementById('pxPassword').value;
  var config=buildSocks5Url(host,port,login,password);
  var body={
    name:document.getElementById('pxName').value.trim(),
    type:'socks5',
    config:config,
    priority:parseInt(document.getElementById('pxPriority').value)||0,
    enabled:document.getElementById('pxEnabled').checked
  };
  if(!body.name||!host){setPxStatus('Заполните поля',true);return;}
  var url=id?'/api/admin/proxies/'+id:'/api/admin/proxies/';
  var method=id?'PUT':'POST';
  fetch(url,{method:method,headers:{'Content-Type':'application/json'},body:JSON.stringify(body)})
    .then(function(r){if(!r.ok)throw new Error();return r;})
    .then(function(){setPxStatus(id?'Сохранено':'Добавлено');resetProxyForm();loadProxies();})
    .catch(function(){setPxStatus('Ошибка',true);});
}

function delProxy(id){
  if(!confirm('Удалить прокси?'))return;
  fetch('/api/admin/proxies/'+id,{method:'DELETE'})
    .then(function(r){if(r.ok)loadProxies();else setPxStatus('Ошибка',true);});
}

function testProxy(id){
  setPxStatus('Тестирование…');
  fetch('/api/admin/proxies/'+id+'/test',{method:'POST'})
    .then(function(r){return r.json();})
    .then(function(d){setPxStatus(d.ok?('✓ '+d.ms+'ms'):('✗ '+(d.error||d.status)));})
    .catch(function(){setPxStatus('Ошибка',true);});
}

function saveRouting(){
  var data=pxRoutes.map(function(rt){
    var en=document.getElementById('rt_en_'+rt.route);
    var ids=pxConfigs.reduce(function(acc,c){
      var cb=document.getElementById('rt_px_'+rt.route+'_'+c.id);
      if(cb&&cb.checked)acc.push(c.id);
      return acc;
    },[]);
    return{route:rt.route,enabled:en?en.checked:false,proxy_ids:ids};
  });
  fetch('/api/admin/proxies/routing',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(data)})
    .then(function(r){if(!r.ok)throw new Error();setRtStatus('Сохранено');})
    .catch(function(){setRtStatus('Ошибка',true);});
}

loadProxies();
</script>
</body></html>`

var parserRestartHTML = `<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Перезапуск</title>
<style>body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;
     min-height:100vh;margin:0;background:#111;color:#eee;text-align:center}p{color:#aaa}</style>
</head>
<body>
<div><p id="msg">Настройки сохранены. Сервис перезапускается…</p></div>
<script>
(function(){var n=0;function poll(){fetch('/health').then(function(r){if(r.ok){window.location.href='/admin';}else retry();}).catch(retry);}
function retry(){if(++n>60){document.getElementById('msg').textContent='Сервис не отвечает, проверьте вручную.';return;}setTimeout(poll,1500);}
setTimeout(poll,2000);})();
</script>
</body></html>`

// ─── Banned patterns ──────────────────────────────────────────────────────────

func loadBannedList(ctx context.Context) []string {
	val, _ := store.GetSetting(ctx, "banned_patterns")
	var list []string
	for _, line := range strings.Split(val, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			list = append(list, line)
		}
	}
	return list
}

func saveBannedList(ctx context.Context, list []string) {
	store.SetSetting(ctx, "banned_patterns", strings.Join(list, "\n"))
	invalidateBannedCache()
}

// GET /api/admin/banned-patterns
func handleAPIAdminBannedGet(w http.ResponseWriter, r *http.Request) {
	list := loadBannedList(r.Context())
	if list == nil {
		list = []string{}
	}
	JSON(w, http.StatusOK, list)
}

// POST /api/admin/banned-patterns  {"patterns": "lampa.mx cub.red"}
func handleAPIAdminBannedAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Patterns string `json:"patterns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Patterns == "" {
		Error(w, http.StatusBadRequest, "patterns required")
		return
	}
	parts := strings.FieldsFunc(body.Patterns, func(c rune) bool {
		return c == ' ' || c == ',' || c == ';' || c == '\n' || c == '\t'
	})
	existing := loadBannedList(r.Context())
	set := make(map[string]struct{}, len(existing))
	for _, p := range existing {
		set[p] = struct{}{}
	}
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			if _, dup := set[p]; !dup {
				existing = append(existing, p)
				set[p] = struct{}{}
			}
		}
	}
	saveBannedList(r.Context(), existing)
	JSON(w, http.StatusOK, existing)
}

// DELETE /api/admin/banned-patterns  {"pattern": "lampa.mx"}
func handleAPIAdminBannedDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Pattern string `json:"pattern"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Pattern == "" {
		Error(w, http.StatusBadRequest, "pattern required")
		return
	}
	list := loadBannedList(r.Context())
	filtered := list[:0]
	for _, p := range list {
		if p != body.Pattern {
			filtered = append(filtered, p)
		}
	}
	saveBannedList(r.Context(), filtered)
	if filtered == nil {
		filtered = []string{}
	}
	JSON(w, http.StatusOK, filtered)
}

// ─── Child blocked keywords ───────────────────────────────────────────────────

func loadChildKeywordList(ctx context.Context) []int {
	val, _ := store.GetSetting(ctx, "child_blocked_keywords")
	if strings.TrimSpace(val) == "" {
		return append([]int{}, DefaultChildBlockedKeywords...)
	}
	var ids []int
	for _, line := range strings.Split(val, "\n") {
		line = strings.TrimSpace(line)
		if id, err := strconv.Atoi(line); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

func saveChildKeywordList(ctx context.Context, ids []int) {
	lines := make([]string, len(ids))
	for i, id := range ids {
		lines[i] = strconv.Itoa(id)
	}
	store.SetSetting(ctx, "child_blocked_keywords", strings.Join(lines, "\n"))
	InvalidateCategoryCache()
}

// GET /api/admin/child-keywords
func handleAPIAdminChildKeywordsGet(w http.ResponseWriter, r *http.Request) {
	ids := loadChildKeywordList(r.Context())
	if ids == nil {
		ids = []int{}
	}
	JSON(w, http.StatusOK, ids)
}

// POST /api/admin/child-keywords  {"ids": "41278 13141"}
func handleAPIAdminChildKeywordsAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.IDs == "" {
		Error(w, http.StatusBadRequest, "ids required")
		return
	}
	existing := loadChildKeywordList(r.Context())
	set := make(map[int]struct{}, len(existing))
	for _, id := range existing {
		set[id] = struct{}{}
	}
	for _, s := range strings.FieldsFunc(body.IDs, func(c rune) bool {
		return c == ' ' || c == ',' || c == ';' || c == '\n' || c == '\t'
	}) {
		if id, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && id > 0 {
			if _, dup := set[id]; !dup {
				existing = append(existing, id)
				set[id] = struct{}{}
			}
		}
	}
	saveChildKeywordList(r.Context(), existing)
	JSON(w, http.StatusOK, existing)
}

// DELETE /api/admin/child-keywords  {"id": 41278}
func handleAPIAdminChildKeywordsDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == 0 {
		Error(w, http.StatusBadRequest, "id required")
		return
	}
	list := loadChildKeywordList(r.Context())
	filtered := list[:0]
	for _, id := range list {
		if id != body.ID {
			filtered = append(filtered, id)
		}
	}
	saveChildKeywordList(r.Context(), filtered)
	if filtered == nil {
		filtered = []int{}
	}
	JSON(w, http.StatusOK, filtered)
}

// GET /api/admin/child-keywords/resolve — fetch names for current blocked keyword IDs from TMDB
func handleAPIAdminChildKeywordsResolve(w http.ResponseWriter, r *http.Request) {
	ids := loadChildKeywordList(r.Context())
	token := tmdb.TMDBAuthKey
	type kw struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	out := make([]kw, 0, len(ids))
	for _, id := range ids {
		name := ""
		if token != "" {
			url := fmt.Sprintf("https://api.themoviedb.org/3/keyword/%d", id)
			if req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil); err == nil {
				req.Header.Set("Authorization", token)
				req.Header.Set("Accept", "application/json")
				if resp, err := tmdb.HTTPClient().Do(req); err == nil && resp.StatusCode == http.StatusOK {
					var res struct {
						Name string `json:"name"`
					}
					if json.NewDecoder(resp.Body).Decode(&res) == nil {
						name = res.Name
					}
					resp.Body.Close()
				}
			}
		}
		out = append(out, kw{ID: id, Name: name})
	}
	JSON(w, http.StatusOK, out)
}

// GET /api/admin/child-keywords/search?q=nudity — search TMDB keywords by name
func handleAPIAdminChildKeywordsSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		JSON(w, http.StatusOK, []any{})
		return
	}
	token := tmdb.TMDBAuthKey
	if token == "" {
		Error(w, http.StatusServiceUnavailable, "TMDB not configured")
		return
	}
	url := "https://api.themoviedb.org/3/search/keyword?query=" + strings.ReplaceAll(q, " ", "+")
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		Error(w, http.StatusInternalServerError, "request error")
		return
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("Accept", "application/json")
	resp, err := tmdb.HTTPClient().Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		Error(w, http.StatusBadGateway, "TMDB error")
		return
	}
	defer resp.Body.Close()
	var result struct {
		Results []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		Error(w, http.StatusInternalServerError, "parse error")
		return
	}
	type kw struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	out := make([]kw, 0, len(result.Results))
	for _, r := range result.Results {
		out = append(out, kw{ID: r.ID, Name: r.Name})
	}
	JSON(w, http.StatusOK, out)
}

// POST /api/admin/child-keywords/reset — restore defaults
func handleAPIAdminChildKeywordsReset(w http.ResponseWriter, r *http.Request) {
	store.SetSetting(r.Context(), "child_blocked_keywords", "")
	InvalidateCategoryCache()
	JSON(w, http.StatusOK, DefaultChildBlockedKeywords)
}

// ─── Child text keywords ──────────────────────────────────────────────────────

// GET /api/admin/child-text-keywords
func handleAPIAdminChildTextKwGet(w http.ResponseWriter, r *http.Request) {
	val, _ := store.GetSetting(r.Context(), "child_text_keywords")
	var list []string
	for _, s := range strings.Split(val, "\n") {
		if s = strings.TrimSpace(s); s != "" {
			list = append(list, s)
		}
	}
	if list == nil {
		list = []string{}
	}
	JSON(w, http.StatusOK, list)
}

// POST /api/admin/child-text-keywords  {"words": "секс насилие"}
func handleAPIAdminChildTextKwAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Words string `json:"words"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Words == "" {
		Error(w, http.StatusBadRequest, "words required")
		return
	}
	val, _ := store.GetSetting(r.Context(), "child_text_keywords")
	set := map[string]struct{}{}
	var list []string
	for _, s := range strings.Split(val, "\n") {
		if s = strings.TrimSpace(strings.ToLower(s)); s != "" {
			if _, dup := set[s]; !dup {
				list = append(list, s)
				set[s] = struct{}{}
			}
		}
	}
	for _, s := range strings.FieldsFunc(body.Words, func(c rune) bool {
		return c == ',' || c == ';' || c == '\n' || c == '\t'
	}) {
		if s = strings.TrimSpace(strings.ToLower(s)); s != "" {
			if _, dup := set[s]; !dup {
				list = append(list, s)
				set[s] = struct{}{}
			}
		}
	}
	store.SetSetting(r.Context(), "child_text_keywords", strings.Join(list, "\n"))
	InvalidateCategoryCache()
	JSON(w, http.StatusOK, list)
}

// DELETE /api/admin/child-text-keywords  {"word": "секс"}
func handleAPIAdminChildTextKwDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Word string `json:"word"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Word == "" {
		Error(w, http.StatusBadRequest, "word required")
		return
	}
	val, _ := store.GetSetting(r.Context(), "child_text_keywords")
	var list []string
	for _, s := range strings.Split(val, "\n") {
		if s = strings.TrimSpace(s); s != "" && s != strings.ToLower(body.Word) {
			list = append(list, s)
		}
	}
	store.SetSetting(r.Context(), "child_text_keywords", strings.Join(list, "\n"))
	InvalidateCategoryCache()
	if list == nil {
		list = []string{}
	}
	JSON(w, http.StatusOK, list)
}

// GET /api/admin/child-text-keyword-ages
func handleAPIAdminChildTextAgesGet(w http.ResponseWriter, r *http.Request) {
	ages := cachedChildTextAges()
	JSON(w, http.StatusOK, ages)
}

// POST /api/admin/child-text-keyword-ages  {"ages": [0, 6]}
func handleAPIAdminChildTextAgesSave(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ages []int `json:"ages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	parts := make([]string, len(body.Ages))
	for i, a := range body.Ages {
		parts[i] = strconv.Itoa(a)
	}
	store.SetSetting(r.Context(), "child_text_keyword_ages", strings.Join(parts, ","))
	InvalidateCategoryCache()
	JSON(w, http.StatusOK, body.Ages)
}

func handleAPIAdminSystemStats(w http.ResponseWriter, _ *http.Request) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	uptime := time.Since(appStartTime)
	days := int(uptime.Hours()) / 24
	hours := int(uptime.Hours()) % 24
	minutes := int(uptime.Minutes()) % 60

	JSON(w, http.StatusOK, map[string]any{
		"uptime_days":    days,
		"uptime_hours":   hours,
		"uptime_minutes": minutes,
		"goroutines":     runtime.NumGoroutine(),
		"memory_mb":      ms.Sys / 1024 / 1024,
		"num_cpu":        runtime.NumCPU(),
	})
}
