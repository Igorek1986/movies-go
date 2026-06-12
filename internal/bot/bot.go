package bot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"movies-api/db/store"
	"movies-api/internal/proxy"
)

var (
	instance      *tgbotapi.BotAPI
	adminIDs      []int64
	pollingCancel context.CancelFunc
)

// Start initializes the Telegram bot. Does nothing if token is not configured.
func Start(ctx context.Context) error {
	token, _ := store.GetSetting(ctx, "telegram_bot_token")
	if token == "" {
		log.Println("bot: token not configured, skipping")
		return nil
	}

	httpClient := proxy.Default.ClientFor(ctx, proxy.RouteTelegram)
	bot, err := tgbotapi.NewBotAPIWithClient(token, tgbotapi.APIEndpoint, httpClient)
	if err != nil {
		log.Printf("bot: init error: %v", err)
		return fmt.Errorf("telegram bot init: %w", err)
	}
	instance = bot
	adminIDsStr, _ := store.GetSetting(ctx, "telegram_admin_ids")
	adminIDs = parseAdminIDs(adminIDsStr)

	usePolling, _ := store.GetSetting(ctx, "telegram_use_polling")
	mode := "webhook"
	if usePolling == "1" {
		mode = "polling"
	}
	log.Printf("bot: started @%s, mode=%s, admins=%v", bot.Self.UserName, mode, adminIDs)

	if usePolling == "1" {
		if _, err := bot.Request(tgbotapi.DeleteWebhookConfig{DropPendingUpdates: false}); err != nil {
			log.Printf("bot: deleteWebhook error: %v", err)
		} else {
			log.Println("bot: webhook deleted, starting polling")
		}
		pollCtx, cancel := context.WithCancel(ctx)
		pollingCancel = cancel
		go runPolling(pollCtx)
	}
	return nil
}

// Stop shuts down the bot instance and cancels polling if active.
func Stop() {
	if instance == nil {
		return
	}
	log.Println("bot: stopping")
	if pollingCancel != nil {
		pollingCancel()
		pollingCancel = nil
	}
	instance.StopReceivingUpdates()
	instance = nil
	adminIDs = nil
}

// Restart stops the current bot instance and starts a fresh one from settings.
func Restart(ctx context.Context) error {
	log.Println("bot: restarting")
	Stop()
	return Start(ctx)
}

