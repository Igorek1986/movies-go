package tasks

import (
	"context"
	"fmt"
	"log"
	"time"

	"movies-api/db/postgres"
	"movies-api/db/store"
	"movies-api/internal/bot"
)

var appCtx context.Context

// AppCtx returns the application-level context passed to Start.
// Used by admin handlers that need to start tasks tied to the server lifetime.
func AppCtx() context.Context { return appCtx }

// Start launches all background tasks. Call once from main().
func Start(ctx context.Context) {
	appCtx = ctx
	go runDailyLoop(ctx)
	go runDeliveryLoop(ctx)
}

// ─── Daily loop ───────────────────────────────────────────────────────────────

func runDailyLoop(ctx context.Context) {
	for {
		now := time.Now()
		taskHour := store.GetSettingInt(ctx, "daily_task_hour")
		if taskHour <= 0 {
			taskHour = 2
		}
		next := time.Date(now.Year(), now.Month(), now.Day(), taskHour, 0, 0, 0, now.Location())
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
			runDailyTasks(ctx)
		}
	}
}

func runDailyTasks(ctx context.Context) {
	log.Println("tasks: running daily tasks")
	RunPremiumExpiryCheck(ctx)
	RunInactiveUserCheck(ctx)
	go RunFixZeroRuntime(ctx)
	go RunRefreshCards(ctx)
}

// ─── Delivery loop (every 10 min) ────────────────────────────────────────────

func runDeliveryLoop(ctx context.Context) {
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	// Run once immediately
	RunNotificationDelivery(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			RunNotificationDelivery(ctx)
		}
	}
}

// ─── Premium expiry check ─────────────────────────────────────────────────────

func RunPremiumExpiryCheck(ctx context.Context) {
	log.Println("tasks: premium expiry check")
	warnDays := store.GetSettingInt(ctx, "premium_warn_days")
	if warnDays <= 0 {
		warnDays = 3
	}

	// 1. Demote expired premium users
	for _, u := range store.GetExpiredPremiumUsers(ctx) {
		notifyAt := nextDeliveryTime(u.Timezone, u.NotifyStart, u.NotifyEnd)
		store.SetPremiumExpiredNotify(ctx, u.ID, notifyAt)
		cleanupAfterDemotion(ctx, u.ID)
		log.Printf("tasks: demoted %s (premium expired %s)", u.Username, u.PremiumUntil.Format("2006-01-02"))
	}

	// 2. Schedule warning for near-expiring premium
	for _, u := range store.GetExpiringPremiumUsers(ctx, warnDays) {
		notifyAt := nextDeliveryTime(u.Timezone, u.NotifyStart, u.NotifyEnd)
		store.SetPremiumWarningNotify(ctx, u.ID, notifyAt)
		log.Printf("tasks: warning scheduled for %s (expires %s)", u.Username, u.PremiumUntil.Format("2006-01-02"))
	}
}

// cleanupAfterDemotion trims devices and timecodes to simple limits after premium expires.
func cleanupAfterDemotion(ctx context.Context, userID int64) {
	lim := store.LimitsFor("simple")

	// Trim excess devices (keep most recent)
	if lim.MaxDevices > 0 {
		postgres.Pool.Exec(ctx, //nolint:errcheck
			`DELETE FROM devices WHERE id IN (
			     SELECT id FROM devices WHERE user_id=$1
			     ORDER BY created_at DESC OFFSET $2
			 )`, userID, lim.MaxDevices)
	}

	// Trim timecodes per device
	devices := store.GetDevicesWithStats(ctx, userID)
	for _, d := range devices {
		store.TrimToLimitCount(ctx, d.ID, "", "simple")
	}
}

// ─── Notification delivery ────────────────────────────────────────────────────

func RunNotificationDelivery(ctx context.Context) {
	if !bot.Enabled() {
		return
	}
	for _, n := range store.GetPendingNotifications(ctx) {
		if !inDeliveryWindow(n.Timezone, n.NotifyStart, n.NotifyEnd) {
			continue
		}
		text := buildPremiumNotifyText(n.NotifyType, n.Username)
		if text == "" {
			store.ClearPremiumNotify(ctx, n.UserID)
			continue
		}
		if bot.SendMessage(n.TelegramID, text) {
			store.ClearPremiumNotify(ctx, n.UserID)
			log.Printf("tasks: sent %s notification to %s", n.NotifyType, n.Username)
		}
	}
}

func buildPremiumNotifyText(notifyType, username string) string {
	switch notifyType {
	case "warning":
		return fmt.Sprintf("⚠️ <b>%s</b>, ваша подписка Premium скоро истекает. Продлите её, чтобы не потерять доступ к полному функционалу.", username)
	case "expired":
		return fmt.Sprintf("😔 <b>%s</b>, ваша подписка Premium истекла. Вы переведены на базовый план.", username)
	default:
		return ""
	}
}

// ─── Inactive user check ──────────────────────────────────────────────────────

func RunInactiveUserCheck(ctx context.Context) {
	deleteDays := store.GetSettingInt(ctx, "inactive_delete_days")
	warnDays := store.GetSettingInt(ctx, "inactive_warn_days")
	if deleteDays <= 0 {
		deleteDays = 180
	}
	if warnDays <= 0 {
		warnDays = 7
	}

	for _, u := range store.GetInactiveSimpleUsers(ctx, deleteDays) {
		if u.InactWarn && u.WarnedAt != nil && time.Now().After(*u.WarnedAt) {
			// Grace period expired — delete
			if err := store.DeleteUser(ctx, u.ID); err == nil {
				log.Printf("tasks: deleted inactive user %s", u.Username)
			}
			continue
		}
		if !u.InactWarn {
			// First warning
			deleteAt := time.Now().Add(time.Duration(warnDays) * 24 * time.Hour)
			postgres.Pool.Exec(ctx, //nolint:errcheck
				`UPDATE users SET inactive_warned=true, notify_inactive_after=$2 WHERE id=$1`,
				u.ID, deleteAt)
			// Notify if Telegram linked
			if u.TelegramID != nil {
				bot.SendMessage(*u.TelegramID, fmt.Sprintf(
					"⚠️ <b>%s</b>, ваш аккаунт будет удалён через %d дней из-за неактивности. Войдите в систему, чтобы сохранить его.",
					u.Username, warnDays,
				))
			}
			log.Printf("tasks: warned inactive user %s (delete in %d days)", u.Username, warnDays)
		}
	}
}

// ─── Timezone helpers ─────────────────────────────────────────────────────────

// nextDeliveryTime returns the next time within [notifyStart, notifyEnd) in the user's timezone.
func nextDeliveryTime(timezone string, notifyStart, notifyEnd int) time.Time {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	candidate := time.Date(now.Year(), now.Month(), now.Day(), notifyStart, 0, 0, 0, loc)
	if now.Hour() >= notifyEnd || now.Before(candidate) {
		// Already past window today or before window — use tomorrow
		candidate = candidate.Add(24 * time.Hour)
	}
	return candidate.UTC()
}

// inDeliveryWindow checks if current time in the user's timezone falls within [start, end).
func inDeliveryWindow(timezone string, start, end int) bool {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	h := time.Now().In(loc).Hour()
	return h >= start && h < end
}
