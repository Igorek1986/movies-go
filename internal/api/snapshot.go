package api

import (
	"context"
	"encoding/gob"
	"log"
	"movies-api/db/store"
	"os"
	"time"
)

// cacheSnapshot is the on-disk shape of the persisted in-memory caches, written
// periodically and on graceful shutdown so a restart doesn't start every cache
// stone cold (see catcache.go for what these caches are and why they matter).
type cacheSnapshot struct {
	SavedAt        time.Time
	CatCache       map[string]cachedResp
	WatchedCache   map[string][]string
	UnwatchedCache map[string][]store.UnwatchedTVShow
}

// SaveCacheSnapshot writes the current catCache/watchedCache/unwatchedCache to
// path, atomically (write to a temp file, then rename) so a crash mid-write never
// leaves a corrupt snapshot behind.
func SaveCacheSnapshot(path string) error {
	catCacheMu.RLock()
	snap := cacheSnapshot{
		SavedAt:  time.Now(),
		CatCache: make(map[string]cachedResp, len(catCache)),
	}
	for k, v := range catCache {
		snap.CatCache[k] = v
	}
	catCacheMu.RUnlock()

	watchedMu.RLock()
	snap.WatchedCache = make(map[string][]string, len(watchedCache))
	for k, v := range watchedCache {
		snap.WatchedCache[k] = v
	}
	watchedMu.RUnlock()

	unwatchedMu.RLock()
	snap.UnwatchedCache = make(map[string][]store.UnwatchedTVShow, len(unwatchedCache))
	for k, v := range unwatchedCache {
		snap.UnwatchedCache[k] = v
	}
	unwatchedMu.RUnlock()

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(&snap); err != nil {
		f.Close() //nolint:errcheck
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// StartCacheSnapshotLoop periodically saves the caches to path so a non-graceful
// restart (crash, SIGKILL) loses at most `interval` worth of warmth instead of
// everything. Also saved once on graceful shutdown, see cmd/main.go.
func StartCacheSnapshotLoop(ctx context.Context, path string, interval time.Duration) {
	go func() {
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if err := SaveCacheSnapshot(path); err != nil {
					log.Printf("catcache: snapshot save failed: %v", err)
				}
			}
		}
	}()
}

// LoadCacheSnapshot restores catCache/watchedCache/unwatchedCache from path at
// startup, if present. catCache entries are loaded as immediately stale (served
// instantly from the snapshot, refreshed in the background on next request — see
// withCategoryCache) since we can't be sure the catalog didn't change while the
// process was down. watchedCache/unwatchedCache don't have that generation
// concept, so instead this replays any aired-cutoff crossings between the
// snapshot's SavedAt and now, invalidating exactly the shows affected — the only
// thing that can make them stale while the process isn't running (MyShows sync,
// the other trigger, only ever runs from inside this process).
func LoadCacheSnapshot(ctx context.Context, path string) {
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("catcache: snapshot load failed: %v", err)
		}
		return
	}
	defer f.Close()

	var snap cacheSnapshot
	if err := gob.NewDecoder(f).Decode(&snap); err != nil {
		log.Printf("catcache: snapshot decode failed: %v", err)
		return
	}

	catCacheMu.Lock()
	for k, v := range snap.CatCache {
		v.Generation = -1 // force stale under any real catGeneration (starts at 0)
		catCache[k] = v
		catCacheElems[k] = catCacheOrder.PushFront(k)
	}
	catCacheMu.Unlock()

	watchedMu.Lock()
	for k, ids := range snap.WatchedCache {
		watchedCache[k] = ids
		for _, id := range ids {
			indexWatchedCard(id, k)
		}
	}
	watchedMu.Unlock()

	unwatchedMu.Lock()
	for k, shows := range snap.UnwatchedCache {
		unwatchedCache[k] = shows
		for _, s := range shows {
			indexUnwatchedCard(s.CardID, k)
		}
	}
	unwatchedMu.Unlock()

	log.Printf("catcache: restored snapshot from %s (saved %s ago): %d categories, %d watched-sets, %d unwatched-lists",
		path, time.Since(snap.SavedAt).Round(time.Second), len(snap.CatCache), len(snap.WatchedCache), len(snap.UnwatchedCache))

	replayMissedCutoffs(ctx, snap.SavedAt)
}

// replayMissedCutoffs invalidates watched/unwatched cache entries for shows whose
// episodes crossed the aired-cutoff threshold at any point between since and now —
// covers downtime spanning one or more of those daily crossings, which the
// snapshot alone can't reflect.
func replayMissedCutoffs(ctx context.Context, since time.Time) {
	if since.IsZero() {
		return
	}
	total := 0
	for d := since; !d.After(time.Now()); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		for _, id := range store.CardIDsAiringOn(ctx, date) {
			InvalidateWatchedForCard(id)
			total++
		}
	}
	if total > 0 {
		// InvalidateWatchedForCard only reaches unwatched-cache entries restored
		// from the snapshot that already referenced one of these cards. A show
		// that crossed the cutoff during the downtime and had zero unwatched
		// episodes as of the snapshot (so wasn't in any list captured in it) has
		// no such entry to invalidate through — see InvalidateAllUnwatched's own
		// comment in catcache.go. Wipe the whole list cache to be correct.
		InvalidateAllUnwatched()
		log.Printf("catcache: replayed missed aired-cutoff crossings since snapshot, invalidated %d show(s)", total)
	}
}
