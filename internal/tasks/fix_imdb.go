package tasks

import (
	"context"
	"log"
	"movies-api/db/postgres"
	"movies-api/movies/tmdb"
	"sync"
	"sync/atomic"
)

const fixImdbWorkers = 5

var (
	fixImdbRunning atomic.Bool
	fixImdbCurrent atomic.Int64
	fixImdbTotal   atomic.Int64
	fixImdbFixed   atomic.Int64

	fixImdbMu     sync.Mutex
	fixImdbCancel context.CancelFunc
)

// FixImdbStatus holds the current state of the fix-imdb task.
type FixImdbStatus struct {
	Running bool  `json:"running"`
	Current int64 `json:"current"`
	Total   int64 `json:"total"`
	Fixed   int64 `json:"fixed"`
}

// GetFixImdbStatus returns a snapshot of the current task state.
func GetFixImdbStatus() FixImdbStatus {
	return FixImdbStatus{
		Running: fixImdbRunning.Load(),
		Current: fixImdbCurrent.Load(),
		Total:   fixImdbTotal.Load(),
		Fixed:   fixImdbFixed.Load(),
	}
}

// StopFixMissingImdbID cancels the currently running fix-imdb task, if any.
func StopFixMissingImdbID() {
	fixImdbMu.Lock()
	defer fixImdbMu.Unlock()
	if fixImdbCancel != nil {
		fixImdbCancel()
	}
}

type fixImdbRow struct {
	CardID string
	TmdbID int64
}

// RunFixMissingImdbID backfills media_cards.imdb_id for TV shows that don't
// have one yet, via TMDB's /tv/{id}/external_ids (see tmdb.FetchTVImdbID).
// Movies already get imdb_id from the base /movie/{id} response on every
// normal enrichment — this one-off backfill is TV-only, since TMDB never
// includes it there without the extra call (see fixEntity in
// movies/tmdb/utils.go, wired in going forward for new/refreshed cards).
// Populating this unblocks exact-ID lookups against other sources matched by
// imdb_id (TVmaze, poiskkino, Kinopoisk Api Unofficial) instead of fuzzy
// title search. Safe to call concurrently — only one instance runs at a time.
func RunFixMissingImdbID(parentCtx context.Context) {
	if tmdb.TMDBAuthKey == "" {
		log.Println("tasks: fix_imdb skipped — TMDB token not configured")
		return
	}
	if !fixImdbRunning.CompareAndSwap(false, true) {
		log.Println("tasks: fix_imdb already running")
		return
	}

	ctx, cancel := context.WithCancel(parentCtx)
	fixImdbMu.Lock()
	fixImdbCancel = cancel
	fixImdbMu.Unlock()

	fixImdbCurrent.Store(0)
	fixImdbTotal.Store(0)
	fixImdbFixed.Store(0)
	defer func() {
		cancel()
		fixImdbMu.Lock()
		fixImdbCancel = nil
		fixImdbMu.Unlock()
		fixImdbRunning.Store(false)
	}()

	rows, err := postgres.Pool.Query(ctx, `
		SELECT card_id, tmdb_id FROM media_cards
		WHERE media_type = 'tv' AND (imdb_id IS NULL OR imdb_id = '')
		ORDER BY vote_count DESC NULLS LAST`)
	if err != nil {
		log.Printf("tasks: fix_imdb query: %v", err)
		return
	}
	var cards []fixImdbRow
	for rows.Next() {
		var c fixImdbRow
		if rows.Scan(&c.CardID, &c.TmdbID) == nil {
			cards = append(cards, c)
		}
	}
	rows.Close()

	total := int64(len(cards))
	log.Printf("tasks: fix_imdb: %d TV cards to process", total)
	fixImdbTotal.Store(total)
	if total == 0 {
		return
	}

	work := make(chan fixImdbRow, fixImdbWorkers*2)
	var wg sync.WaitGroup
	for range fixImdbWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range work {
				if id := tmdb.FetchTVImdbID(c.TmdbID); id != "" {
					postgres.Pool.Exec(ctx, //nolint:errcheck
						`UPDATE media_cards SET imdb_id = $1, updated_at = now() WHERE card_id = $2`,
						id, c.CardID,
					)
					fixImdbFixed.Add(1)
				}
				fixImdbCurrent.Add(1)
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
	log.Printf("tasks: fix_imdb done: fixed %d/%d", fixImdbFixed.Load(), total)
}
