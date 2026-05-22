package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"movies-api/config"
	"movies-api/db/postgres"
	"movies-api/db/store"
	tasks "movies-api/internal/tasks"
	"movies-api/movies/tmdb"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

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

func requireParserAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !checkParserSession(r) {
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
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&users)                                                    //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE created_at::date = CURRENT_DATE`).Scan(&usersToday)         //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices`).Scan(&devices)                                                //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices WHERE created_at::date = CURRENT_DATE`).Scan(&devicesToday)     //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM media_cards`).Scan(&cards)                                              //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM media_cards WHERE created_at::date = CURRENT_DATE`).Scan(&cardsToday)   //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM timecodes`).Scan(&timecodes)                                            //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM timecodes WHERE created_at::date = CURRENT_DATE`).Scan(&timecodesToday) //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM media_cards WHERE media_type='movie' AND (runtime IS NULL OR runtime=0)`).Scan(&noRuntimeMovies)                        //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM media_cards WHERE media_type='tv' AND (episode_run_time IS NULL OR episode_run_time=0)`).Scan(&noRuntimeTV) //nolint:errcheck

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
		"users":             users,
		"users_today":       usersToday,
		"devices":           devices,
		"devices_today":     devicesToday,
		"media_cards":          cards,
		"media_cards_today":    cardsToday,
		"no_runtime_movies":    noRuntimeMovies,
		"no_runtime_tv":        noRuntimeTV,
		"timecodes":         timecodes,
		"timecodes_today":   timecodesToday,
		"new_users_today":   newUsersToday,
		"api_ips_today":     apiIPsToday,
		"api_reqs_today":    apiReqsToday,
		"api_today":         apiToday,
		"api_total":         apiTotal,
		"cats_today":        catsToday,
		"cats_total":        catsTotal,
		"myshows_today":     myshowsToday,
		"myshows_total":     myshowsTotal,
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
	{"Категории парсера (требует перезапуска)", []string{
		"movies_new_year_delta", "movies_new_min_quality", "movies_4k_year_delta",
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
			if checkboxSettingKeys[key] != "" {
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

// GET /admin — parser-mode only
func handleParserModeAdmin(w http.ResponseWriter, r *http.Request) {
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
.row{display:flex;gap:.5rem;align-items:center}
select,input[type=text]{flex:1;padding:.45rem .6rem;border-radius:4px;border:1px solid #333;
  background:#222;color:#eee;font-size:.9rem}
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
loadPats();
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