// EnsureWebhookSecret returns the stored Telegram webhook secret token,
// generating and persisting a fresh one if none is set. The secret is sent to
// Telegram via setWebhook(secret_token=…) and echoed back by Telegram in the
// X-Telegram-Bot-Api-Secret-Token header on every update, so the webhook
// handler can reject forged requests.
func EnsureWebhookSecret(ctx context.Context) string {
	if s, ok := store.GetSetting(ctx, "telegram_webhook_secret"); ok && s != "" {
		return s
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	secret := hex.EncodeToString(b) // 32 hex chars, allowed alphabet for secret_token
	store.SetSetting(ctx, "telegram_webhook_secret", secret)
	return secret
}

// SetWebhook registers the bot webhook at the given URL. When secret is
// non-empty it is passed as secret_token so Telegram signs every update.
func SetWebhook(webhookURL, secret string) error {
	if instance == nil {
		return nil
	}
	params := tgbotapi.Params{"url": webhookURL}
	if secret != "" {
		params["secret_token"] = secret
	}
	_, err := instance.MakeRequest("setWebhook", params)
	return err
}

// SetMenuButton sets the bot's default menu button to a Mini App web_app button.
func SetMenuButton(appURL string) error {
	if instance == nil {
		return nil
	}
	menuButton := fmt.Sprintf(`{"type":"web_app","text":"📱 Управление","web_app":{"url":%q}}`, appURL)
	v := map[string]string{"menu_button": menuButton}
	_, err := instance.MakeRequest("setChatMenuButton", v)
	return err
}

// HandleWebhookUpdate processes a single update received via webhook.
func HandleWebhookUpdate(update tgbotapi.Update) {
	if instance == nil {
		return
	}
	handleUpdate(update)
}

// SendMessage sends a plain HTML message to a Telegram user.
func SendMessage(telegramID int64, text string) bool {
	if instance == nil {
		return false
	}
	msg := tgbotapi.NewMessage(telegramID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	_, err := instance.Send(msg)
	if err != nil {
		log.Printf("bot: send to %d failed: %v", telegramID, err)
	}
	return err == nil
}

// SendResetCode sends a 6-digit password reset code to the user via Telegram.
func SendResetCode(telegramID int64, username, code string) bool {
	text := fmt.Sprintf(
		"🔑 Код для сброса пароля аккаунта <b>%s</b>:\n\n<code>%s</code>\n\nКод действителен 15 минут.",
		username, code,
	)
	return SendMessage(telegramID, text)
}

// SendNewSessionNotification notifies the user about a new login from an unknown device.
func SendNewSessionNotification(telegramID int64, ip, userAgent string) bool {
	text := fmt.Sprintf(
		"⚠️ Новый вход в аккаунт\n\nIP: <code>%s</code>\nБраузер: %s\nВремя: %s\n\nЕсли это не вы — смените пароль.",
		ip, userAgent, time.Now().Format("02.01.2006 15:04"),
	)
	return SendMessage(telegramID, text)
}

// IsAdmin reports whether telegramID belongs to a configured admin.
func IsAdmin(telegramID int64) bool {
	for _, id := range adminIDs {
		if id == telegramID {
			return true
		}
	}
	return false
}

// AdminIDs returns a copy of the configured admin IDs.
func AdminIDs() []int64 {
	cp := make([]int64, len(adminIDs))
	copy(cp, adminIDs)
	return cp
}

// Enabled reports whether the bot is configured and running.
func Enabled() bool { return instance != nil }

// Username returns the bot's Telegram username, or empty string if not running.
func Username() string {
	if instance == nil {
		return ""
	}
	return instance.Self.UserName
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

func runPolling(ctx context.Context) {
	log.Println("bot: polling started")
	local := instance // capture before loop; Stop() may nil instance concurrently
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := local.GetUpdatesChan(u)
	for {
		select {
		case <-ctx.Done():
			log.Println("bot: polling stopped (context cancelled)")
			return
		case update, ok := <-updates:
			if !ok {
				log.Println("bot: polling stopped (updates channel closed)")
				return
			}
			handleUpdate(update)
		}
	}
}

func send(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	instance.Send(msg) //nolint:errcheck
}

func sendWithKeyboard(chatID int64, text string, kb tgbotapi.ReplyKeyboardMarkup) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = tgbotapi.ModeHTML
	msg.ReplyMarkup = kb
	instance.Send(msg) //nolint:errcheck
}


func mainKeyboard(isAdmin bool) tgbotapi.ReplyKeyboardMarkup {
	rows := [][]tgbotapi.KeyboardButton{
		{tgbotapi.NewKeyboardButton("📊 Статус")},
		{tgbotapi.NewKeyboardButton("🚨 Не работает")},
	}
	donateURL, _ := store.GetSetting(context.Background(), "donate_url")
	if donateURL != "" {
		rows = append([][]tgbotapi.KeyboardButton{
			{tgbotapi.NewKeyboardButton("💰 Донат")},
		}, rows...)
	}
	if isAdmin {
		rows = append(rows,
			[]tgbotapi.KeyboardButton{
				tgbotapi.NewKeyboardButton("👥 Пользователи"),
				tgbotapi.NewKeyboardButton("📢 Рассылка"),
			},
		)
	}
	return tgbotapi.NewReplyKeyboard(rows...)
}

func parseAdminIDs(s string) []int64 {
	s = strings.Trim(s, "[] ")
	var ids []int64
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if n, err := strconv.ParseInt(p, 10, 64); err == nil {
			ids = append(ids, n)
		}
	}
	return ids
}
