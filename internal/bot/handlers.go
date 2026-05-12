package bot

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"lampa-api/config"
	"lampa-api/db/store"
)

// ─── FSM state for broadcast ──────────────────────────────────────────────────

var (
	broadcastWaiting   = map[int64]bool{}
	broadcastWaitingMu sync.Mutex
)

// ─── Report cooldown (30 min, in-memory) ─────────────────────────────────────

var (
	reportCooldown   = map[int64]time.Time{}
	reportCooldownMu sync.Mutex
)

func reportAllowed(telegramID int64) bool {
	reportCooldownMu.Lock()
	defer reportCooldownMu.Unlock()
	t, ok := reportCooldown[telegramID]
	if !ok || time.Since(t) > 30*time.Minute {
		reportCooldown[telegramID] = time.Now()
		return true
	}
	return false
}

// ─── Main update dispatcher ───────────────────────────────────────────────────

func handleUpdate(update tgbotapi.Update) {
	if update.Message == nil {
		return
	}
	ctx := context.Background()
	msg := update.Message
	chatID := msg.Chat.ID
	text := strings.TrimSpace(msg.Text)

	// ── Admin broadcast FSM ───────────────────────────────────────────────────
	broadcastWaitingMu.Lock()
	waiting := broadcastWaiting[chatID]
	if waiting {
		delete(broadcastWaiting, chatID)
	}
	broadcastWaitingMu.Unlock()

	if waiting && IsAdmin(chatID) {
		doBroadcast(ctx, chatID, text)
		return
	}

	// ── Admin reply to forwarded support message ──────────────────────────────
	if msg.ReplyToMessage != nil && IsAdmin(chatID) {
		handleAdminReply(ctx, msg)
		return
	}

	// ── Commands ──────────────────────────────────────────────────────────────
	if msg.IsCommand() {
		handleCommand(ctx, msg)
		return
	}

	// ── Keyboard buttons ──────────────────────────────────────────────────────
	switch text {
	case "📊 Статус":
		sendStatus(ctx, chatID)
	case "💰 Донат":
		cfg := config.Get()
		if cfg.DonateURL != "" {
			send(chatID, fmt.Sprintf("💰 <a href=\"%s\">Поддержать проект</a>", cfg.DonateURL))
		}
	case "🚨 Не работает":
		if !reportAllowed(chatID) {
			send(chatID, "⏳ Повторный репорт возможен через 30 минут.")
			return
		}
		username := ""
		if msg.From != nil {
			username = msg.From.UserName
		}
		for _, adminID := range AdminIDs() {
			send(adminID, fmt.Sprintf("🚨 Репорт от @%s (tg_id: %d): сервис не работает", username, chatID))
		}
		send(chatID, "✅ Репорт отправлен администраторам. Спасибо!")
	case "👥 Пользователи":
		if IsAdmin(chatID) {
			sendUserStats(ctx, chatID)
		}
	case "📢 Рассылка":
		if IsAdmin(chatID) {
			broadcastWaitingMu.Lock()
			broadcastWaiting[chatID] = true
			broadcastWaitingMu.Unlock()
			send(chatID, "Введите текст рассылки (HTML поддерживается):")
		}
	default:
		// Support chat: forward to admins
		forwardToSupport(ctx, msg)
	}
}

// ─── Command handlers ─────────────────────────────────────────────────────────

func handleCommand(ctx context.Context, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	switch msg.Command() {
	case "start":
		handleStart(ctx, chatID, msg.CommandArguments(), msg.From)
	case "status":
		sendStatus(ctx, chatID)
	case "admin":
		if IsAdmin(chatID) {
			send(chatID, adminHelp())
		}
	case "info":
		if IsAdmin(chatID) {
			sendUserInfo(ctx, chatID, strings.TrimSpace(msg.CommandArguments()))
		}
	case "setpremium":
		if IsAdmin(chatID) {
			setRole(ctx, chatID, strings.TrimSpace(msg.CommandArguments()), "premium")
		}
	case "setsuper":
		if IsAdmin(chatID) {
			setRole(ctx, chatID, strings.TrimSpace(msg.CommandArguments()), "super")
		}
	case "setsimple":
		if IsAdmin(chatID) {
			setRole(ctx, chatID, strings.TrimSpace(msg.CommandArguments()), "simple")
		}
	case "broadcast":
		if IsAdmin(chatID) {
			arg := strings.TrimSpace(msg.CommandArguments())
			if arg != "" {
				doBroadcast(ctx, chatID, arg)
			} else {
				broadcastWaitingMu.Lock()
				broadcastWaiting[chatID] = true
				broadcastWaitingMu.Unlock()
				send(chatID, "Введите текст рассылки:")
			}
		}
	default:
		// Unknown command — treat as regular message for support
		forwardToSupport(ctx, msg)
	}
}

