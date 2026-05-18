package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"lampa-api/config"
)

var (
	instance *tgbotapi.BotAPI
	adminIDs []int64
)

// Start initializes the Telegram bot. Does nothing if token is not configured.
func Start(ctx context.Context) error {
	cfg := config.Get()
	if cfg.TelegramBotToken == "" {
		return nil
	}

	bot, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		return fmt.Errorf("telegram bot init: %w", err)
	}
	instance = bot
	adminIDs = parseAdminIDs(cfg.TelegramAdminIDs)

	log.Printf("Telegram bot: @%s (admins: %v)", bot.Self.UserName, adminIDs)

	if cfg.TelegramUsePolling {
		go runPolling(ctx)
	}
	return nil
}

// SetWebhook registers the bot webhook at the given URL.
func SetWebhook(webhookURL string) error {
	if instance == nil {
		return nil
	}
	wh, err := tgbotapi.NewWebhook(webhookURL)
	if err != nil {
		return err
	}
	_, err = instance.Request(wh)
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

// ─── Internal helpers ─────────────────────────────────────────────────────────

func runPolling(ctx context.Context) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := instance.GetUpdatesChan(u)
	for {
		select {
		case <-ctx.Done():
			instance.StopReceivingUpdates()
			return
		case update, ok := <-updates:
			if !ok {
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
	cfg := config.Get()
	rows := [][]tgbotapi.KeyboardButton{
		{tgbotapi.NewKeyboardButton("📊 Статус")},
		{tgbotapi.NewKeyboardButton("🚨 Не работает")},
	}
	if cfg.DonateURL != "" {
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
