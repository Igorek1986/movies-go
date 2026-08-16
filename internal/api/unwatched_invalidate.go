package api

import (
	"context"
	"log"
	"movies-api/db/store"
	"time"
)

// StartUnwatchedCutoffInvalidation runs a daily loop that drops the entire
// "Непросмотренные" cache (unwatchedCache) at the moment episodes airing
// "today" cross the aired_cutoff threshold (setting aired_cutoff_hour, in
// default_timezone) — the one wall-clock event that can make a show newly
// eligible without anyone having watched anything (see store.AiredCutoffDate).
// Without this, a cache warmed earlier the same day (e.g. checked at 15:00,
// before a 20:00 cutoff) would keep hiding a just-aired episode until some
// unrelated timecode write happened to invalidate it, or the server restarted
// — someone who only opens Lampa, looks, and closes it (without watching
// anything) could miss a new episode indefinitely.
//
// aired_cutoff_days shifts which day's episodes qualify but not the
// time-of-day the crossing happens, so only aired_cutoff_hour matters here.
func StartUnwatchedCutoffInvalidation(ctx context.Context) {
	go func() {
		for {
			next := nextAiredCutoffFire(ctx)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Until(next)):
				unwatchedMu.Lock()
				unwatchedCache = map[string][]store.UnwatchedTVShow{}
				unwatchedMu.Unlock()
				log.Println("catcache: unwatched cache cleared (aired cutoff crossed)")
			}
		}
	}()
}

func nextAiredCutoffFire(ctx context.Context) time.Time {
	hour := store.GetSettingInt(ctx, "aired_cutoff_hour")
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
	return next
}
