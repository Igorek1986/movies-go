package api

import (
	"context"
	"encoding/json"
	"fmt"
	"movies-api/db/store"
	"movies-api/internal/auth"
	botpkg "movies-api/internal/bot"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// GET /logout
func handleLogoutPage(w http.ResponseWriter, r *http.Request) {
	key := auth.SessionFromRequest(r)
	if key != "" {
		auth.DeleteSession(r.Context(), key) //nolint:errcheck
	}
	http.SetCookie(w, &http.Cookie{Name: "session_key", MaxAge: -1, Path: "/"})
	http.Redirect(w, r, "/login", http.StatusFound)
}

// ─── Sessions (JSON API) ──────────────────────────────────────────────────────

// GET /api/sessions
func handleAPISessions(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	key := auth.SessionFromRequest(r)
	sessions := auth.ListSessions(r.Context(), u.ID, key)
	type sessionOut struct {
		ID        int64  `json:"id"`
		Browser   string `json:"browser"`
		IP        string `json:"ip"`
		CreatedAt string `json:"created_at"`
		IsCurrent bool   `json:"is_current"`
	}
	out := make([]sessionOut, len(sessions))
	for i, s := range sessions {
		out[i] = sessionOut{
			ID:        s.ID,
			Browser:   s.Browser,
			IP:        s.IP,
			CreatedAt: s.CreatedAt.Format("02.01.2006"),
			IsCurrent: s.IsCurrent,
		}
	}
	JSON(w, http.StatusOK, map[string]any{"sessions": out})
}

// DELETE /api/sessions/{id}
func handleAPISessionRevoke(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	revokedKey := auth.DeleteSessionByID(r.Context(), id, u.ID)
	currentKey := auth.SessionFromRequest(r)
	if revokedKey != "" && revokedKey == currentKey {
		auth.ClearSessionCookie(w)
		JSON(w, http.StatusOK, map[string]any{"ok": true, "logged_out": true})
		return
	}
	JSON(w, http.StatusOK, map[string]any{"ok": true, "logged_out": false})
}

// DELETE /api/sessions
func handleAPISessionRevokeAll(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	auth.DeleteAllUserSessions(r.Context(), u.ID)
	auth.ClearSessionCookie(w)
	JSON(w, http.StatusOK, map[string]any{"ok": true, "logged_out": true})
}

// ─── Forgot / Reset password — JSON API ──────────────────────────────────────

// POST /api/forgot-password  body: {"username":"..."}
func handleAPIForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		Error(w, http.StatusBadRequest, "username required")
		return
	}

	u := store.GetUserByUsername(r.Context(), req.Username)
	if u == nil {
		JSON(w, http.StatusOK, map[string]any{"ok": true, "bot_name": botName()})
		return
	}
	tgLink := store.GetTelegramLinkByUserID(r.Context(), u.ID)
	if tgLink == nil {
		Error(w, http.StatusUnprocessableEntity, "no_telegram")
		return
	}
	ttl := store.GetSettingInt(r.Context(), "reset_code_ttl_minutes")
	code, err := store.CreatePasswordResetToken(r.Context(), u.ID, ttl)
	if err != nil {
		Error(w, http.StatusInternalServerError, "server error")
		return
	}
	botpkg.SendResetCode(tgLink.TelegramID, req.Username, code)
	JSON(w, http.StatusOK, map[string]any{"ok": true, "bot_name": botName()})
}

