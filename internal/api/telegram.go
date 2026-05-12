package api

import (
	"encoding/json"
	"net/http"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"lampa-api/config"
	"lampa-api/db/store"
	"lampa-api/internal/bot"
)

// POST /bot/webhook — receives Telegram updates
func handleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
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
	botName := config.Get().TelegramBotName
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
