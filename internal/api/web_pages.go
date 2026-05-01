package api

// Handlers for server-side HTML pages (Go html/template, replaces FastAPI Jinja2 pages).

import (
	"fmt"
	"lampa-api/config"
	"lampa-api/db/postgres"
	"lampa-api/db/store"
	"lampa-api/internal/auth"
	"lampa-api/internal/render"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// ─── Login ────────────────────────────────────────────────────────────────────

// GET /login
func handleLoginPage(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	render.Page(w, r, "login", nil, nil)
}

// POST /login  (form submission)
func handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		render.Page(w, r, "login", nil, map[string]string{"Error": "Ошибка запроса"})
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	u := store.GetUserByUsername(r.Context(), username)
	if u == nil || !auth.CheckPassword(u.PasswordHash, password) {
		render.Page(w, r, "login", nil, map[string]string{"Error": "Неверное имя пользователя или пароль"})
		return
	}
	if u.BlockedAt != nil {
		msg := "Аккаунт заблокирован"
		if u.BlockReason != nil {
			msg = *u.BlockReason
		}
		render.Page(w, r, "login", nil, map[string]string{"Error": msg})
		return
	}

	if u.TotpEnabled {
		ttl := store.GetSettingInt(r.Context(), "pending_2fa_ttl_sec")
		if ttl <= 0 {
			ttl = 600
		}
		pendingToken, err := store.CreateTotpPendingToken(r.Context(), u.ID, ttl)
		if err != nil {
			render.Page(w, r, "login", nil, map[string]string{"Error": "Ошибка сервера"})
			return
		}
		http.Redirect(w, r, "/verify-2fa?t="+pendingToken, http.StatusFound)
		return
	}

	sess, err := auth.CreateSession(r.Context(), u.ID, r.RemoteAddr, r.Header.Get("User-Agent"))
	if err != nil {
		render.Page(w, r, "login", nil, map[string]string{"Error": "Ошибка сервера"})
		return
	}
	auth.SetSessionCookie(w, sess.Key, sess.ExpiresAt)
	http.Redirect(w, r, "/", http.StatusFound)
}

// GET /logout
func handleLogoutPage(w http.ResponseWriter, r *http.Request) {
	key := auth.SessionFromRequest(r)
	if key != "" {
		auth.DeleteSession(r.Context(), key) //nolint:errcheck
	}
	http.SetCookie(w, &http.Cookie{Name: "session_key", MaxAge: -1, Path: "/"})
	http.Redirect(w, r, "/login", http.StatusFound)
}

// ─── Register ─────────────────────────────────────────────────────────────────

// GET /register
func handleRegisterPage(w http.ResponseWriter, r *http.Request) {
	if u := userFromCtx(r); u != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	render.Page(w, r, "register", nil, nil)
}

// POST /register
func handleRegisterForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		render.Page(w, r, "register", nil, map[string]string{"Error": "Ошибка запроса"})
		return
	}
	// Honeypot check
	if r.FormValue("website") != "" {
		http.Redirect(w, r, "/register-success", http.StatusFound)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")
	confirm := r.FormValue("password_confirm")

	if password != confirm {
		render.Page(w, r, "register", nil, map[string]string{"Error": "Пароли не совпадают"})
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		render.Page(w, r, "register", nil, map[string]string{"Error": "Ошибка сервера"})
		return
	}
	u, err := store.CreateUser(r.Context(), username, hash, "simple")
	if err != nil {
		render.Page(w, r, "register", nil, map[string]string{"Error": err.Error()})
		return
	}

	sess, err := auth.CreateSession(r.Context(), u.ID, r.RemoteAddr, r.Header.Get("User-Agent"))
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	auth.SetSessionCookie(w, sess.Key, sess.ExpiresAt)
	http.Redirect(w, r, "/register-success", http.StatusFound)
}

// GET /register-success
func handleRegisterSuccess(w http.ResponseWriter, r *http.Request) {
	render.Page(w, r, "register_success", userFromCtx(r), nil)
}

// ─── Sessions ─────────────────────────────────────────────────────────────────

type sessionsData struct {
	Sessions []auth.SessionInfo
}

// GET /sessions
func handleSessionsPage(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	key := auth.SessionFromRequest(r)
	sessions := auth.ListSessions(r.Context(), u.ID, key)
	render.Page(w, r, "sessions", u, sessionsData{Sessions: sessions})
}