func handleStart(ctx context.Context, chatID int64, arg string, from *tgbotapi.User) {
	username := ""
	if from != nil {
		username = from.UserName
	}

	if arg != "" {
		// Link Telegram account via code
		userID, err := store.ConsumeTelegramLinkCode(ctx, arg)
		if err != nil || userID == 0 {
			send(chatID, "❌ Неверный или устаревший код привязки.")
			return
		}
		if err := store.UpsertTelegramLink(ctx, userID, chatID, username); err != nil {
			send(chatID, "❌ Ошибка привязки аккаунта.")
			return
		}
		sendWithKeyboard(chatID, "✅ Telegram успешно привязан к вашему аккаунту!", mainKeyboard(IsAdmin(chatID)))
		return
	}

	sendWithKeyboard(chatID, welcomeText(), mainKeyboard(IsAdmin(chatID)))
}

func sendStatus(ctx context.Context, chatID int64) {
	link := store.GetTelegramLinkByTelegramID(ctx, chatID)
	if link == nil {
		send(chatID, "Ваш Telegram не привязан к аккаунту.\nПривяжите его в личном кабинете.")
		return
	}
	u := store.GetUserByID(ctx, link.UserID)
	if u == nil {
		send(chatID, "Аккаунт не найден.")
		return
	}

	lim := store.LimitsFor(u.Role)
	role := roleLabel(u.Role)

	var premium string
	if u.PremiumUntil != nil {
		premium = fmt.Sprintf("\n📅 Подписка до: <b>%s</b>", u.PremiumUntil.Format("02.01.2006"))
	}

	devices := store.GetDevicesWithStats(ctx, u.ID)

	text := fmt.Sprintf(
		"👤 <b>%s</b>\n🎭 Роль: <b>%s</b>%s\n📱 Устройств: %d / %d",
		u.Username, role, premium,
		len(devices), lim.MaxDevices,
	)
	send(chatID, text)
}

// ─── Admin helpers ────────────────────────────────────────────────────────────

func sendUserInfo(ctx context.Context, chatID int64, username string) {
	if username == "" {
		send(chatID, "Укажите имя пользователя: /info username")
		return
	}
	u := store.GetUserByUsername(ctx, username)
	if u == nil {
		send(chatID, "Пользователь не найден.")
		return
	}
	lim := store.LimitsFor(u.Role)
	devices := store.GetDevicesWithStats(ctx, u.ID)
	tgLink := store.GetTelegramLinkByUserID(ctx, u.ID)
	tgInfo := "не привязан"
	if tgLink != nil {
		tgInfo = fmt.Sprintf("@%s (id: %d)", tgLink.Username, tgLink.TelegramID)
	}
	var premium string
	if u.PremiumUntil != nil {
		premium = fmt.Sprintf("\n📅 Подписка до: <b>%s</b>", u.PremiumUntil.Format("02.01.2006"))
	}
	send(chatID, fmt.Sprintf(
		"👤 <b>%s</b>\nID: %d\nРоль: <b>%s</b>%s\n📱 Устройств: %d / %d\nTelegram: %s\nРег: %s",
		u.Username, u.ID, roleLabel(u.Role), premium,
		len(devices), lim.MaxDevices,
		tgInfo, u.CreatedAt.Format("02.01.2006"),
	))
}

func setRole(ctx context.Context, chatID int64, username, role string) {
	if username == "" {
		send(chatID, fmt.Sprintf("Укажите имя: /set%s username", role))
		return
	}
	u := store.GetUserByUsername(ctx, username)
	if u == nil {
		send(chatID, "Пользователь не найден.")
		return
	}
	if err := store.SetUserRole(ctx, u.ID, role); err != nil {
		send(chatID, "Ошибка: "+err.Error())
		return
	}
	store.InvalidateLimitsCache()
	send(chatID, fmt.Sprintf("✅ Роль %s установлена для @%s", roleLabel(role), username))
	// Notify the user if they have Telegram linked
	if tg := store.GetTelegramLinkByUserID(ctx, u.ID); tg != nil {
		SendMessage(tg.TelegramID, fmt.Sprintf("🎉 Ваша роль изменена на <b>%s</b>.", roleLabel(role)))
	}
}

