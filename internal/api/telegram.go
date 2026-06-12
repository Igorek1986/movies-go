package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"movies-api/db/store"
	"movies-api/internal/bot"
)

// POST /bot/webhook — receives Telegram updates.
//
// Telegram echoes the configured secret_token in the
// X-Telegram-Bot-Api-Secret-Token header on every update. Without this check
// anyone who knows the URL could POST forged updates and, since bot-admin
// rights are derived from the update's chat id, execute admin commands. We
// reject any request whose header does not match the stored secret (and reject
// everything if no secret is configured, e.g. polling mode).
func handleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	secret, _ := store.GetSetting(r.Context(), "telegram_webhook_secret")
	provided := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
	if secret == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) != 1 {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	var update tgbotapi.Update
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	bot.HandleWebhookUpdate(update)
	w.WriteHeader(http.StatusOK)
}

// POST /api/telegram/generate-link-code — generates a Telegram link code for the current user
func handleGenerateLinkCode(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !bot.Enabled() {
		Error(w, http.StatusServiceUnavailable, "telegram bot not configured")
		return
	}
	ttl := store.GetSettingInt(r.Context(), "telegram_link_ttl_minutes")
	code, err := store.CreateTelegramLinkCode(r.Context(), u.ID, ttl)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to create link code")
		return
	}
	botName, _ := store.GetSetting(r.Context(), "telegram_bot_name")
	JSON(w, http.StatusOK, map[string]any{
		"code":     code,
		"bot_name": botName,
		"link":     "https://t.me/" + botName + "?start=" + code,
		"ttl_min":  ttl,
	})
}

// GET /api/telegram/status — returns whether the current user has Telegram linked
func handleTelegramStatus(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	link := store.GetTelegramLinkByUserID(r.Context(), u.ID)
	if link == nil {
		JSON(w, http.StatusOK, map[string]any{"linked": false})
		return
	}
	JSON(w, http.StatusOK, map[string]any{
		"linked":      true,
		"username":    link.Username,
		"telegram_id": link.TelegramID,
	})
}

// DELETE /api/telegram/unlink — removes Telegram link for the current user
func handleTelegramUnlink(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	store.DeleteTelegramLink(r.Context(), u.ID)
	JSON(w, http.StatusOK, map[string]any{"ok": true})
}