// POST /api/reset-password  body: {"token":"...","new_password":"..."}
func handleAPIResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Token == "" || len(req.NewPassword) < 6 {
		Error(w, http.StatusBadRequest, "token and new_password (min 6 chars) required")
		return
	}
	userID, err := store.ConsumePasswordResetToken(r.Context(), req.Token)
	if err != nil || userID == 0 {
		Error(w, http.StatusUnauthorized, "invalid or expired code")
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		Error(w, http.StatusInternalServerError, "server error")
		return
	}
	if err := store.UpdatePassword(r.Context(), userID, hash); err != nil {
		Error(w, http.StatusInternalServerError, "server error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func botName() string {
	if name, ok := store.GetSetting(context.Background(), "telegram_bot_name"); ok && name != "" {
		return name
	}
	return "bot"
}

// ─── Categories / Profile IDs ─────────────────────────────────────────────────

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
	"np_popular": "Популярное",
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

// GET /api/categories
func handleAPICategories(w http.ResponseWriter, r *http.Request) {
	type cat struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	order := []string{
		"movies_ru_new", "movies_new", "tv_shows", "tv_shows_ru",
		"movies_4k_new", "legends_id", "movies_4k", "movies", "movies_ru",
		"cartoon_movies", "cartoon_series", "anime",
	}
	result := make([]cat, 0, len(order)+16)
	if getPopularSourceURL(r.Context()) != "" || store.HasPopularData(r.Context(), 30) {
		result = append(result, cat{ID: "np_popular", Name: "Популярное"})
	}
	for _, id := range order {
		result = append(result, cat{ID: id, Name: categoryDisplayName(id)})
	}
	currentYear := time.Now().Year()
	for y := currentYear; y >= 1980; y-- {
		id := fmt.Sprintf("movies_id_%d", y)
		result = append(result, cat{ID: id, Name: categoryDisplayName(id)})
	}
	JSON(w, http.StatusOK, result)
}

// GET /api/profile-ids?device_id=
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

// GET /api/public/page?name=consent|privacy
func handlePublicPage(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	siteName, _ := store.GetSetting(r.Context(), "site_name")
	if siteName == "" {
		siteName = store.SettingDefaults["site_name"]
	}
	contactEmail, _ := store.GetSetting(r.Context(), "contact_email")
	baseURL, _ := store.GetSetting(r.Context(), "base_url")

	var title, settingKey, defaultHTML string
	switch name {
	case "consent":
		title = "Согласие на обработку персональных данных"
		settingKey = "consent_content"
		defaultHTML = consentDefaultHTML(siteName, baseURL, contactEmail)
	case "privacy":
		title = "Политика обработки персональных данных"
		settingKey = "privacy_policy_content"
		defaultHTML = privacyDefaultHTML(siteName, baseURL, contactEmail)
	default:
		Error(w, http.StatusNotFound, "unknown page")
		return
	}

	html := defaultHTML
	if custom, ok := store.GetSetting(r.Context(), settingKey); ok && custom != "" {
		html = custom
	}

	JSON(w, http.StatusOK, map[string]string{"title": title, "html": html})
}

func consentDefaultHTML(site, siteURL, email string) string {
	contact := ""
	if email != "" {
		contact = `<p><strong>Контактные данные оператора:</strong> <a href="mailto:` + email + `">` + email + `</a></p>`
	}
	siteRef := "<strong>" + site + "</strong>"
	if siteURL != "" {
		siteRef = `<a href="` + siteURL + `"><strong>` + site + `</strong></a>`
	}
	privacyLink := "/privacy"
	if siteURL != "" {
		privacyLink = siteURL + "/privacy"
	}
	return `<h2>Согласие на обработку персональных данных</h2>
<p>Настоящим я, пользователь сервиса ` + siteRef + `, свободно, своей волей и в своём интересе даю согласие на обработку персональных данных в соответствии с <a href="` + privacyLink + `">Политикой обработки персональных данных</a>.</p>
<p>Перечень обрабатываемых данных:</p>
<ul><li>Telegram ID и username в мессенджере Telegram</li></ul>
<p><strong>Цели обработки:</strong></p>
<ul>
  <li>Восстановление доступа к аккаунту через Telegram</li>
  <li>Отправка уведомлений о входе в аккаунт и истечении подписки</li>
</ul>
<p>Согласие предоставляется путём установки отметки (чекбокса) на странице привязки Telegram-аккаунта и действует до его отзыва.</p>
<p>Я вправе в любой момент отозвать согласие, отвязав Telegram-аккаунт в разделе «Настройки аккаунта». После отзыва согласия данные будут удалены.</p>
` + contact
}

func privacyDefaultHTML(site, siteURL, email string) string {
	contact := ""
	if email != "" {
		contact = `<h4>7. Контактные данные</h4><p>По вопросам обработки персональных данных: <a href="mailto:` + email + `">` + email + `</a></p>`
	}
	siteRef := "<strong>" + site + "</strong>"
	if siteURL != "" {
		siteRef = `<a href="` + siteURL + `"><strong>` + site + `</strong></a>`
	}
	return `<h2>Политика обработки персональных данных</h2>
<p>Настоящая Политика определяет порядок обработки персональных данных пользователей сервиса ` + siteRef + `.</p>
<h4>1. Какие данные мы обрабатываем</h4>
<ul>
  <li>Имя пользователя (логин)</li>
  <li>Telegram ID и username — при добровольной привязке аккаунта Telegram</li>
  <li>IP-адрес и User-Agent браузера — для защиты аккаунта и ведения сессий</li>
</ul>
<h4>2. Цели обработки</h4>
<ul>
  <li>Аутентификация и управление сессиями</li>
  <li>Восстановление пароля через Telegram</li>
  <li>Отправка уведомлений о входе в аккаунт и истечении подписки</li>
</ul>
<h4>3. Хранение данных</h4>
<p>Данные хранятся на сервере оператора. Telegram ID и username хранятся только при наличии явно данного согласия и удаляются при отвязке Telegram-аккаунта.</p>
<h4>4. Передача третьим лицам</h4>
<p>Данные не передаются третьим лицам, за исключением технически необходимого взаимодействия с платформой Telegram (при привязке аккаунта).</p>
<h4>5. Права субъекта данных</h4>
<p>Вы вправе в любой момент отвязать Telegram-аккаунт и удалить свою учётную запись в разделе «Настройки аккаунта».</p>
<h4 id="cookie-policy">6. Использование cookie-файлов</h4>
<p>Сайт использует cookie-файлы для обеспечения работы сессий авторизации и хранения пользовательских предпочтений. Cookie не передаются третьим лицам.</p>
` + contact
}

// ─── Notification settings ────────────────────────────────────────────────────

// GET /api/notification-settings
func handleGetNotificationSettings(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	s := store.GetNotificationSettings(r.Context(), u.ID)
	JSON(w, http.StatusOK, s)
}

// PATCH /api/notification-settings
func handlePatchNotificationSettings(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body store.NotificationSettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.NotifyStart < 0 || body.NotifyStart > 23 || body.NotifyEnd < 0 || body.NotifyEnd > 23 {
		Error(w, http.StatusBadRequest, "notify_start and notify_end must be 0-23")
		return
	}
	if err := store.SaveNotificationSettings(r.Context(), u.ID, body); err != nil {
		Error(w, http.StatusInternalServerError, "failed to save")
		return
	}
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}
