package api

import (
	"context"
	"encoding/json"
	"log"
	"movies-api/db/store"
	"time"
)

// How often to re-check whether the aired cutoff has advanced. Short enough
// that a settings change (default_timezone/aired_cutoff_hour/aired_cutoff_days)
// or the daily crossing itself is picked up promptly, cheap enough (two
// GetSettingInt + one GetSetting cache lookup, no query) to just poll rather
// than precompute a single wake-up instant — see StartUnwatchedCutoffInvalidation's
// own comment for why a precomputed instant was the actual bug here.
const unwatchedCutoffPollInterval = time.Minute

// StartUnwatchedCutoffInvalidation runs a loop that, whenever the aired cutoff
// date (setting aired_cutoff_hour/aired_cutoff_days, in default_timezone —
// see store.AiredCutoffDate) advances, invalidates the watched/unwatched cache
// for exactly the shows that just became newly aired — the one wall-clock
// event that can make a show newly eligible without anyone having watched
// anything. Without this, a cache warmed earlier the same day (e.g. checked
// at 15:00, before a 20:00 cutoff) would keep hiding a just-aired episode —
// or keep a show hidden by hide_watched even though it's no longer fully
// watched — until some unrelated timecode write happened to invalidate it,
// or the server restarted.
//
// Polls every unwatchedCutoffPollInterval and compares the cutoff date to
// its last-seen value, rather than computing one fixed wake-up instant and
// sleeping until it (the original design) — that instant is computed once
// from whatever aired_cutoff_hour/aired_cutoff_days/default_timezone were
// at the time, and a goroutine parked in time.After has no way to notice
// those settings changing while it sleeps. Concretely: an admin fixing a
// wrong default_timezone mid-day (exactly the kind of correction this
// setting exists for) wouldn't actually take effect here until the STALE
// schedule — computed from the wrong timezone — happened to elapse on its
// own, up to 24h later; meanwhile push notifications (db/store/push.go,
// FindNewEpisodeNotifications) recompute the cutoff fresh on every poll and
// were already correct, so the two would visibly disagree (a push saying a
// show has new episodes that don't yet appear in "Непросмотренные") for as
// long as this stayed asleep. Re-checking every minute makes it self-correct
// for any settings change, not just the ordinary once-a-day crossing.
//
// Targeted via InvalidateWatchedForCard (same reverse-index path as
// myshows.OnEpisodesUpdated) rather than wiping every profile's cache — only
// shows with an episode airing on the date(s) that just crossed are affected.
// Walks every date strictly after the last-seen cutoff up through the new
// one (usually just one day, but a settings change can jump the cutoff
// forward by more than a day in a single poll) — a cutoff that moves
// BACKWARD (e.g. aired_cutoff_hour increased) just resyncs silently: nothing
// needs invalidating retroactively for that.
//
// Also pushes a lightweight WS broadcast ({"type":"unwatched_stale"}) to every
// connected client (see plugins/np_unwatched.js's connectWS) — cache-clearing
// alone only fixes the count on the NEXT request; a card already rendered on
// a screen that's just sitting open through the cutoff won't re-fetch on its
// own without this nudge. No payload beyond the type: each client re-checks
// its own visible cards against the (uncached) /unwatched/progress endpoint,
// scoped by its own token — nothing about the cutoff itself is user-specific.
func StartUnwatchedCutoffInvalidation(ctx context.Context) {
	go func() {
		// Deliberately one day BEHIND the real current cutoff, not equal to
		// it — a fresh process start (deploy, crash, manual restart) has no
		// idea whether today's crossing was already correctly invalidated
		// before it started. Starting "caught up" would silently trust that
		// it was; starting a day behind makes the very first check below
		// always re-run today's invalidation pass too, which is exactly
		// what's needed to actually recover from this same bug's past
		// damage (a cache entry poisoned before this fix shipped, or one
		// lost to an unrelated restart race) instead of just preventing it
		// from happening again from this point on. Cheap and idempotent —
		// InvalidateWatchedForCard on an already-fine cache entry is a
		// no-op in effect, just a wasted map delete.
		last := currentAiredCutoffDate(ctx).AddDate(0, 0, -1)
		checkAndInvalidate := func() {
			cur := currentAiredCutoffDate(ctx)
			if !cur.After(last) {
				last = cur
				return
			}
			changed := 0
			for d := last.AddDate(0, 0, 1); !d.After(cur); d = d.AddDate(0, 0, 1) {
				dateStr := d.Format("2006-01-02")
				cardIDs := store.CardIDsAiringOn(ctx, dateStr)
				for _, id := range cardIDs {
					InvalidateWatchedForCard(id)
				}
				changed += len(cardIDs)
				log.Printf("catcache: aired cutoff crossed, invalidated %d show(s) airing %s", len(cardIDs), dateStr)
			}
			last = cur
			if changed > 0 {
				// InvalidateWatchedForCard above only reaches profiles whose cached
				// unwatched list ALREADY referenced one of these cards. A show with
				// zero unwatched episodes yesterday (so absent from every cached
				// list) becoming newly eligible today has no such reverse-index
				// entry to invalidate through — see InvalidateAllUnwatched's own
				// comment. A full wipe is the only correct fix, and cheap here:
				// this branch runs at most a few times a day.
				InvalidateAllUnwatched()
				msg, _ := json.Marshal(map[string]any{"type": "unwatched_stale"})
				TimecodeHub.BroadcastAll(msg)
			}
		}
		checkAndInvalidate()
		ticker := time.NewTicker(unwatchedCutoffPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkAndInvalidate()
			}
		}
	}()
}

// currentAiredCutoffDate is the Go-side equivalent of store.AiredCutoffDate's
// SQL expression — kept in sync with it by hand (see that function's own
// comment for why the timezone conversion has to be explicit rather than
// relying on the server/container's own local time). Returned as a UTC
// midnight so callers can compare/subtract dates without any timezone
// involved past this point.
func currentAiredCutoffDate(ctx context.Context) time.Time {
	days := store.GetSettingInt(ctx, "aired_cutoff_days")
	hour := store.GetSettingInt(ctx, "aired_cutoff_hour")
	tzName, _ := store.GetSetting(ctx, "default_timezone")
	if tzName == "" {
		tzName = "Europe/Moscow"
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.UTC
	}
	cutoffInstant := time.Now().In(loc).AddDate(0, 0, -days).Add(-time.Duration(hour) * time.Hour)
	y, m, d := cutoffInstant.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
