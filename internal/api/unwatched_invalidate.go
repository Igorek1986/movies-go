package api

import (
	"context"
	"encoding/json"
	"log"
	"movies-api/db/store"
	"time"
)

// StartUnwatchedCutoffInvalidation runs a daily loop that, at the moment episodes
// airing "today" cross the aired_cutoff threshold (setting aired_cutoff_hour, in
// default_timezone), invalidates the watched/unwatched cache for exactly the shows
// that just became newly aired — the one wall-clock event that can make a show
// newly eligible without anyone having watched anything (see store.AiredCutoffDate).
// Without this, a cache warmed earlier the same day (e.g. checked at 15:00, before a
// 20:00 cutoff) would keep hiding a just-aired episode — or keep a show hidden by
// hide_watched even though it's no longer fully watched — until some unrelated
// timecode write happened to invalidate it, or the server restarted.
//
// aired_cutoff_days shifts which day's episodes qualify but not the time-of-day the
// crossing happens, so only aired_cutoff_hour matters for scheduling; aired_cutoff_days
// is still applied to work out which air_date just started counting as aired.
//
// Targeted via InvalidateWatchedForCard (same reverse-index path as
// myshows.OnEpisodesUpdated) rather than wiping every profile's cache — only shows
// with an episode airing on that date are affected.
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
		for {
			next, cutoffDays := nextAiredCutoffFire(ctx)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Until(next)):
				newlyAired := next.AddDate(0, 0, -cutoffDays).Format("2006-01-02")
				cardIDs := store.CardIDsAiringOn(ctx, newlyAired)
				for _, id := range cardIDs {
					InvalidateWatchedForCard(id)
				}
				log.Printf("catcache: aired cutoff crossed, invalidated %d show(s) airing %s", len(cardIDs), newlyAired)

				msg, _ := json.Marshal(map[string]any{"type": "unwatched_stale"})
				TimecodeHub.BroadcastAll(msg)
			}
		}
	}()
}

// nextAiredCutoffFire returns the next wall-clock moment the aired cutoff crosses,
// plus the current aired_cutoff_days setting (needed by the caller to work out which
// air_date just started counting as aired).
func nextAiredCutoffFire(ctx context.Context) (time.Time, int) {
	hour := store.GetSettingInt(ctx, "aired_cutoff_hour")
	days := store.GetSettingInt(ctx, "aired_cutoff_days")
	tzName, _ := store.GetSetting(ctx, "default_timezone")
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, loc)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next, days
}