// POST /sessions/{id}/revoke
func handleSessionRevoke(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	revokedKey := auth.DeleteSessionByID(r.Context(), id, u.ID)
	currentKey := auth.SessionFromRequest(r)
	if revokedKey != "" && revokedKey == currentKey {
		auth.ClearSessionCookie(w)
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/sessions", http.StatusFound)
}

// POST /sessions/revoke-all
func handleSessionRevokeAll(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	auth.DeleteAllUserSessions(r.Context(), u.ID)
	auth.ClearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

// ─── Profile account actions (form-based) ────────────────────────────────────

// POST /profile/reset-password — change password via form
func handleFormChangePassword(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/profiles", http.StatusFound)
		return
	}
	current := r.FormValue("current_password")
	newPwd := r.FormValue("new_password")
	if !auth.CheckPassword(u.PasswordHash, current) {
		http.Redirect(w, r, "/profiles?error=wrong_password", http.StatusFound)
		return
	}
	if len(newPwd) < 6 {
		http.Redirect(w, r, "/profiles?error=password_short", http.StatusFound)
		return
	}
	hash, err := auth.HashPassword(newPwd)
	if err != nil {
		http.Redirect(w, r, "/profiles", http.StatusFound)
		return
	}
	if err := store.UpdatePassword(r.Context(), u.ID, hash); err != nil {
		http.Redirect(w, r, "/profiles", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/profiles?success=password_changed", http.StatusFound)
}

// POST /profile/delete — delete account via form
func handleFormDeleteAccount(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/profiles", http.StatusFound)
		return
	}
	if !auth.CheckPassword(u.PasswordHash, r.FormValue("password")) {
		http.Redirect(w, r, "/profiles?error=wrong_password", http.StatusFound)
		return
	}
	if u.IsAdmin {
		http.Redirect(w, r, "/profiles", http.StatusFound)
		return
	}
	if err := store.DeleteUser(r.Context(), u.ID); err != nil {
		http.Redirect(w, r, "/profiles", http.StatusFound)
		return
	}
	key := auth.SessionFromRequest(r)
	if key != "" {
		auth.DeleteSession(r.Context(), key)
	}
	auth.ClearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusFound)
}

// ─── Actor page ───────────────────────────────────────────────────────────────

type actorPageData struct {
	PersonID  int64
	ImageBase string
}

// GET /actor/{person_id}
func handleActorPage(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	personID, err := strconv.ParseInt(chi.URLParam(r, "person_id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid person id", http.StatusBadRequest)
		return
	}
	render.Page(w, r, "actor", u, actorPageData{PersonID: personID, ImageBase: imageBase()})
}

// ─── Forgot / Reset password ──────────────────────────────────────────────────

type forgotData struct {
	Step     bool
	BotName  string
	Error    string
	Success  string
	Username string
}

// GET /forgot-password
func handleForgotPasswordPage(w http.ResponseWriter, r *http.Request) {
	render.Page(w, r, "forgot_password", nil, forgotData{BotName: botName()})
}

// POST /forgot-password
func handleForgotPasswordForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		render.Page(w, r, "forgot_password", nil, forgotData{BotName: botName(), Error: "Ошибка запроса"})
		return
	}
	username := r.FormValue("username")
	u := store.GetUserByUsername(r.Context(), username)
	if u == nil {
		// Don't reveal if user exists — just show step 2 anyway
		render.Page(w, r, "forgot_password", nil, forgotData{Step: true, BotName: botName(), Username: username})
		return
	}
	// TODO: send Telegram code
	render.Page(w, r, "forgot_password", nil, forgotData{Step: true, BotName: botName(), Username: username})
}

// GET /reset-password?token=
func handleResetPasswordPage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	render.Page(w, r, "reset_password", nil, map[string]string{"Token": token})
}

// POST /reset-password
func handleResetPasswordForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		render.Page(w, r, "reset_password", nil, map[string]string{"Error": "Ошибка запроса"})
		return
	}
	// TODO: validate token/code and change password
	render.Page(w, r, "reset_password", nil, map[string]string{"Error": "Функция ещё не реализована"})
}

func botName() string {
	name := config.Get().TelegramBotName
	if name == "" {
		return "bot"
	}
	return name
}

func imageBase() string {
	if config.Get().ProxyURL != "" {
		return "/imgproxy"
	}
	return "https://image.tmdb.org"
}

// ─── Card detail ─────────────────────────────────────────────────────────────

type cardDetailData struct {
	CardID    string
	ImageBase string
}

// GET /card/{card_id}
func handleCardDetailPage(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	cardID := chi.URLParam(r, "card_id")
	render.Page(w, r, "card_detail", u, cardDetailData{CardID: cardID, ImageBase: imageBase()})
}

// ─── History ──────────────────────────────────────────────────────────────────

type historyData struct {
	Devices   []store.DeviceWithStats
	ImageBase string
}

// GET /history
func handleHistoryPage(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	devices := store.GetDevicesWithStats(r.Context(), u.ID)
	render.Page(w, r, "history", u, historyData{Devices: devices, ImageBase: imageBase()})
}

// ─── Catalog / Index ──────────────────────────────────────────────────────────

type catalogData struct {
	Devices   []store.DeviceWithStats
	ImageBase string
}

