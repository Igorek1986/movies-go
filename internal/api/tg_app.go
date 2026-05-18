package api

// Telegram Mini App — /tg-app
// Authentication: X-Telegram-Init-Data header with Telegram WebApp initData.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"movies-api/config"
	"movies-api/db/postgres"
	"movies-api/db/store"
	botpkg "movies-api/internal/bot"
	"movies-api/internal/tasks"
)

// ─── initData validation ──────────────────────────────────────────────────────

type tgUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

// validateInitData verifies the Telegram WebApp initData signature and returns parsed user.
// Returns nil if signature is invalid.
func validateInitData(initData, botToken string) *tgUser {
	vals, err := url.ParseQuery(initData)
	if err != nil {
		return nil
	}
	hashVal := vals.Get("hash")
	if hashVal == "" {
		return nil
	}

	// Build data-check-string: sorted key=value pairs (excluding hash), joined by \n
	var pairs []string
	for k, vs := range vals {
		if k == "hash" {
			continue
		}
		pairs = append(pairs, k+"="+vs[0])
	}
	sort.Strings(pairs)
	dataCheckString := strings.Join(pairs, "\n")

	// secret_key = HMAC-SHA256("WebAppData", bot_token)
	mac := hmac.New(sha256.New, []byte("WebAppData"))
	mac.Write([]byte(botToken))
	secretKey := mac.Sum(nil)

	// computed = HMAC-SHA256(secret_key, data_check_string)
	mac2 := hmac.New(sha256.New, secretKey)
	mac2.Write([]byte(dataCheckString))
	computed := hex.EncodeToString(mac2.Sum(nil))

	if !hmac.Equal([]byte(computed), []byte(hashVal)) {
		return nil
	}

	userJSON := vals.Get("user")
	if userJSON == "" {
		return &tgUser{}
	}
	var u tgUser
	if err := json.Unmarshal([]byte(userJSON), &u); err != nil {
		return nil
	}
	return &u
}

// tgAppCtxKey is context key for the mini-app auth context.
type tgAppCtxKey struct{}

type tgAppCtx struct {
	TgUser  *tgUser
	IsAdmin bool
}

func tgAppFromCtx(r *http.Request) *tgAppCtx {
	v, _ := r.Context().Value(tgAppCtxKey{}).(*tgAppCtx)
	return v
}

