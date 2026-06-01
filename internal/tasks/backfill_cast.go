package tasks

import (
	"context"
	"log"
	"movies-api/db/postgres"
	"movies-api/db/store"
	"movies-api/movies/tmdb"
	"sync"
	"sync/atomic"
	"time"
)

const backfillCastWorkers = 10

var (
	backfillCastRunning atomic.Bool
	backfillCastCurrent atomic.Int64
	backfillCastTotal   atomic.Int64
	backfillCastUpdated atomic.Int64

	backfillCastMu     sync.Mutex
	backfillCastCancel context.CancelFunc
)

type BackfillCastStatus struct {
	Running bool  `json:"running"`
	Current int64 `json:"current"`
	Total   int64 `json:"total"`
	Updated int64 `json:"updated"`
}

func GetBackfillCastStatus() BackfillCastStatus {
	return BackfillCastStatus{
		Running: backfillCastRunning.Load(),
		Current: backfillCastCurrent.Load(),
		Total:   backfillCastTotal.Load(),
		Updated: backfillCastUpdated.Load(),
	}
}

func StopBackfillCast() {
	backfillCastMu.Lock()
	defer backfillCastMu.Unlock()
	if backfillCastCancel != nil {
		backfillCastCancel()
	}
}

// RunBackfillCast fetches and stores cast for all cards that have no entries in media_card_cast.
func RunBackfillCast(parentCtx context.Context) {
	if tmdb.TMDBAuthKey == "" {
		log.Println("tasks: backfill_cast skipped — TMDB token not configured")
		return
	}
	if !backfillCastRunning.CompareAndSwap(false, true) {
		log.Println("tasks: backfill_cast already running")
		return
	}

	ctx, cancel := context.WithCancel(parentCtx)
	backfillCastMu.Lock()
	backfillCastCancel = cancel
	backfillCastMu.Unlock()

	backfillCastCurrent.Store(0)
	backfillCastTotal.Store(0)
	backfillCastUpdated.Store(0)

	defer func() {
		cancel()
		backfillCastMu.Lock()
		backfillCastCancel = nil
		backfillCastMu.Unlock()
		backfillCastRunning.Store(false)
	}()

	rows, err := postgres.Pool.Query(ctx, `
		SELECT card_id, tmdb_id, media_type FROM media_cards
		WHERE card_id NOT IN (SELECT DISTINCT card_id FROM media_card_cast)
		ORDER BY vote_average DESC NULLS LAST`)
	if err != nil {
		log.Printf("tasks: backfill_cast query: %v", err)
		return
	}

	type row struct{ CardID string; TmdbID int64; MediaType string }
	var cards []row
	for rows.Next() {
		var r row
		rows.Scan(&r.CardID, &r.TmdbID, &r.MediaType) //nolint:errcheck
		cards = append(cards, r)
	}
	rows.Close()

	total := int64(len(cards))
	log.Printf("tasks: backfill_cast: %d cards without cast", total)
	backfillCastTotal.Store(total)
	if total == 0 {
		return
	}

	startedAt := time.Now()
	work := make(chan row, backfillCastWorkers*2)
	var wg sync.WaitGroup

	logStep := total / 10
	if logStep < 100 {
		logStep = 100
	}

	for range backfillCastWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range work {
				isMovie := c.MediaType == "movie"
				md := tmdb.FetchVideoDetails(isMovie, c.TmdbID)
				if md != nil && md.Credits != nil {
					store.UpsertCast(ctx, c.CardID, md.Credits.Cast)
					backfillCastUpdated.Add(1)
				}
				cur := backfillCastCurrent.Add(1)
				if cur%logStep == 0 {
					log.Printf("tasks: backfill_cast: progress %d/%d updated=%d", cur, total, backfillCastUpdated.Load())
				}
			}
		}()
	}

	for _, c := range cards {
		select {
		case <-ctx.Done():
			goto done
		case work <- c:
		}
	}
done:
	close(work)
	wg.Wait()
	log.Printf("tasks: backfill_cast done: updated=%d/%d elapsed=%s",
		backfillCastUpdated.Load(), total, time.Since(startedAt).Round(time.Second))
}
