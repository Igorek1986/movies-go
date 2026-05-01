package api

import (
	"encoding/json"
	"lampa-api/db/postgres"
	"lampa-api/db/store"
	"lampa-api/internal/render"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func handleAdminStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var users, devices, cards, timecodes int
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&users)             //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices`).Scan(&devices)         //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM media_cards`).Scan(&cards)       //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM timecodes`).Scan(&timecodes)     //nolint:errcheck
	JSON(w, http.StatusOK, map[string]int{
		"users":       users,
		"devices":     devices,
		"media_cards": cards,
		"timecodes":   timecodes,
	})
}

func handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := postgres.Pool.Query(r.Context(), `
		SELECT u.id, u.username, u.role, u.is_admin, u.created_at,
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
		ID          int64  `json:"id"`
		Username    string `json:"username"`
		Role        string `json:"role"`
		IsAdmin     bool   `json:"is_admin"`
		CreatedAt   string `json:"created_at"`
		DeviceCount int    `json:"device_count"`
	}
	var result []userView
	for rows.Next() {
		var u userView
		var createdAt time.Time
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.IsAdmin, &createdAt, &u.DeviceCount); err == nil {
			u.CreatedAt = createdAt.Format("2006-01-02T15:04:05Z")
			result = append(result, u)
		}
	}
	if result == nil {
		result = []userView{}
	}
	JSON(w, http.StatusOK, result)
}

// ─── Web history (session auth) ───────────────────────────────────────────────

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
			  WHERE device_id=$1 AND lampa_profile_id=$2 AND card_id=$3`,
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
		  WHERE device_id=$1 AND lampa_profile_id=$2 AND card_id=$3 AND counted_at IS NOT NULL`,
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
	if err := store.SetUserRole(r.Context(), id, req.Role); err != nil {
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

// ─── Admin HTML page ──────────────────────────────────────────────────────────

type adminUserRow struct {
	ID           int64
	Username     string
	Role         string
	IsAdmin      bool
	Blocked      bool
	BlockedSince string
	BlockReason  string
	CreatedAt    string
	DeviceCount  int
	DeviceLimit  string
	PremiumUntil string
}

type adminDashData struct {
	Users      []adminUserRow
	Roles      []string
	Success    string
	Error      string
	ParserDate string // current rutor_last_parsed_at in YYYY-MM-DD for date input
}

func requireAdminPage(w http.ResponseWriter, r *http.Request) bool {
	u := userFromCtx(r)
	if u == nil || !u.IsAdmin {
		http.Redirect(w, r, "/login", http.StatusFound)
		return false
	}
	return true
}

func adminRedirect(w http.ResponseWriter, r *http.Request, key, msg string) {
	v := url.Values{key: {msg}}
	http.Redirect(w, r, "/admin?"+v.Encode(), http.StatusFound)
}

// GET /admin
func handleAdminPage(w http.ResponseWriter, r *http.Request) {
	if !requireAdminPage(w, r) {
		return
	}
	u := userFromCtx(r)
	success := r.URL.Query().Get("success")
	errMsg := r.URL.Query().Get("error")

	rows, err := postgres.Pool.Query(r.Context(), `
		SELECT u.id, u.username, u.role, u.is_admin,
		       u.blocked_at, u.block_reason, u.created_at, u.premium_until,
		       COUNT(d.id) AS device_count
		FROM users u
		LEFT JOIN devices d ON d.user_id = u.id
		GROUP BY u.id
		ORDER BY u.id`)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var users []adminUserRow
	for rows.Next() {
		var row adminUserRow
		var blockedAt, premiumUntil *time.Time
		var blockReason *string
		var createdAt time.Time
		if err := rows.Scan(&row.ID, &row.Username, &row.Role, &row.IsAdmin,
			&blockedAt, &blockReason, &createdAt, &premiumUntil, &row.DeviceCount); err != nil {
			continue
		}
		row.CreatedAt = createdAt.Format("02.01.2006")
		if blockedAt != nil {
			row.Blocked = true
			row.BlockedSince = blockedAt.Format("02.01.2006")
		}
		if blockReason != nil {
			row.BlockReason = *blockReason
		}
		if premiumUntil != nil {
			row.PremiumUntil = premiumUntil.Format("02.01.2006")
		}
		lim := store.LimitsFor(row.Role)
		if lim.MaxDevices == 0 {
			row.DeviceLimit = "∞"
		} else {
			row.DeviceLimit = strconv.Itoa(lim.MaxDevices)
		}
		users = append(users, row)
	}
	if users == nil {
		users = []adminUserRow{}
	}

	parserDate := ""
	if t := store.LastParsedAt(); !t.IsZero() {
		parserDate = t.Format("2006-01-02")
	}

	render.Page(w, r, "admin_dashboard", u, adminDashData{
		Users:      users,
		Roles:      []string{"simple", "premium", "super"},
		Success:    success,
		Error:      errMsg,
		ParserDate: parserDate,
	})
}

// POST /admin/user/{id}/role
func handleAdminUserSetRole(w http.ResponseWriter, r *http.Request) {
	if !requireAdminPage(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		adminRedirect(w, r, "error", "invalid id")
		return
	}
	r.ParseForm() //nolint:errcheck
	role := r.FormValue("role")
	switch role {
	case "simple", "premium", "super":
	default:
		adminRedirect(w, r, "error", "invalid role")
		return
	}
	store.SetUserRole(r.Context(), id, role) //nolint:errcheck
	adminRedirect(w, r, "success", "Роль изменена")
}

// POST /admin/user/{id}/toggle-admin
func handleAdminUserToggleAdmin(w http.ResponseWriter, r *http.Request) {
	if !requireAdminPage(w, r) {
		return
	}
	u := userFromCtx(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id == u.ID {
		adminRedirect(w, r, "error", "недопустимая операция")
		return
	}
	postgres.Pool.Exec(r.Context(), `UPDATE users SET is_admin = NOT is_admin WHERE id = $1`, id) //nolint:errcheck
	adminRedirect(w, r, "success", "Изменено")
}

// POST /admin/user/{id}/block
func handleAdminUserBlock(w http.ResponseWriter, r *http.Request) {
	if !requireAdminPage(w, r) {
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	r.ParseForm() //nolint:errcheck
	reason := r.FormValue("reason")
	now := time.Now()
	if reason != "" {
		postgres.Pool.Exec(r.Context(), //nolint:errcheck
			`UPDATE users SET blocked_at = $1, block_reason = $2 WHERE id = $3 AND is_admin = false`, now, reason, id)
	} else {
		postgres.Pool.Exec(r.Context(), //nolint:errcheck
			`UPDATE users SET blocked_at = $1 WHERE id = $2 AND is_admin = false`, now, id)
	}
	adminRedirect(w, r, "success", "Пользователь заблокирован")
}

// POST /admin/user/{id}/unblock
func handleAdminUserUnblock(w http.ResponseWriter, r *http.Request) {
	if !requireAdminPage(w, r) {
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	postgres.Pool.Exec(r.Context(), //nolint:errcheck
		`UPDATE users SET blocked_at = NULL, block_reason = NULL WHERE id = $1`, id)
	adminRedirect(w, r, "success", "Пользователь разблокирован")
}

// POST /admin/user/{id}/delete
func handleAdminUserDelete(w http.ResponseWriter, r *http.Request) {
	if !requireAdminPage(w, r) {
		return
	}
	u := userFromCtx(r)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id == u.ID {
		adminRedirect(w, r, "error", "нельзя удалить себя")
		return
	}
	store.DeleteUser(r.Context(), id) //nolint:errcheck
	adminRedirect(w, r, "success", "Пользователь удалён")
}

// POST /admin/user/{id}/reset-sync — сброс счётчика MyShows синхронизаций
func handleAdminUserResetSync(w http.ResponseWriter, r *http.Request) {
	if !requireAdminPage(w, r) {
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	postgres.Pool.Exec(r.Context(), //nolint:errcheck
		`UPDATE users SET myshows_synced_today = 0 WHERE id = $1`, id)
	adminRedirect(w, r, "success", "Счётчик MyShows сброшен")
}

// POST /admin/user/{id}/cleanup-limits — сброс счётчиков импорта
func handleAdminUserCleanupLimits(w http.ResponseWriter, r *http.Request) {
	if !requireAdminPage(w, r) {
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	postgres.Pool.Exec(r.Context(), //nolint:errcheck
		`UPDATE users SET imported_today = 0 WHERE id = $1`, id)
	adminRedirect(w, r, "success", "Счётчики лимитов сброшены")
}

// POST /admin/run-expiry-check
func handleAdminRunExpiryCheck(w http.ResponseWriter, r *http.Request) {
	if !requireAdminPage(w, r) {
		return
	}
	adminRedirect(w, r, "success", "Проверка Premium запущена")
}

// POST /admin/extend-all-premium
func handleAdminExtendAllPremium(w http.ResponseWriter, r *http.Request) {
	if !requireAdminPage(w, r) {
		return
	}
	days := store.GetSettingInt(r.Context(), "premium_extend_all_days")
	if days <= 0 {
		days = 3
	}
	postgres.Pool.Exec(r.Context(), //nolint:errcheck
		`UPDATE users SET premium_until = COALESCE(premium_until, now()) + ($1 || ' days')::interval
		 WHERE role = 'premium'`, strconv.Itoa(days))
	adminRedirect(w, r, "success", "Premium продлён для всех пользователей")
}

// POST /admin/episodes-refresh
func handleAdminEpisodesRefresh(w http.ResponseWriter, r *http.Request) {
	if !requireAdminPage(w, r) {
		return
	}
	adminRedirect(w, r, "success", "Обновление эпизодов запущено")
}

// POST /admin/episodes-find-ids
func handleAdminEpisodesFindIDs(w http.ResponseWriter, r *http.Request) {
	if !requireAdminPage(w, r) {
		return
	}
	adminRedirect(w, r, "success", "Поиск MyShows ID запущен")
}

// POST /admin/parser-reset-date
func handleAdminParserResetDate(w http.ResponseWriter, r *http.Request) {
	if !requireAdminPage(w, r) {
		return
	}
	dateStr := r.FormValue("date")
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil || t.IsZero() {
		adminRedirect(w, r, "error", "Неверная дата")
		return
	}
	store.SetLastParsedAtTime(t)
	adminRedirect(w, r, "success", "Дата парсера сброшена на "+t.Format("02.01.2006"))
}

// ─── Admin settings page ──────────────────────────────────────────────────────

type settingRow struct {
	Key      string
	Label    string
	Value    string
	Textarea bool
	Checkbox bool
	ShowsKey string // for checkboxes: related input key to show/hide
}

type settingGroupRendered struct {
	Name string
	Rows []settingRow
}

type adminSettingsData struct {
	Groups  []settingGroupRendered
	Success string
	Error   string
}

// textareaKeys and checkboxKeys mirror FastAPI TEXTAREA_KEYS / CHECKBOX_KEYS.
var textareaSettingKeys = map[string]bool{
	"privacy_policy_content": true,
	"consent_content":        true,
}
var checkboxSettingKeys = map[string]string{
	"yandex_metrika_enabled":   "yandex_metrika_id",
	"google_analytics_enabled": "google_analytics_id",
}

// settingLabels mirrors FastAPI LABELS.
var settingLabels = map[string]string{
	"simple_device_limit":   "Simple — устройств",
	"simple_profile_limit":  "Simple — профилей",
	"simple_timecode_limit": "Simple — таймкодов на профиль",
	"simple_favorite_limit": "Simple — закладок на категорию",
	"simple_import_daily":   "Simple — импортов в сутки",
	"premium_device_limit":    "Premium — устройств",
	"premium_profile_limit":   "Premium — профилей",
	"premium_timecode_limit":  "Premium — таймкодов на профиль",
	"premium_favorite_limit":  "Premium — закладок на категорию",
	"premium_import_daily":    "Premium — импортов в сутки",
	"premium_myshows_daily":   "Premium — MyShows синков в сутки",
	"premium_duration_days":   "Premium — длительность (дней)",
	"super_device_limit":   "Super — устройств (0=∞)",
	"super_profile_limit":  "Super — профилей (0=∞)",
	"super_timecode_limit": "Super — таймкодов на профиль (0=∞)",
	"super_favorite_limit": "Super — закладок на категорию (0=∞)",
	"super_import_daily":   "Super — импортов в сутки (0=∞)",
	"super_myshows_daily":  "Super — MyShows синков в сутки (0=∞)",
	"episodes_future_threshold": "Порог будущих серий (меньше — обновляем)",
	"episodes_refresh_batch":    "Размер пачки при обновлении",
	"episodes_refresh_delay":    "Пауза между пачками (сек)",
	"inactive_delete_days":     "Автоудаление неактивных аккаунтов (дней, 0 = выкл)",
	"inactive_warn_days":       "Предупреждение об удалении аккаунта (дней до удаления)",
	"timecode_grace_days":      "Грейс-период таймкодов (дней)",
	"premium_warn_days":        "Предупреждение об истечении Premium (дней)",
	"premium_extend_all_days":  "Продлить всем Premium (дней)",
	"watched_threshold":        "Порог «просмотрено» (%)",
	"popular_period_days":      "Популярное — период (дней)",
	"daily_task_hour":          "Час запуска ежедневной задачи (0–23)",
	"default_timezone":         "Таймзона по умолчанию",
	"session_ttl_days":          "Срок сессии (дней)",
	"session_renew_days":        "Продление сессии (дней до истечения)",
	"device_token_ttl_days":     "Срок токена устройства (дней)",
	"device_code_ttl_minutes":   "TTL кода устройства (мин)",
	"telegram_link_ttl_minutes": "TTL кода Telegram (мин)",
	"reset_code_ttl_minutes":    "TTL кода сброса пароля (мин)",
	"pending_2fa_ttl_sec":       "Ожидание 2FA (сек)",
	"rate_login_max":          "Rate: login — попыток",
	"rate_login_window_sec":   "Rate: login — окно (сек)",
	"rate_register_max":       "Rate: register — попыток",
	"rate_register_window_sec": "Rate: register — окно (сек)",
	"rate_forgot_max":         "Rate: forgot — попыток",
	"rate_forgot_window_sec":  "Rate: forgot — окно (сек)",
	"rate_2fa_max":            "Rate: 2FA — попыток",
	"rate_2fa_window_sec":     "Rate: 2FA — окно (сек)",
	"sync_cooldown_sec":       "MyShows cooldown (сек)",
	"yandex_metrika_enabled":   "Яндекс.Метрика — включена",
	"yandex_metrika_id":        "Яндекс.Метрика ID",
	"google_analytics_enabled": "Google Analytics — включена",
	"google_analytics_id":      "Google Analytics ID",
	"site_name":              "Название сервиса",
	"contact_email":          "Контактный email",
	"privacy_policy_content": "Текст Политики обработки персональных данных (HTML)",
	"consent_content":        "Текст Согласия на обработку персональных данных (HTML)",
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
}

func buildSettingsGroups(all map[string]string) []settingGroupRendered {
	groups := make([]settingGroupRendered, 0, len(settingsGroupDefs))
	for _, gd := range settingsGroupDefs {
		g := settingGroupRendered{Name: gd.Name}
		for _, key := range gd.Keys {
			label := settingLabels[key]
			if label == "" {
				label = key
			}
			val := all[key]
			row := settingRow{
				Key:      key,
				Label:    label,
				Value:    val,
				Textarea: textareaSettingKeys[key],
				Checkbox: checkboxSettingKeys[key] != "",
				ShowsKey: checkboxSettingKeys[key],
			}
			g.Rows = append(g.Rows, row)
		}
		groups = append(groups, g)
	}
	return groups
}

// GET /admin/settings
func handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	if !requireAdminPage(w, r) {
		return
	}
	u := userFromCtx(r)
	all := store.GetAllSettings(r.Context())
	render.Page(w, r, "admin_settings", u, adminSettingsData{
		Groups:  buildSettingsGroups(all),
		Success: r.URL.Query().Get("success"),
		Error:   r.URL.Query().Get("error"),
	})
}

// POST /admin/settings
func handleAdminSettingsSave(w http.ResponseWriter, r *http.Request) {
	if !requireAdminPage(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		v := url.Values{"error": {"Ошибка запроса"}}
		http.Redirect(w, r, "/admin/settings?"+v.Encode(), http.StatusFound)
		return
	}
	// Collect all known keys from all groups
	for _, gd := range settingsGroupDefs {
		for _, key := range gd.Keys {
			if checkboxSettingKeys[key] != "" {
				// Checkbox: present = "1", absent = "0"
				if r.FormValue(key) == "1" {
					store.SetSetting(r.Context(), key, "1")
				} else {
					store.SetSetting(r.Context(), key, "0")
				}
				continue
			}
			val := r.FormValue(key)
			if textareaSettingKeys[key] {
				// Textarea: always save (allow empty)
				store.SetSetting(r.Context(), key, val)
			} else if val != "" {
				store.SetSetting(r.Context(), key, val)
			}
		}
	}
	store.InvalidateLimitsCache()
	v := url.Values{"success": {"Настройки сохранены"}}
	http.Redirect(w, r, "/admin/settings?"+v.Encode(), http.StatusFound)
}
