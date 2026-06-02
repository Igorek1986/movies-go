package tasks

import (
	"context"
	"movies-api/db/postgres"
	"movies-api/db/store"
	"movies-api/internal/myshows"
	"movies-api/movies/tmdb"
	"log"
	"sync"
	"sync/atomic"
	"time"
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
// and episode_run_time for TV shows with episode_run_time=0. For TV shows that
// TMDB has no runtime for, it falls back to the median episode runtime from MyShows.
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
		fixRuntimeTV(ctx)
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

// fixRuntimeTV fills episode_run_time for TV shows: TMDB first, then MyShows
// (median episode runtime) as a fallback for shows TMDB has no runtime for.
func fixRuntimeTV(ctx context.Context) {
	rows, err := postgres.Pool.Query(ctx, `
		SELECT card_id, tmdb_id, COALESCE(original_title,''), COALESCE(title,''), imdb_id,
		       myshows_id,
		       COALESCE(LEFT(COALESCE(first_air_date::text, release_date::text,''),4),'')
		FROM media_cards
		WHERE media_type = 'tv' AND (episode_run_time IS NULL OR episode_run_time = 0)
		ORDER BY vote_count DESC NULLS LAST`)
	if err != nil {
		log.Printf("tasks: fix_runtime tv query: %v", err)
		return
	}
	var cards []*store.MediaCardEpInfo
	for rows.Next() {
		mc := &store.MediaCardEpInfo{}
		if rows.Scan(&mc.CardID, &mc.TmdbID, &mc.OriginalTitle, &mc.Title,
			&mc.ImdbID, &mc.MyshowsID, &mc.Year) == nil {
			cards = append(cards, mc)
		}
	}
	rows.Close()

	total := int64(len(cards))
	log.Printf("tasks: fix_runtime tv: %d cards to process", total)

	fixRuntimeStage.Store("tv")
	fixRuntimeCurrent.Store(0)
	fixRuntimeTotal.Store(total)

	work := make(chan *store.MediaCardEpInfo, fixRuntimeWorkers*2)
	var wg sync.WaitGroup
	for range fixRuntimeWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for mc := range work {
				rt := tmdb.FetchRuntime(false, mc.TmdbID)
				if rt == 0 {
					rt = myshowsRuntimeFallback(ctx, mc)
				}
				if rt > 0 {
					postgres.Pool.Exec(ctx, //nolint:errcheck
						`UPDATE media_cards SET episode_run_time = $1, updated_at = now() WHERE card_id = $2`,
						rt, mc.CardID,
					)
					fixRuntimeFixed.Add(1)
				}
				fixRuntimeCurrent.Add(1)
			}
		}()
	}

	for _, mc := range cards {
		select {
		case <-ctx.Done():
			goto done
		case work <- mc:
		}
	}
done:
	close(work)
	wg.Wait()

	log.Printf("tasks: fix_runtime tv done: fixed %d/%d", fixRuntimeFixed.Load(), total)
}

// myshowsRuntimeFallback resolves the show on MyShows (caching the id) and returns
// its median episode runtime in minutes, or 0 if not found.
func myshowsRuntimeFallback(ctx context.Context, mc *store.MediaCardEpInfo) int {
	mctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if mc.MyshowsID == nil {
		sid := myshows.FindShow(mctx, mc, "")
		if sid == 0 {
			return 0
		}
		store.SetMyshowsID(mctx, mc.CardID, sid) //nolint:errcheck
		mc.MyshowsID = &sid
	}
	return myshows.FetchEpisodeRuntime(mctx, mc)
}
