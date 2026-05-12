package tasks

import (
	"context"
	"lampa-api/db/postgres"
	"lampa-api/movies/tmdb"
	"log"
	"sync"
	"sync/atomic"
)

const fixRuntimeWorkers = 3

var (
	fixRuntimeRunning atomic.Bool
	fixRuntimeStage   atomic.Value // string: "movie" | "tv" | ""
	fixRuntimeCurrent atomic.Int64
	fixRuntimeTotal   atomic.Int64
	fixRuntimeFixed   atomic.Int64

	fixRuntimeMu     sync.Mutex
	fixRuntimeCancel context.CancelFunc
)

// FixRuntimeStatus holds the current state of the fix-runtime task.
type FixRuntimeStatus struct {
	Running bool   `json:"running"`
	Stage   string `json:"stage"`
	Current int64  `json:"current"`
	Total   int64  `json:"total"`
	Fixed   int64  `json:"fixed"`
}

// GetFixRuntimeStatus returns a snapshot of the current task state.
func GetFixRuntimeStatus() FixRuntimeStatus {
	stage, _ := fixRuntimeStage.Load().(string)
	return FixRuntimeStatus{
		Running: fixRuntimeRunning.Load(),
		Stage:   stage,
		Current: fixRuntimeCurrent.Load(),
		Total:   fixRuntimeTotal.Load(),
		Fixed:   fixRuntimeFixed.Load(),
	}
}

// StopFixZeroRuntime cancels the currently running fix-runtime task, if any.
func StopFixZeroRuntime() {
	fixRuntimeMu.Lock()
	defer fixRuntimeMu.Unlock()
	if fixRuntimeCancel != nil {
		fixRuntimeCancel()
	}
}

// RunFixZeroRuntime fetches runtime from TMDB for all movies with runtime=0
// and episode_run_time for TV shows with episode_run_time=0.
// parentCtx should be the app-level context so SIGTERM stops the task.
// Safe to call concurrently — only one instance runs at a time.
func RunFixZeroRuntime(parentCtx context.Context) {
	if tmdb.TMDBAuthKey == "" {
		log.Println("tasks: fix_runtime skipped — TMDB token not configured")
		return
	}
	if !fixRuntimeRunning.CompareAndSwap(false, true) {
		log.Println("tasks: fix_runtime already running")
		return
	}

	ctx, cancel := context.WithCancel(parentCtx)
	fixRuntimeMu.Lock()
	fixRuntimeCancel = cancel
	fixRuntimeMu.Unlock()

	fixRuntimeCurrent.Store(0)
	fixRuntimeTotal.Store(0)
	fixRuntimeFixed.Store(0)
	fixRuntimeStage.Store("")
	defer func() {
		cancel()
		fixRuntimeMu.Lock()
		fixRuntimeCancel = nil
		fixRuntimeMu.Unlock()
		fixRuntimeRunning.Store(false)
		fixRuntimeStage.Store("")
	}()

	log.Println("tasks: fix_runtime started")
	fixRuntimeForType(ctx, "movie", "runtime")
	if ctx.Err() == nil {
		fixRuntimeForType(ctx, "tv", "episode_run_time")
	}
	log.Println("tasks: fix_runtime finished")
}

type fixRow struct {
	CardID string
	TmdbID int64
}

func fixRuntimeForType(ctx context.Context, mediaType, col string) {
	rows, err := postgres.Pool.Query(ctx,
		`SELECT card_id, tmdb_id FROM media_cards
		 WHERE media_type = $1 AND ("`+col+`" IS NULL OR "`+col+`" = 0)
		 ORDER BY vote_count DESC NULLS LAST`,
		mediaType,
	)
	if err != nil {
		log.Printf("tasks: fix_runtime %s query: %v", mediaType, err)
		return
	}

	var cards []fixRow
	for rows.Next() {
		var r fixRow
		rows.Scan(&r.CardID, &r.TmdbID) //nolint:errcheck
		cards = append(cards, r)
	}
	rows.Close()

	isMovie := mediaType == "movie"
	total := int64(len(cards))
	log.Printf("tasks: fix_runtime %s: %d cards to process", mediaType, total)

	fixRuntimeStage.Store(mediaType)
	fixRuntimeCurrent.Store(0)
	fixRuntimeTotal.Store(total)

	work := make(chan fixRow, fixRuntimeWorkers*2)

	var wg sync.WaitGroup
	for range fixRuntimeWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range work {
				val := tmdb.FetchRuntime(isMovie, c.TmdbID)
				if val > 0 {
					postgres.Pool.Exec(ctx, //nolint:errcheck
						`UPDATE media_cards SET "`+col+`" = $1, updated_at = now() WHERE card_id = $2`,
						val, c.CardID,
					)
					fixRuntimeFixed.Add(1)
				}
				fixRuntimeCurrent.Add(1)
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

	log.Printf("tasks: fix_runtime %s done: fixed %d/%d", mediaType, fixRuntimeFixed.Load(), total)
}