func sendUserStats(ctx context.Context, chatID int64) {
	var total, premium, simple, superRole int
	conn := context.Background()
	_ = conn
	// Use direct queries for stats
	type roleCount struct {
		Role  string
		Count int
	}
	rows, err := store.QueryUserRoleCounts(ctx)
	if err != nil {
		send(chatID, "Ошибка получения статистики.")
		return
	}
	for _, rc := range rows {
		total += rc.Count
		switch rc.Role {
		case "premium":
			premium = rc.Count
		case "simple":
			simple = rc.Count
		case "super":
			superRole = rc.Count
		}
	}
	send(chatID, fmt.Sprintf(
		"👥 Пользователи: <b>%d</b>\n🌟 Premium: %d\n👤 Simple: %d\n⚡ Super: %d",
		total, premium, simple, superRole,
	))
}

func doBroadcast(ctx context.Context, adminChatID int64, text string) {
	ids := store.GetAllTelegramIDs(ctx)
	sent, failed := 0, 0
	for _, id := range ids {
		if SendMessage(id, text) {
			sent++
		} else {
			failed++
		}
	}
	send(adminChatID, fmt.Sprintf("📢 Рассылка завершена: отправлено %d, ошибок %d", sent, failed))
}

// ─── Support chat ─────────────────────────────────────────────────────────────

func forwardToSupport(ctx context.Context, msg *tgbotapi.Message) {
	if msg == nil || msg.Text == "" {
		return
	}
	chatID := msg.Chat.ID
	username := ""
	if msg.From != nil {
		username = msg.From.UserName
	}

	adminList := AdminIDs()
	if len(adminList) == 0 {
		send(chatID, "✅ Сообщение получено.")
		return
	}

	header := fmt.Sprintf("💬 Сообщение от @%s (tg_id: %d):\n\n%s", username, chatID, msg.Text)

	var firstAdminMsgID *int
	var firstAdminID *int64
	for _, adminID := range adminList {
		m := tgbotapi.NewMessage(adminID, header)
		m.ParseMode = tgbotapi.ModeHTML
		sent, err := instance.Send(m)
		if err == nil && firstAdminMsgID == nil {
			id := sent.MessageID
			firstAdminMsgID = &id
			aid := adminID
			firstAdminID = &aid
		}
	}

	store.SaveSupportMessage(ctx, chatID, username, "in", msg.Text, firstAdminID, firstAdminMsgID)
	send(chatID, "✅ Сообщение передано администраторам.")
}

func handleAdminReply(ctx context.Context, msg *tgbotapi.Message) {
	if msg.ReplyToMessage == nil {
		return
	}
	replyToMsgID := msg.ReplyToMessage.MessageID
	userTelegramID := store.GetUserTelegramIDByAdminMsgID(ctx, replyToMsgID)
	if userTelegramID == 0 {
		return
	}

	adminID := msg.Chat.ID
	store.SaveSupportMessage(ctx, userTelegramID, "", "out", msg.Text, &adminID, nil)

	adminUsername := ""
	if msg.From != nil {
		adminUsername = msg.From.UserName
	}
	text := fmt.Sprintf("📩 Ответ от поддержки (@%s):\n\n%s", adminUsername, msg.Text)
	SendMessage(userTelegramID, text)
}

// ─── Texts ────────────────────────────────────────────────────────────────────

func welcomeText() string {
	cfg := config.Get()
	name := cfg.SiteName
	if name == "" {
		name = "Lampa"
	}
	return fmt.Sprintf(
		"👋 Привет! Это бот <b>%s</b>.\n\n"+
			"Чтобы привязать Telegram к аккаунту, перейдите в личный кабинет и используйте кнопку привязки.\n\n"+
			"Если у вас есть вопросы — просто напишите сообщение, оно будет передано администраторам.",
		name,
	)
}

func adminHelp() string {
	return "🛠 Команды администратора:\n\n" +
		"/info &lt;username&gt; — информация об аккаунте\n" +
		"/setpremium &lt;username&gt; — установить роль premium\n" +
		"/setsuper &lt;username&gt; — установить роль super\n" +
		"/setsimple &lt;username&gt; — установить роль simple\n" +
		"/broadcast &lt;текст&gt; — рассылка всем пользователям"
}

func roleLabel(role string) string {
	switch role {
	case "premium":
		return "Premium"
	case "super":
		return "Super"
	default:
		return "Simple"
	}
}
