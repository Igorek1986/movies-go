package tasks

import (
	"context"
	"log"
	"movies-api/db/store"
	"movies-api/internal/myshows"
	"sync"
	"sync/atomic"
	"time"
)

const refreshEpisodesWorkers = 3

var refreshEpisodesRunning atomic.Bool

// RunRefreshOngoingEpisodes refreshes stale episode lists, including ended shows.
// Safe to call concurrently — only one instance runs at a time.
func RunRefreshOngoingEpisodes(ctx context.Context) {
	if !refreshEpisodesRunning.CompareAndSwap(false, true) {
		log.Println("tasks: refresh_episodes already running")
		return
	}
	defer refreshEpisodesRunning.Store(false)

	log.Println("tasks: refresh_episodes started")

	cards, err := store.GetEpisodeRefreshCandidates(ctx, 0)
	if err != nil {
		log.Printf("tasks: refresh_episodes query failed")
		return
	}

	total := len(cards)
	log.Printf("tasks: refresh_episodes: %d shows to process", total)
	if total == 0 {
		return
	}

	work := make(chan *store.MediaCardEpInfo, refreshEpisodesWorkers*2)
	var synced, failed int64
	var wg sync.WaitGroup

	for range refreshEpisodesWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for mc := range work {
				if !myshows.ShouldSync(mc) {
					continue
				}
				syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				err := myshows.RefreshEpisodes(syncCtx, mc)
				cancel()
				if err != nil {
					atomic.AddInt64(&failed, 1)
				} else {
					atomic.AddInt64(&synced, 1)
				}
			}
		}()
	}

	// Bound catalog catch-up traffic to two requests per second.
	pace := time.NewTicker(500 * time.Millisecond)
	defer pace.Stop()
	for _, mc := range cards {
		select {
		case <-ctx.Done():
			goto done
		case <-pace.C:
		}
		select {
		case <-ctx.Done():
			goto done
		case work <- mc:
		}
	}
done:
	close(work)
	wg.Wait()
	log.Printf("tasks: refresh_episodes done: synced=%d failed=%d total=%d", synced, failed, total)
}

// runEpisodeRefreshLoop discovers new seasons without requiring an open card.
func runEpisodeRefreshLoop(ctx context.Context) {
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	for {
		RunRefreshOngoingEpisodes(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
