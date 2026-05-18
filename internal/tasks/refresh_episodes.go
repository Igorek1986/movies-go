package tasks

import (
	"context"
	"movies-api/db/postgres"
	"movies-api/db/store"
	"movies-api/internal/myshows"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

const refreshEpisodesWorkers = 3

var refreshEpisodesRunning atomic.Bool

// RunRefreshOngoingEpisodes syncs episodes from MyShows for all ongoing TV shows
// that have a myshows_id. Safe to call concurrently — only one instance runs at a time.
func RunRefreshOngoingEpisodes(ctx context.Context) {
	if !refreshEpisodesRunning.CompareAndSwap(false, true) {
		log.Println("tasks: refresh_episodes already running")
		return
	}
	defer refreshEpisodesRunning.Store(false)

	log.Println("tasks: refresh_episodes started")

	rows, err := postgres.Pool.Query(ctx, `
		SELECT card_id, tmdb_id, COALESCE(original_title,''), COALESCE(title,''), imdb_id,
		       myshows_id, episodes_synced_at, next_ep_air_date, episode_run_time, status,
		       COALESCE(LEFT(COALESCE(first_air_date::text, release_date::text,''),4),'')
		FROM media_cards
		WHERE media_type = 'tv'
		  AND myshows_id IS NOT NULL
		  AND (
		        status IN ('Returning Series','In Production','Pilot')
		        OR episodes_synced_at IS NULL
		      )
		ORDER BY updated_at DESC`)
	if err != nil {
		log.Printf("tasks: refresh_episodes query: %v", err)
		return
	}

	var cards []*store.MediaCardEpInfo
	for rows.Next() {
		mc := &store.MediaCardEpInfo{}
		if err := rows.Scan(
			&mc.CardID, &mc.TmdbID, &mc.OriginalTitle, &mc.Title, &mc.ImdbID,
			&mc.MyshowsID, &mc.EpisodesSyncedAt, &mc.NextEpAirDate, &mc.EpisodeRunTime,
			&mc.Status, &mc.Year,
		); err == nil {
			cards = append(cards, mc)
		}
	}
	rows.Close()

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
				err := myshows.SyncEpisodes(syncCtx, mc)
				cancel()
				if err != nil {
					atomic.AddInt64(&failed, 1)
				} else {
					atomic.AddInt64(&synced, 1)
				}
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
	log.Printf("tasks: refresh_episodes done: synced=%d failed=%d total=%d", synced, failed, total)
}