// requireTgAuth middleware validates initData from X-Telegram-Init-Data header.
func requireTgAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := config.Get().TelegramBotToken
		if token == "" {
			Error(w, http.StatusServiceUnavailable, "telegram bot not configured")
			return
		}
		initData := r.Header.Get("X-Telegram-Init-Data")
		if initData == "" {
			Error(w, http.StatusUnauthorized, "missing X-Telegram-Init-Data")
			return
		}
		u := validateInitData(initData, token)
		if u == nil {
			Error(w, http.StatusUnauthorized, "invalid initData")
			return
		}
		ctx := context.WithValue(r.Context(), tgAppCtxKey{}, &tgAppCtx{
			TgUser:  u,
			IsAdmin: botpkg.IsAdmin(u.ID),
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requireTgAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac := tgAppFromCtx(r)
		if ac == nil || !ac.IsAdmin {
			Error(w, http.StatusForbidden, "admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ─── Route registration (called from router.go) ───────────────────────────────

func registerTgAppRoutes(r chi.Router) {
	r.Route("/tg-app", func(r chi.Router) {
		r.Route("/api", func(r chi.Router) {
			r.Post("/auth", handleTgAppAuth)

			r.Group(func(r chi.Router) {
				r.Use(requireTgAuth)
				r.Get("/me", handleTgAppMe)
				r.Post("/me/unlink", handleTgAppUnlink)
				r.Post("/me/devices/create", handleTgAppCreateDevice)
				r.Get("/me/devices/{device_id}", handleTgAppDeviceDetails)
				r.Post("/me/devices/{device_id}/rename", handleTgAppRenameDevice)
				r.Post("/me/devices/{device_id}/delete", handleTgAppDeleteDevice)
				r.Post("/me/devices/{device_id}/regenerate", handleTgAppRegenerateToken)
				r.Post("/me/devices/{device_id}/clear-timecodes", handleTgAppClearTimecodes)
				r.Post("/me/devices/{device_id}/profiles/create", handleTgAppCreateProfile)
				r.Post("/me/devices/{device_id}/profiles/{profile_id}/delete", handleTgAppDeleteProfile)
			})

			r.Group(func(r chi.Router) {
				r.Use(requireTgAuth, requireTgAdmin)
				r.Get("/stats", handleTgAppStats)
				r.Get("/users", handleTgAppUsers)
				r.Post("/users/{id}/role", handleTgAppSetRole)
				r.Post("/users/{id}/block", handleTgAppBlockUser)
				r.Post("/users/{id}/unblock", handleTgAppUnblockUser)
				r.Post("/users/{id}/reset-myshows", handleTgAppResetMyShows)
				r.Post("/users/{id}/cleanup-limits", handleTgAppCleanupLimits)
				r.Post("/users/{id}/delete", handleTgAppDeleteUser)
				r.Post("/admin/check-premium", handleTgAppCheckPremium)
				r.Post("/admin/extend-premium", handleTgAppExtendPremium)
				r.Post("/admin/refresh-episodes", handleTgAppRefreshEpisodes)
				r.Post("/admin/fix-runtime", handleTgAppFixRuntime)
				r.Post("/admin/reset-parser", handleTgAppResetParser)
				r.Get("/messages", handleTgAppMessages)
				r.Post("/messages/{tg_id}/reply", handleTgAppReply)
				r.Post("/messages/{tg_id}/read", handleTgAppMarkRead)
				r.Get("/settings", handleTgAppGetSettings)
				r.Post("/settings", handleTgAppUpdateSetting)
			})
		})
	})
}

// ─── Auth ─────────────────────────────────────────────────────────────────────

func handleTgAppAuth(w http.ResponseWriter, r *http.Request) {
	token := config.Get().TelegramBotToken
	if token == "" {
		Error(w, http.StatusServiceUnavailable, "telegram bot not configured")
		return
	}
	var body struct {
		InitData string `json:"initData"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	u := validateInitData(body.InitData, token)
	if u == nil {
		Error(w, http.StatusUnauthorized, "invalid initData")
		return
	}
	isAdmin := botpkg.IsAdmin(u.ID)
	link := store.GetTelegramLinkByTelegramID(r.Context(), u.ID)
	JSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"is_admin":  isAdmin,
		"is_linked": link != nil,
		"user": map[string]any{
			"id":         u.ID,
			"first_name": u.FirstName,
			"username":   u.Username,
		},
	})
}

// ─── Me / User ────────────────────────────────────────────────────────────────

func handleTgAppMe(w http.ResponseWriter, r *http.Request) {
	ac := tgAppFromCtx(r)
	link := store.GetTelegramLinkByTelegramID(r.Context(), ac.TgUser.ID)
	if link == nil {
		Error(w, http.StatusForbidden, "telegram not linked to any account")
		return
	}
	u := store.GetUserByID(r.Context(), link.UserID)
	if u == nil {
		Error(w, http.StatusNotFound, "account not found")
		return
	}
	lim := store.LimitsFor(u.Role)
	devices := store.GetDevicesWithStats(r.Context(), u.ID)

	roleLabels := map[string]string{"simple": "Базовый", "premium": "Премиум", "super": "Супер"}
	devOut := make([]map[string]any, 0, len(devices))
	for _, d := range devices {
		var tcCount int
		postgres.Pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM timecodes WHERE device_id=$1`, d.ID).Scan(&tcCount) //nolint:errcheck
		devOut = append(devOut, map[string]any{
			"id":              d.ID,
			"name":            d.Name,
			"timecodes_count": tcCount,
			"created_at":      d.CreatedAt.Format("02.01.2006"),
		})
	}
	JSON(w, http.StatusOK, map[string]any{
		"username":      u.Username,
		"role":          u.Role,
		"role_label":    roleLabels[u.Role],
		"device_count":  len(devices),
		"device_limit":  lim.MaxDevices,
		"devices":       devOut,
	})
}

func handleTgAppUnlink(w http.ResponseWriter, r *http.Request) {
	ac := tgAppFromCtx(r)
	link := store.GetTelegramLinkByTelegramID(r.Context(), ac.TgUser.ID)
	if link == nil {
		Error(w, http.StatusNotFound, "not linked")
		return
	}
	store.DeleteTelegramLink(r.Context(), link.UserID)
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ─── Device management ────────────────────────────────────────────────────────

func tgLinkedUser(r *http.Request) (int64, bool) {
	ac := tgAppFromCtx(r)
	link := store.GetTelegramLinkByTelegramID(r.Context(), ac.TgUser.ID)
	if link == nil {
		return 0, false
	}
	return link.UserID, true
}

func tgOwnedDevice(r *http.Request, userID int64) int64 {
	deviceID, _ := strconv.ParseInt(chi.URLParam(r, "device_id"), 10, 64)
	if deviceID == 0 {
		return 0
	}
	var ownerID int64
	postgres.Pool.QueryRow(r.Context(), `SELECT user_id FROM devices WHERE id=$1`, deviceID).Scan(&ownerID) //nolint:errcheck
	if ownerID != userID {
		return 0
	}
	return deviceID
}

func handleTgAppCreateDevice(w http.ResponseWriter, r *http.Request) {
	userID, ok := tgLinkedUser(r)
	if !ok {
		Error(w, http.StatusForbidden, "not linked")
		return
	}
	var body struct{ Name string `json:"name"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		Error(w, http.StatusBadRequest, "name required")
		return
	}
	u := store.GetUserByID(r.Context(), userID)
	lim := store.LimitsFor(u.Role).MaxDevices
	devices := store.GetDevicesWithStats(r.Context(), userID)
	if lim > 0 && len(devices) >= lim {
		Error(w, http.StatusForbidden, "device limit reached")
		return
	}
	d, err := store.CreateDevice(r.Context(), userID, name)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed")
		return
	}
	JSON(w, http.StatusOK, map[string]any{"ok": true, "id": d.ID, "name": d.Name, "token": d.Token})
}

func handleTgAppDeviceDetails(w http.ResponseWriter, r *http.Request) {
	userID, ok := tgLinkedUser(r)
	if !ok {
		Error(w, http.StatusForbidden, "not linked")
		return
	}
	deviceID := tgOwnedDevice(r, userID)
	if deviceID == 0 {
		Error(w, http.StatusNotFound, "device not found")
		return
	}
	u := store.GetUserByID(r.Context(), userID)
	profiles := store.ListProfiles(r.Context(), deviceID)
	var token, name string
	var createdAt time.Time
	postgres.Pool.QueryRow(r.Context(), `SELECT token, name, created_at FROM devices WHERE id=$1`, deviceID).Scan(&token, &name, &createdAt) //nolint:errcheck
	JSON(w, http.StatusOK, map[string]any{
		"id":            deviceID,
		"name":          name,
		"token":         token,
		"profile_limit": store.LimitsFor(u.Role).MaxProfiles,
		"profiles":      profiles,
	})
}

func handleTgAppRenameDevice(w http.ResponseWriter, r *http.Request) {
	userID, ok := tgLinkedUser(r)
	if !ok {
		Error(w, http.StatusForbidden, "not linked")
		return
	}
	deviceID := tgOwnedDevice(r, userID)
	if deviceID == 0 {
		Error(w, http.StatusNotFound, "device not found")
		return
	}
	var body struct{ Name string `json:"name"` }
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
	name := strings.TrimSpace(body.Name)
	if name == "" {
		Error(w, http.StatusBadRequest, "name required")
		return
	}
	store.RenameDevice(r.Context(), deviceID, userID, name)
	JSON(w, http.StatusOK, map[string]any{"ok": true, "name": name})
}

func handleTgAppDeleteDevice(w http.ResponseWriter, r *http.Request) {
	userID, ok := tgLinkedUser(r)
	if !ok {
		Error(w, http.StatusForbidden, "not linked")
		return
	}
	deviceID := tgOwnedDevice(r, userID)
	if deviceID == 0 {
		Error(w, http.StatusNotFound, "device not found")
		return
	}
	store.DeleteDevice(r.Context(), deviceID, userID)
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleTgAppRegenerateToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := tgLinkedUser(r)
	if !ok {
		Error(w, http.StatusForbidden, "not linked")
		return
	}
	deviceID := tgOwnedDevice(r, userID)
	if deviceID == 0 {
		Error(w, http.StatusNotFound, "device not found")
		return
	}
	token, err := store.RegenerateToken(r.Context(), deviceID, userID)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed")
		return
	}
	JSON(w, http.StatusOK, map[string]any{"ok": true, "token": token})
}

func handleTgAppClearTimecodes(w http.ResponseWriter, r *http.Request) {
	userID, ok := tgLinkedUser(r)
	if !ok {
		Error(w, http.StatusForbidden, "not linked")
		return
	}
	deviceID := tgOwnedDevice(r, userID)
	if deviceID == 0 {
		Error(w, http.StatusNotFound, "device not found")
		return
	}
	store.ClearDeviceTimecodes(r.Context(), deviceID, userID) //nolint:errcheck
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleTgAppCreateProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := tgLinkedUser(r)
	if !ok {
		Error(w, http.StatusForbidden, "not linked")
		return
	}
	deviceID := tgOwnedDevice(r, userID)
	if deviceID == 0 {
		Error(w, http.StatusNotFound, "device not found")
		return
	}
	var body struct {
		Name      string `json:"name"`
		ProfileID string `json:"profile_id"`
	}
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
	name := strings.TrimSpace(body.Name)
	if name == "" {
		Error(w, http.StatusBadRequest, "name required")
		return
	}
	u := store.GetUserByID(r.Context(), userID)
	maxProfiles := store.LimitsFor(u.Role).MaxProfiles
	if maxProfiles > 0 && store.CountProfiles(r.Context(), deviceID) >= maxProfiles {
		Error(w, http.StatusForbidden, "profile limit reached")
		return
	}
	lp, err := store.CreateProfile(r.Context(), deviceID, strings.TrimSpace(body.ProfileID), name, "")
	if err != nil {
		Error(w, http.StatusForbidden, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]any{"ok": true, "profile_id": lp.ProfileID, "name": name})
}

func handleTgAppDeleteProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := tgLinkedUser(r)
	if !ok {
		Error(w, http.StatusForbidden, "not linked")
		return
	}
	deviceID := tgOwnedDevice(r, userID)
	if deviceID == 0 {
		Error(w, http.StatusNotFound, "device not found")
		return
	}
	profileID := chi.URLParam(r, "profile_id")
	store.DeleteProfile(r.Context(), deviceID, profileID)
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ─── Admin: Stats ─────────────────────────────────────────────────────────────

func handleTgAppStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var totalUsers, totalDevices, totalTCs, tgLinked, newToday, activeTC int
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&totalUsers)                                                                               //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices`).Scan(&totalDevices)                                                                          //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM timecodes`).Scan(&totalTCs)                                                                            //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM telegram_users`).Scan(&tgLinked)                                                                       //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE created_at::date = CURRENT_DATE`).Scan(&newToday)                                          //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM timecodes WHERE updated_at::date = CURRENT_DATE`).Scan(&activeTC)                                      //nolint:errcheck

	roleCounts := map[string]int{}
	rows, _ := postgres.Pool.Query(ctx, `SELECT role, COUNT(*) FROM users GROUP BY role`)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var role string
			var cnt int
			rows.Scan(&role, &cnt) //nolint:errcheck
			roleCounts[role] = cnt
		}
	}

	var unreadSupport int
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM support_messages WHERE direction='in' AND is_read=false`).Scan(&unreadSupport) //nolint:errcheck

	JSON(w, http.StatusOK, map[string]any{
		"total_users":     totalUsers,
		"total_devices":   totalDevices,
		"total_timecodes": totalTCs,
		"tg_linked":       tgLinked,
		"new_users_today": newToday,
		"tcs_today":       activeTC,
		"role_counts":     roleCounts,
		"unread_support":  unreadSupport,
	})
}

// ─── Admin: Users ─────────────────────────────────────────────────────────────

func handleTgAppUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	ctx := r.Context()

	filter := "%"
	if q != "" {
		filter = "%" + q + "%"
	}
	rows, err := postgres.Pool.Query(ctx,
		`SELECT u.id, u.username, u.role, u.created_at, u.blocked_at, u.block_reason,
		        (SELECT COUNT(*) FROM devices WHERE user_id=u.id),
		        tu.telegram_id, tu.username, u.premium_until
		 FROM users u
		 LEFT JOIN telegram_users tu ON tu.user_id=u.id
		 WHERE u.username ILIKE $1
		 ORDER BY u.id DESC LIMIT 200`, filter)
	if err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	type userRow struct {
		ID           int64   `json:"id"`
		Username     string  `json:"username"`
		Role         string  `json:"role"`
		CreatedAt    string  `json:"created_at"`
		BlockedAt    *string `json:"blocked_at,omitempty"`
		BlockReason  *string `json:"block_reason,omitempty"`
		DeviceCount  int     `json:"device_count"`
		TgID         *int64  `json:"tg_id,omitempty"`
		TgUsername   *string `json:"tg_username,omitempty"`
		PremiumUntil *string `json:"premium_until,omitempty"`
	}

	var users []userRow
	for rows.Next() {
		var u userRow
		var createdAt time.Time
		var blockedAt *time.Time
		var premiumUntil *time.Time
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &createdAt, &blockedAt, &u.BlockReason, &u.DeviceCount, &u.TgID, &u.TgUsername, &premiumUntil); err != nil {
			continue
		}
		u.CreatedAt = createdAt.Format("02.01.2006")
		if blockedAt != nil {
			s := blockedAt.Format("02.01.2006")
			u.BlockedAt = &s
		}
		if premiumUntil != nil {
			s := premiumUntil.Format("02.01.2006")
			u.PremiumUntil = &s
		}
		users = append(users, u)
	}
	if users == nil {
		users = []userRow{}
	}
	JSON(w, http.StatusOK, map[string]any{"users": users})
}

func handleTgAppSetRole(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var body struct{ Role string `json:"role"` }
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
	if body.Role != "simple" && body.Role != "premium" && body.Role != "super" {
		Error(w, http.StatusBadRequest, "invalid role")
		return
	}
	var roleErr error
	if body.Role == "premium" {
		_, roleErr = postgres.Pool.Exec(r.Context(),
			`UPDATE users SET role='premium', premium_until=COALESCE(premium_until, now()) + interval '30 days' WHERE id=$1`, id)
	} else {
		roleErr = store.SetUserRole(r.Context(), id, body.Role)
		if roleErr == nil {
			postgres.Pool.Exec(r.Context(), `UPDATE users SET premium_until=NULL WHERE id=$1`, id) //nolint:errcheck
		}
	}
	if roleErr != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	store.InvalidateLimitsCache()
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleTgAppBlockUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var body struct{ Reason string `json:"reason"` }
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

	reason := strings.TrimSpace(body.Reason)
	var reasonPtr *string
	if reason != "" {
		reasonPtr = &reason
	}
	now := time.Now()
	postgres.Pool.Exec(r.Context(), //nolint:errcheck
		`UPDATE users SET blocked_at=$2, block_reason=$3 WHERE id=$1`,
		id, now, reasonPtr)
	// Revoke all sessions
	postgres.Pool.Exec(r.Context(), `DELETE FROM sessions WHERE user_id=$1`, id) //nolint:errcheck
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleTgAppUnblockUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	postgres.Pool.Exec(r.Context(), `UPDATE users SET blocked_at=NULL, block_reason=NULL WHERE id=$1`, id) //nolint:errcheck
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ─── Admin: global actions ────────────────────────────────────────────────────

func handleTgAppCheckPremium(w http.ResponseWriter, r *http.Request) {
	postgres.Pool.Exec(r.Context(), //nolint:errcheck
		`UPDATE users SET role='simple', premium_until=NULL
		 WHERE role='premium' AND premium_until IS NOT NULL AND premium_until < now()`)
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleTgAppExtendPremium(w http.ResponseWriter, r *http.Request) {
	days := store.GetSettingInt(r.Context(), "premium_extend_all_days")
	if days <= 0 {
		days = 3
	}
	postgres.Pool.Exec(r.Context(), //nolint:errcheck
		`UPDATE users SET premium_until=COALESCE(premium_until, now()) + ($1 || ' days')::interval
		 WHERE role='premium'`, strconv.Itoa(days))
	JSON(w, http.StatusOK, map[string]any{"ok": true, "days": days})
}

func handleTgAppRefreshEpisodes(w http.ResponseWriter, r *http.Request) {
	go tasks.RunRefreshOngoingEpisodes(tasks.AppCtx())
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleTgAppFixRuntime(w http.ResponseWriter, r *http.Request) {
	if tasks.GetFixRuntimeStatus().Running {
		JSON(w, http.StatusOK, map[string]any{"ok": false, "message": "already running"})
		return
	}
	go tasks.RunFixZeroRuntime(tasks.AppCtx())
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleTgAppResetParser(w http.ResponseWriter, r *http.Request) {
	var body struct{ Date string `json:"date"` }
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
	t, err := time.ParseInLocation("02.01.2006", body.Date, time.UTC)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid date, expected DD.MM.YYYY")
		return
	}
	store.SetLastParsedAtTime(t)
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleTgAppResetMyShows(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ResetUserSyncCounter(id)
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleTgAppCleanupLimits(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var role string
	postgres.Pool.QueryRow(r.Context(), `SELECT role FROM users WHERE id=$1`, id).Scan(&role) //nolint:errcheck
	deleted := store.CleanupUserOverlimit(r.Context(), id, role)
	JSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": deleted})
}

func handleTgAppDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := store.DeleteUser(r.Context(), id); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ─── Admin: Support messages ───────────────────────────────────────────────────

func handleTgAppMessages(w http.ResponseWriter, r *http.Request) {
	rows, err := postgres.Pool.Query(r.Context(),
		`SELECT id, user_telegram_id, COALESCE(user_username,''), direction, text, is_read, created_at
		 FROM support_messages ORDER BY created_at DESC LIMIT 300`)
	if err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	type msgRow struct {
		ID            int64  `json:"id"`
		UserTelegramID int64 `json:"user_telegram_id"`
		UserUsername  string `json:"user_username"`
		Direction     string `json:"direction"`
		Text          string `json:"text"`
		IsRead        bool   `json:"is_read"`
		CreatedAt     string `json:"created_at"`
	}

	convMap := map[int64]*map[string]any{}
	convOrder := []int64{}
	for rows.Next() {
		var m msgRow
		var createdAt time.Time
		if err := rows.Scan(&m.ID, &m.UserTelegramID, &m.UserUsername, &m.Direction, &m.Text, &m.IsRead, &createdAt); err != nil {
			continue
		}
		m.CreatedAt = createdAt.Format("02.01.2006 15:04")
		uid := m.UserTelegramID
		if _, ok := convMap[uid]; !ok {
			conv := map[string]any{
				"user_telegram_id": uid,
				"user_username":    m.UserUsername,
				"messages":         []msgRow{},
				"has_unread":       false,
			}
			convMap[uid] = &conv
			convOrder = append(convOrder, uid)
		}
		conv := *convMap[uid]
		conv["messages"] = append(conv["messages"].([]msgRow), m)
		if m.Direction == "in" && !m.IsRead {
			conv["has_unread"] = true
		}
	}

	convList := make([]map[string]any, 0, len(convOrder))
	for _, uid := range convOrder {
		convList = append(convList, *convMap[uid])
	}
	JSON(w, http.StatusOK, map[string]any{"conversations": convList})
}

func handleTgAppReply(w http.ResponseWriter, r *http.Request) {
	tgID, _ := strconv.ParseInt(chi.URLParam(r, "tg_id"), 10, 64)
	var body struct{ Text string `json:"text"` }
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
	if strings.TrimSpace(body.Text) == "" {
		Error(w, http.StatusBadRequest, "empty message")
		return
	}
	ac := tgAppFromCtx(r)
	adminID := ac.TgUser.ID
	store.SaveSupportMessage(r.Context(), tgID, "", "out", body.Text, &adminID, nil)
	botpkg.SendMessage(tgID, "💬 <b>Ответ от поддержки:</b>\n\n"+body.Text)
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleTgAppMarkRead(w http.ResponseWriter, r *http.Request) {
	tgID, _ := strconv.ParseInt(chi.URLParam(r, "tg_id"), 10, 64)
	postgres.Pool.Exec(r.Context(), //nolint:errcheck
		`UPDATE support_messages SET is_read=true WHERE user_telegram_id=$1 AND direction='in' AND is_read=false`,
		tgID)
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ─── Admin: App settings ──────────────────────────────────────────────────────

func handleTgAppGetSettings(w http.ResponseWriter, r *http.Request) {
	all := store.GetAllSettings(r.Context())
	groups := buildTgSettingsGroups(all)
	JSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func handleTgAppUpdateSetting(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
	if _, ok := store.SettingDefaults[body.Key]; !ok {
		Error(w, http.StatusBadRequest, "unknown key")
		return
	}
	store.SetSetting(r.Context(), body.Key, strings.TrimSpace(body.Value))
	store.InvalidateLimitsCache()
	JSON(w, http.StatusOK, map[string]any{"ok": true, "key": body.Key, "value": body.Value})
}

// buildTgSettingsGroups groups settings by category for the UI.
func buildTgSettingsGroups(all map[string]string) []map[string]any {
	groups := []struct {
		Name string
		Keys []string
	}{
		{"Лимиты simple", []string{"simple_device_limit", "simple_profile_limit", "simple_timecode_limit", "simple_favorite_limit", "simple_import_daily"}},
		{"Лимиты premium", []string{"premium_device_limit", "premium_profile_limit", "premium_timecode_limit", "premium_favorite_limit", "premium_import_daily", "premium_myshows_daily", "premium_duration_days"}},
		{"Подписка", []string{"premium_warn_days", "timecode_grace_days", "inactive_delete_days", "inactive_warn_days"}},
		{"Парсер", []string{"parser_overlap_days"}},
		{"Бот / Telegram", []string{"telegram_link_ttl_minutes", "reset_code_ttl_minutes", "daily_task_hour", "default_timezone"}},
		{"Сессии / Устройства", []string{"session_ttl_days", "device_token_ttl_days", "device_code_ttl_minutes"}},
	}
	var result []map[string]any
	for _, g := range groups {
		items := make([]map[string]any, 0, len(g.Keys))
		for _, k := range g.Keys {
			items = append(items, map[string]any{
				"key":     k,
				"value":   all[k],
				"default": store.SettingDefaults[k],
			})
		}
		result = append(result, map[string]any{"name": g.Name, "items": items})
	}
	return result
}

