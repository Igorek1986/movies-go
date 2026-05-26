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

const refreshCardsWorkers = 20

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

	newYearDelta := store.GetSettingInt(ctx, "tmdb_refresh_new_year_delta")
	if newYearDelta <= 0 {
		newYearDelta = 2
	}
	oldBatch := store.GetSettingInt(ctx, "tmdb_refresh_old_batch")
	if oldBatch <= 0 {
		oldBatch = 10000
	}
	ageDays := store.GetSettingInt(ctx, "tmdb_refresh_age_days")
	if ageDays <= 0 {
		ageDays = 30
	}

	// Новые карточки: вышли за последние newYearDelta лет — обновляем ежедневно.
	// Старые карточки: batch из oldBatch штук, ротация по tmdb_updated_at ASC.
	rows, err := postgres.Pool.Query(ctx, `
		SELECT card_id, tmdb_id, media_type FROM (
			SELECT card_id, tmdb_id, media_type, tmdb_updated_at FROM media_cards
			WHERE COALESCE(release_date, first_air_date) > now() - ($1 * interval '1 year')
			  AND (tmdb_updated_at IS NULL OR tmdb_updated_at < now() - interval '1 day')
			UNION ALL
			SELECT card_id, tmdb_id, media_type, tmdb_updated_at FROM media_cards
			WHERE (COALESCE(release_date, first_air_date) IS NULL
			    OR COALESCE(release_date, first_air_date) <= now() - ($1 * interval '1 year'))
			  AND (tmdb_updated_at IS NULL OR tmdb_updated_at < now() - ($2 * interval '1 day'))
		) t
		ORDER BY tmdb_updated_at ASC NULLS FIRST
		LIMIT $3`,
		newYearDelta, ageDays, oldBatch,
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
	log.Printf("tasks: refresh_cards: %d cards to process (new<=%dy daily + old batch=%d age=%dd)",
		total, newYearDelta, oldBatch, ageDays)
	startedAt := time.Now()
	refreshCardsTotal.Store(total)
	if total == 0 {
		return
	}

	work := make(chan refreshCardRow, refreshCardsWorkers*2)
	var wg sync.WaitGroup

	logStep := total / 10
	if logStep < 100 {
		logStep = 100
	}

	for range refreshCardsWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range work {
				isMovie := c.MediaType == "movie"
				md := tmdb.FetchVideoDetails(isMovie, c.TmdbID)
				if md != nil {
					store.RefreshCardTMDB(ctx, c.CardID, md)
					store.ClearCardTMDBNotFound(ctx, c.CardID)
					refreshCardsUpdated.Add(1)
				} else {
					log.Printf("tasks: refresh_cards: TMDB not found tmdb_id=%d type=%s card=%s", c.TmdbID, c.MediaType, c.CardID)
					store.MarkCardTMDBNotFound(ctx, c.CardID)
				}
				cur := refreshCardsCurrent.Add(1)
				if cur%logStep == 0 {
					log.Printf("tasks: refresh_cards: progress %d/%d updated=%d", cur, total, refreshCardsUpdated.Load())
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
	log.Printf("tasks: refresh_cards done: updated=%d/%d elapsed=%s",
		refreshCardsUpdated.Load(), total, time.Since(startedAt).Round(time.Second))
}
