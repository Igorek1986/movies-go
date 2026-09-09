package tasks

import (
	"context"
	"log"
	"sync"
	"time"

	"movies-api/db/postgres"
	"movies-api/db/store"
	"movies-api/internal/instancesync"
)

const instanceSyncWorkers = 3

// StartInstanceSyncLoop runs the pull half of instance sync (see
// dev/instance-sync.md) on a timer, using the admin-configured
// sync_interval_minutes. Only does anything when sync_role=="client" — a
// gateway has nothing to pull, and "off" means sync is disabled entirely.
// Runtime pull piggybacks on the existing daily fix_runtime task instead
// (see RunFixZeroRuntime) since it already has the right per-card loop;
// this loop covers the newer per-item pulls (imdb_id, quality/date) that
// don't fit that loop's WHERE clause.
func StartInstanceSyncLoop(ctx context.Context) {
	for {
		minutes := store.GetSettingInt(ctx, "sync_interval_minutes")
		if minutes < 1 {
			minutes = 15
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(minutes) * time.Minute):
			runInstanceSyncPull(ctx)
		}
	}
}

func runInstanceSyncPull(ctx context.Context) {
	role, _ := store.GetSetting(ctx, "sync_role")
	if role != "client" {
		return
	}
	if v, _ := store.GetSetting(ctx, "sync_pull_ids"); v == "1" {
		pullMissingImdbIDs(ctx)
	}
	if v, _ := store.GetSetting(ctx, "sync_pull_quality"); v == "1" {
		pullQualitySignals(ctx)
	}
}

// pullMissingImdbIDs fills imdb_id for cards that don't have one yet — most
// valuable for TV, where imdb_id is essentially never populated locally
// today (see project notes), which otherwise blocks the imdb-matched
// external runtime sources entirely.
func pullMissingImdbIDs(ctx context.Context) {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT card_id FROM media_cards
		WHERE (imdb_id IS NULL OR imdb_id = '')
		ORDER BY vote_count DESC NULLS LAST`)
	if err != nil {
		log.Printf("tasks: instance_sync imdb_id query: %v", err)
		return
	}
	var cardIDs []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			cardIDs = append(cardIDs, id)
		}
	}
	rows.Close()
	if len(cardIDs) == 0 {
		return
	}
	log.Printf("tasks: instance_sync pull imdb_id: %d cards to check", len(cardIDs))

	work := make(chan string, instanceSyncWorkers*2)
	var wg sync.WaitGroup
	var filled int
	var mu sync.Mutex
	for range instanceSyncWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for cardID := range work {
				id := instancesync.FetchImdbID(ctx, cardID)
				if id == "" {
					continue
				}
				postgres.Pool.Exec(ctx, //nolint:errcheck
					`UPDATE media_cards SET imdb_id = $1, updated_at = now() WHERE card_id = $2`,
					id, cardID)
				mu.Lock()
				filled++
				mu.Unlock()
			}
		}()
	}
	for _, id := range cardIDs {
		select {
		case <-ctx.Done():
			goto done
		case work <- id:
		}
	}
done:
	close(work)
	wg.Wait()
	log.Printf("tasks: instance_sync pull imdb_id done: filled %d/%d", filled, len(cardIDs))
}

// pullQualitySignals fills best_video_quality/latest_torrent_date for cards
// where this instance hasn't found anything itself yet — a witness that
// content in that quality was found somewhere in the sync network (see
// instancesync.QualitySignal), not a promise this instance can hand it out
// directly (actual playback lookup is a separate mechanism entirely).
func pullQualitySignals(ctx context.Context) {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT card_id FROM media_cards
		WHERE (best_video_quality IS NULL OR best_video_quality = 0)
		ORDER BY vote_count DESC NULLS LAST`)
	if err != nil {
		log.Printf("tasks: instance_sync quality query: %v", err)
		return
	}
	var cardIDs []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			cardIDs = append(cardIDs, id)
		}
	}
	rows.Close()
	if len(cardIDs) == 0 {
		return
	}
	log.Printf("tasks: instance_sync pull quality: %d cards to check", len(cardIDs))

	work := make(chan string, instanceSyncWorkers*2)
	var wg sync.WaitGroup
	var filled int
	var mu sync.Mutex
	for range instanceSyncWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for cardID := range work {
				sig, ok := instancesync.FetchQuality(ctx, cardID)
				if !ok {
					continue
				}
				postgres.Pool.Exec(ctx, //nolint:errcheck
					`UPDATE media_cards SET
					   best_video_quality = GREATEST(COALESCE(best_video_quality,0), $1),
					   latest_torrent_date = CASE
					     WHEN $2::timestamptz IS NULL THEN latest_torrent_date
					     WHEN latest_torrent_date IS NULL THEN $2::timestamptz
					     ELSE GREATEST(latest_torrent_date, $2::timestamptz)
					   END,
					   updated_at = now()
					 WHERE card_id = $3`,
					sig.BestVideoQuality, nullIfEmpty(sig.TorrentDate), cardID)
				mu.Lock()
				filled++
				mu.Unlock()
			}
		}()
	}
	for _, id := range cardIDs {
		select {
		case <-ctx.Done():
			goto done
		case work <- id:
		}
	}
done:
	close(work)
	wg.Wait()
	log.Printf("tasks: instance_sync pull quality done: filled %d/%d", filled, len(cardIDs))
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
