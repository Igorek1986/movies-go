package myshows

import (
	"context"
	"fmt"
	"io"
	"movies-api/db/postgres"
	"movies-api/db/store"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Run with EPISODES_TEST_DATABASE_URL pointing at a disposable PostgreSQL.
// All fixtures live in a unique schema; existing application data is untouched.
func TestEpisodeRefreshIntegration(t *testing.T) {
	dsn := os.Getenv("EPISODES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("EPISODES_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("test database configuration failed")
	}
	defer admin.Close()
	schema := pgx.Identifier{fmt.Sprintf("episode_refresh_%d", time.Now().UnixNano())}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal("create test schema failed")
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("test database configuration failed")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("test database connection failed")
	}
	defer pool.Close()
	previous := postgres.Pool
	postgres.Pool = pool
	defer func() { postgres.Pool = previous }()
	sql, err := os.ReadFile("../../db/postgres/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(sql)); err != nil {
		t.Fatal("test schema initialization failed")
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO media_cards(card_id,tmdb_id,media_type,title,original_title,myshows_id,status,episodes_synced_at)
 VALUES ('94947_tv',94947,'tv','Test show','Test show',63875,'Ended',now()-interval '40 days')`)
	exec(`INSERT INTO episodes(tmdb_show_id,season,episode,is_special,hash,air_date)
 VALUES (94947,5,12,false,'old-episode','2025-12-11')`)
	var requests, status atomic.Int32
	status.Store(http.StatusServiceUnavailable)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if status.Load() != 200 {
			w.WriteHeader(int(status.Load()))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"episodes":[{"id":123456,"seasonNumber":6,"episodeNumber":1,"title":"New episode","airDate":"2026-11-16T11:00:00+0300","isSpecial":false}]}}`)
	}))
	defer server.Close()
	store.SetSetting(ctx, "myshows_api_url", server.URL)
	defer store.SetSetting(ctx, "myshows_api_url", "")
	mc := store.GetMediaCardEpInfo(ctx, "94947_tv")
	if mc == nil {
		t.Fatal("fixture card missing")
	}
	candidates, err := store.GetEpisodeRefreshCandidates(ctx, 0)
	if err != nil || len(candidates) != 1 {
		t.Fatal("ended card must be eligible for rediscovery")
	}
	if err := RefreshEpisodes(ctx, mc); err == nil {
		t.Fatal("source failure must be returned")
	}
	after := store.GetMediaCardEpInfo(ctx, mc.CardID)
	if !after.EpisodesSyncedAt.Equal(*mc.EpisodesSyncedAt) || store.EpisodeCount(ctx, 94947) != 1 {
		t.Fatal("failed refresh changed episodes or successful-sync timestamp")
	}
	status.Store(200)
	requests.Store(0)
	var invalidations atomic.Int32
	oldHook := OnEpisodesUpdated
	OnEpisodesUpdated = func(string) { invalidations.Add(1) }
	defer func() { OnEpisodesUpdated = oldHook }()
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- RefreshEpisodes(ctx, mc) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 || invalidations.Load() != 1 {
		t.Fatal("concurrent stale refreshes must fetch and invalidate once")
	}
	after = store.GetMediaCardEpInfo(ctx, mc.CardID)
	if !after.EpisodesSyncedAt.After(*mc.EpisodesSyncedAt) || store.EpisodeCount(ctx, 94947) != 2 {
		t.Fatal("new season was not persisted alongside old episodes")
	}
	// Calendar and device refresh must see the new season even with Ended status.
	exec(`INSERT INTO users(id,username,password_hash) VALUES(1,'fixture','unused')`)
	exec(`INSERT INTO devices(id,user_id,name,token) VALUES(1,1,'fixture','fixture-token')`)
	exec(`INSERT INTO timecodes(device_id,profile_id,card_id,item,data) VALUES(1,'','94947_tv','old-episode','{}')`)
	exec(`UPDATE media_cards SET episodes_synced_at=now()-interval '2 days'`)
	candidates, err = store.GetEpisodeRefreshCandidates(ctx, 1)
	if err != nil || len(candidates) != 1 || !candidates[0].Tracked {
		t.Fatal("tracked Ended show must refresh daily")
	}
	calendar := store.UpcomingEpisodes(ctx, 1, "", "2026-11-01", "2026-12-01")
	if len(calendar) != 1 || calendar[0].Season != 6 || calendar[0].Episode != 1 {
		t.Fatal("new season absent from calendar")
	}
}
