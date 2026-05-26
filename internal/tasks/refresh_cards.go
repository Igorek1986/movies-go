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

const refreshCardsWorkers = 3

var (
	refreshCardsRunning atomic.Bool
	refreshCardsCurrent atomic.Int64
	refreshCardsTotal   atomic.Int64
	refreshCardsUpdated atomic.Int64

	refreshCardsMu     sync.Mutex
	refreshCardsCancel context.CancelFunc
)

type RefreshCardsStatus struct {
	Running bool  `json:"running"`
	Current int64 `json:"current"`
	Total   int64 `json:"total"`
	Updated int64 `json:"updated"`
}

func GetRefreshCardsStatus() RefreshCardsStatus {
	return RefreshCardsStatus{
		Running: refreshCardsRunning.Load(),
		Current: refreshCardsCurrent.Load(),
		Total:   refreshCardsTotal.Load(),
		Updated: refreshCardsUpdated.Load(),
	}
}

func StopRefreshCards() {
	refreshCardsMu.Lock()
	defer refreshCardsMu.Unlock()
	if refreshCardsCancel != nil {
		refreshCardsCancel()
	}
}

type refreshCardRow struct {
	CardID    string
	TmdbID    int64
	MediaType string
}

// RunRefreshCards fetches fresh metadata from TMDB for the oldest batch of cards.
// Batch size and minimum age are controlled by app_settings:
//
//	tmdb_refresh_batch    (default 1000) — cards per run
//	tmdb_refresh_age_days (default 30)  — skip cards updated more recently than this
//
// Safe to call concurrently — only one instance runs at a time.
func RunRefreshCards(parentCtx context.Context) {
	if tmdb.TMDBAuthKey == "" {
		log.Println("tasks: refresh_cards skipped — TMDB token not configured")
		return
	}
	if !refreshCardsRunning.CompareAndSwap(false, true) {
		log.Println("tasks: refresh_cards already running")
		return
	}

	ctx, cancel := context.WithCancel(parentCtx)
	refreshCardsMu.Lock()
	refreshCardsCancel = cancel
	refreshCardsMu.Unlock()

	refreshCardsCurrent.Store(0)
	refreshCardsTotal.Store(0)
	refreshCardsUpdated.Store(0)

	defer func() {
		cancel()
		refreshCardsMu.Lock()
		refreshCardsCancel = nil
		refreshCardsMu.Unlock()
		refreshCardsRunning.Store(false)
	}()

	batchSize := store.GetSettingInt(ctx, "tmdb_refresh_batch")
	if batchSize <= 0 {
		batchSize = 1000
	}
	ageDays := store.GetSettingInt(ctx, "tmdb_refresh_age_days")
	if ageDays <= 0 {
		ageDays = 30
	}

	rows, err := postgres.Pool.Query(ctx,
		`SELECT card_id, tmdb_id, media_type FROM media_cards
		 WHERE tmdb_updated_at IS NULL
		    OR tmdb_updated_at < now() - ($1 * interval '1 day')
		 ORDER BY tmdb_updated_at ASC NULLS FIRST
		 LIMIT $2`,
		ageDays, batchSize,
	)
	if err != nil {
		log.Printf("tasks: refresh_cards query: %v", err)
		return
	}

	var cards []refreshCardRow
	for rows.Next() {
		var r refreshCardRow
		rows.Scan(&r.CardID, &r.TmdbID, &r.MediaType) //nolint:errcheck
		cards = append(cards, r)
	}
	rows.Close()

	total := int64(len(cards))
	log.Printf("tasks: refresh_cards: %d cards to process (batch=%d age=%dd)", total, batchSize, ageDays)
	refreshCardsTotal.Store(total)
	if total == 0 {
		return
	}

	work := make(chan refreshCardRow, refreshCardsWorkers*2)
	var wg sync.WaitGroup

	for range refreshCardsWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range work {
				isMovie := c.MediaType == "movie"
				md := tmdb.FetchVideoDetails(isMovie, c.TmdbID)
				if md != nil {
					store.RefreshCardTMDB(ctx, c.CardID, md)
					refreshCardsUpdated.Add(1)
				}
				refreshCardsCurrent.Add(1)
				time.Sleep(50 * time.Millisecond)
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
	log.Printf("tasks: refresh_cards done: updated=%d/%d", refreshCardsUpdated.Load(), total)
}