type indexData struct {
	SimpleDeviceLimit  int
	SimpleTCLimit      int
	PremiumDeviceLimit int
	PremiumTCLimit     int
	BotName            string
	InactiveWarnDays   int
	InactiveDeleteDays int
	PluginURL          string
}

// GET /
func handleIndexPage(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u != nil {
		// Logged-in user → catalog
		devices := store.GetDevicesWithStats(r.Context(), u.ID)
		render.Page(w, r, "catalog", u, catalogData{Devices: devices, ImageBase: imageBase()})
		return
	}
	// Guest → landing page
	simple := store.LimitsFor("simple")
	premium := store.LimitsFor("premium")
	cfg := config.Get()
	render.Page(w, r, "index", nil, indexData{
		SimpleDeviceLimit:  simple.MaxDevices,
		SimpleTCLimit:      simple.MaxTimecodes,
		PremiumDeviceLimit: premium.MaxDevices,
		PremiumTCLimit:     premium.MaxTimecodes,
		BotName:            cfg.TelegramBotName,
		PluginURL:          cfg.PluginURL,
		InactiveWarnDays:   store.GetSettingInt(r.Context(), "inactive_warn_days"),
		InactiveDeleteDays: store.GetSettingInt(r.Context(), "inactive_delete_days"),
	})
}

type catalogCategoryData struct {
	Category     string
	CategoryName string
	Devices      []store.DeviceWithStats
	ImageBase    string
}

// GET /catalog/{category}
func handleCatalogCategoryPage(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	category := chi.URLParam(r, "category")
	devices := store.GetDevicesWithStats(r.Context(), u.ID)
	render.Page(w, r, "catalog_category", u, catalogCategoryData{
		Category:     category,
		CategoryName: categoryDisplayName(category),
		Devices:      devices,
		ImageBase:    imageBase(),
	})
}

var categoryTitles = map[string]string{
	"movies_ru_new":  "Новые русские фильмы",
	"movies_new":     "Новые фильмы",
	"tv_shows":       "Сериалы",
	"tv_shows_ru":    "Русские сериалы",
	"movies_4k_new":  "В высоком качестве (новые)",
	"legends_id":     "Топ фильмы",
	"movies_4k":      "В высоком качестве",
	"movies":         "Фильмы",
	"movies_ru":      "Русские фильмы",
	"cartoon_movies": "Мультфильмы",
	"cartoon_series": "Мультсериалы",
	"anime":          "Аниме",
	"np_popular":     "Популярно в NP",
}

func categoryDisplayName(id string) string {
	if name, ok := categoryTitles[id]; ok {
		return name
	}
	if strings.HasPrefix(id, "movies_id_") {
		return "Фильмы " + strings.TrimPrefix(id, "movies_id_") + " года"
	}
	return id
}

// GET /api/categories — used by catalog.js
func handleAPICategories(w http.ResponseWriter, r *http.Request) {
	type cat struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	order := []string{
		"np_popular",
		"movies_ru_new", "movies_new", "tv_shows", "tv_shows_ru",
		"movies_4k_new", "legends_id", "movies_4k", "movies", "movies_ru",
		"cartoon_movies", "cartoon_series", "anime",
	}
	result := make([]cat, 0, len(order)+15)
	for _, id := range order {
		result = append(result, cat{ID: id, Name: categoryDisplayName(id)})
	}
	// Year categories: current year down to 1980
	currentYear := time.Now().Year()
	for y := currentYear; y >= 1980; y-- {
		id := fmt.Sprintf("movies_id_%d", y)
		result = append(result, cat{ID: id, Name: categoryDisplayName(id)})
	}
	JSON(w, http.StatusOK, result)
}

// GET /api/profile-ids?device_id= — used by history.js / profiles.js
func handleAPIProfileIDs(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	empty := map[string]any{"profiles": []any{}, "limit": 0}
	if u == nil {
		JSON(w, http.StatusOK, empty)
		return
	}
	deviceID, _ := strconv.ParseInt(r.URL.Query().Get("device_id"), 10, 64)
	if deviceID == 0 {
		JSON(w, http.StatusOK, empty)
		return
	}
	// Verify ownership
	if !userOwnsDevice(r, u.ID, deviceID) {
		JSON(w, http.StatusOK, empty)
		return
	}
	profiles := store.ListProfiles(r.Context(), deviceID)
	if profiles == nil {
		profiles = []store.ProfileInfo{}
	}
	lim := store.LimitsFor(u.Role).MaxProfiles
	JSON(w, http.StatusOK, map[string]any{"profiles": profiles, "limit": lim})
}

// ─── Consent / Privacy (static legal pages) ──────────────────────────────────

type staticPageData struct {
	SiteName     string
	ContactEmail string
}

// GET /consent
func handleConsentPage(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	render.Page(w, r, "consent", userFromCtx(r), staticPageData{
		SiteName:     cfg.SiteName,
		ContactEmail: cfg.ContactEmail,
	})
}

// GET /privacy
func handlePrivacyPage(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	render.Page(w, r, "privacy", userFromCtx(r), staticPageData{
		SiteName:     cfg.SiteName,
		ContactEmail: cfg.ContactEmail,
	})
}

// ─── Stats dashboard ──────────────────────────────────────────────────────────

const statsCookieName = "np_stats"

func statsAuthorized(r *http.Request) bool {
	c, err := r.Cookie(statsCookieName)
	return err == nil && c.Value == "1"
}

type statsLoginData struct {
	Error string
}

// GET /stats
func handleStatsLoginPage(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if statsAuthorized(r) || (u != nil && u.IsAdmin) {
		http.Redirect(w, r, "/stats/dashboard", http.StatusFound)
		return
	}
	render.Page(w, r, "stats_login", u, statsLoginData{})
}

// POST /stats
func handleStatsLoginForm(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if err := r.ParseForm(); err != nil {
		render.Page(w, r, "stats_login", u, statsLoginData{Error: "Ошибка запроса"})
		return
	}
	cfg := config.Get()
	if cfg.AdminPassword == "" || r.FormValue("password") != cfg.AdminPassword {
		render.Page(w, r, "stats_login", u, statsLoginData{Error: "Неверный пароль"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     statsCookieName,
		Value:    "1",
		Path:     "/",
		MaxAge:   86400 * 7,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/stats/dashboard", http.StatusFound)
}

type simpleUserRow struct {
	Username  string
	Role      string
	CreatedAt string
}

type statsDashData struct {
	Now            string
	UsersToday     int
	UsersTotal     int
	DevicesTotal   int
	CardsTotal     int
	TimecodesTotal int
	NewUsersToday  []simpleUserRow
	AllUsers       []simpleUserRow
}

// GET /stats/dashboard
func handleStatsDashboard(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if !statsAuthorized(r) && (u == nil || !u.IsAdmin) {
		http.Redirect(w, r, "/stats", http.StatusFound)
		return
	}
	ctx := r.Context()
	var d statsDashData
	d.Now = time.Now().Format("15:04:05")

	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE created_at::date = CURRENT_DATE`).Scan(&d.UsersToday)   //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&d.UsersTotal)                                          //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices`).Scan(&d.DevicesTotal)                                      //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM media_cards`).Scan(&d.CardsTotal)                                    //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM timecodes`).Scan(&d.TimecodesTotal)                                  //nolint:errcheck

	if rows, err := postgres.Pool.Query(ctx,
		`SELECT username, created_at FROM users WHERE created_at::date = CURRENT_DATE ORDER BY created_at DESC`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var username string
			var createdAt time.Time
			if rows.Scan(&username, &createdAt) == nil {
				d.NewUsersToday = append(d.NewUsersToday, simpleUserRow{Username: username, CreatedAt: createdAt.Format("15:04:05")})
			}
		}
	}

	if rows, err := postgres.Pool.Query(ctx,
		`SELECT username, role, created_at FROM users ORDER BY created_at DESC LIMIT 200`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var row simpleUserRow
			var createdAt time.Time
			if rows.Scan(&row.Username, &row.Role, &createdAt) == nil {
				row.CreatedAt = createdAt.Format("02.01.2006")
				d.AllUsers = append(d.AllUsers, row)
			}
		}
	}

	render.Page(w, r, "stats_dashboard", u, d)
}

// ─── Telegram miniapp ──────────────────────────────────────────────────────────

type tgMiniappData struct {
	User       interface{}
	DeviceCode string
	Success    bool
	Error      string
}

// GET /tg-miniapp
func handleTgMiniappPage(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	code := r.URL.Query().Get("code")
	render.Page(w, r, "tg_miniapp", u, tgMiniappData{User: u, DeviceCode: code})
}

// POST /tg-miniapp
func handleTgMiniappSubmit(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		render.Page(w, r, "tg_miniapp", u, tgMiniappData{User: u, Error: "Ошибка запроса"})
		return
	}
	code := r.FormValue("code")
	if code == "" {
		render.Page(w, r, "tg_miniapp", u, tgMiniappData{User: u, Error: "Код не указан"})
		return
	}
	lim := store.LimitsFor(u.Role).MaxDevices
	if _, err := store.LinkDeviceCode(r.Context(), code, u.ID, "Lampa (Telegram)", lim); err != nil {
		render.Page(w, r, "tg_miniapp", u, tgMiniappData{User: u, DeviceCode: code, Error: "Неверный или устаревший код"})
		return
	}
	render.Page(w, r, "tg_miniapp", u, tgMiniappData{User: u, Success: true})
}
