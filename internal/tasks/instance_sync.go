package tasks

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"movies-api/db/store"
)

var syncHTTPClient = &http.Client{Timeout: 20 * time.Second}

// StartInstanceSyncLoop runs instance sync's client side (see
// dev/instance-sync.md) on a timer, using the admin-configured
// sync_interval_minutes. A no-op tick if sync_peer_url isn't set — a hub
// (empty peer_url) has nothing to pull or push, it only serves (see
// internal/api/sync_serve.go, always on, no toggle).
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
			runInstanceSyncTick(ctx)
		}
	}
}

// runInstanceSyncTick does everything there is to do with a peer — no
// separate per-dataset toggles: pull (cards then events, cards first since
// events reference card_id via FK) always runs once a peer is set; push
// (same order) additionally requires sync_token — the credential this
// instance presents when pushing, which must match the peer's own
// sync_token (see internal/api/sync_serve.go).
func runInstanceSyncTick(ctx context.Context) {
	peer, _ := store.GetSetting(ctx, "sync_peer_url")
	peer = strings.TrimRight(peer, "/")
	if peer == "" {
		return
	}
	pullCards(ctx, peer)
	pullEvents(ctx, peer)
	if token, _ := store.GetSetting(ctx, "sync_token"); token != "" {
		pushCards(ctx, peer, token)
		pushEvents(ctx, peer, token)
	}
}

// syncCursorPages drives one dataset's incremental pull: repeatedly GETs
// peer+path?since=cursor&limit=..., applying each page via apply, advancing
// and persisting the cursor to settingKey only after a page's rows were all
// successfully written locally. Persisting after (not before) the write is
// what gives this natural resilience to a mid-page network/write failure —
// the next tick just retries the same page (see JacRed's lastsync pattern,
// dev/instance-sync.md).
func syncCursorPages(ctx context.Context, peer, path, settingKey string, apply func(page json.RawMessage) (nextSince string, hasMore bool, err error)) {
	since, _ := store.GetSetting(ctx, settingKey)
	for {
		u := peer + path + "?since=" + url.QueryEscape(since) + "&limit=500"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			log.Printf("tasks: instance_sync %s: build request: %v", path, err)
			return
		}
		resp, err := syncHTTPClient.Do(req)
		if err != nil {
			log.Printf("tasks: instance_sync %s: %v", path, err)
			return
		}
		body, err := readAndClose(resp)
		if err != nil || resp.StatusCode != http.StatusOK {
			log.Printf("tasks: instance_sync %s: status=%d err=%v", path, resp.StatusCode, err)
			return
		}

		nextSince, hasMore, err := apply(body)
		if err != nil {
			log.Printf("tasks: instance_sync %s: apply page: %v", path, err)
			return
		}
		if nextSince == "" || nextSince == since {
			return // empty page — caught up
		}
		since = nextSince
		store.SetSetting(ctx, settingKey, since)
		if !hasMore {
			return
		}
	}
}

func readAndClose(resp *http.Response) (json.RawMessage, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func pullCards(ctx context.Context, peer string) {
	var applied, failed int
	syncCursorPages(ctx, peer, "/api/sync/cards", "sync_cursor_cards", func(page json.RawMessage) (string, bool, error) {
		var body struct {
			Cards     []store.SyncCard `json:"cards"`
			NextSince string           `json:"next_since"`
			HasMore   bool             `json:"has_more"`
		}
		if err := json.Unmarshal(page, &body); err != nil {
			return "", false, err
		}
		for _, c := range body.Cards {
			if err := store.UpsertSyncedCard(ctx, c); err != nil {
				failed++
				log.Printf("tasks: instance_sync pull cards: upsert %s: %v", c.CardID, err)
				continue
			}
			applied++
		}
		return body.NextSince, body.HasMore, nil
	})
	if applied > 0 || failed > 0 {
		log.Printf("tasks: instance_sync pull cards from %s: applied %d, failed %d", peer, applied, failed)
	}
}

// syncPushPages drives one dataset's push: repeatedly reads local rows newer
// than the persisted cursor (via list), POSTs them under wrapKey to
// peer+path with the given token, and advances+persists the cursor to
// settingKey only after a batch is accepted — same after-not-before
// resilience as syncCursorPages, mirrored for the outbound direction. T must
// carry its own UpdatedAt (store.SyncCard / store.SyncEvent both do); updatedAt
// extracts it since Go generics can't reach a common field across two
// unrelated structs.
func syncPushPages[T any](ctx context.Context, peer, token, path, wrapKey, settingKey string, list func(ctx context.Context, since time.Time, limit int) ([]T, error), updatedAt func(T) time.Time) {
	since, _ := store.GetSetting(ctx, settingKey)
	var sinceTime time.Time
	if since != "" {
		sinceTime, _ = time.Parse(time.RFC3339Nano, since)
	}

	var applied, failed int
	for {
		items, err := list(ctx, sinceTime, 500)
		if err != nil {
			log.Printf("tasks: instance_sync push %s: query: %v", path, err)
			break
		}
		if len(items) == 0 {
			break
		}

		payload, err := json.Marshal(map[string]any{wrapKey: items})
		if err != nil {
			log.Printf("tasks: instance_sync push %s: marshal: %v", path, err)
			break
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, peer+path, strings.NewReader(string(payload)))
		if err != nil {
			log.Printf("tasks: instance_sync push %s: build request: %v", path, err)
			break
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Sync-Token", token)
		resp, err := syncHTTPClient.Do(req)
		if err != nil {
			log.Printf("tasks: instance_sync push %s: %v", path, err)
			break
		}
		body, err := readAndClose(resp)
		if err != nil || resp.StatusCode != http.StatusOK {
			log.Printf("tasks: instance_sync push %s: status=%d err=%v", path, resp.StatusCode, err)
			break
		}
		var result struct{ Applied, Failed int }
		json.Unmarshal(body, &result) //nolint:errcheck
		applied += result.Applied
		failed += result.Failed

		sinceTime = updatedAt(items[len(items)-1])
		since = sinceTime.UTC().Format(time.RFC3339Nano)
		store.SetSetting(ctx, settingKey, since)

		if len(items) < 500 {
			break
		}
	}
	if applied > 0 || failed > 0 {
		log.Printf("tasks: instance_sync push %s to %s: applied %d, failed %d", path, peer, applied, failed)
	}
}

// pushCards sends local cards this instance hasn't pushed yet — see
// syncPushPages and the file-level doc comment in sync_serve.go. This is how
// a card only THIS instance's parser ever found (the "Одиссея" scenario)
// reaches a peer that would otherwise never learn it exists.
func pushCards(ctx context.Context, peer, token string) {
	syncPushPages(ctx, peer, token, "/api/sync/cards", "cards", "sync_push_cursor_cards",
		store.ListCardsSince, func(c store.SyncCard) time.Time { return c.UpdatedAt })
}

// pushEvents sends local play-events this instance hasn't pushed yet — the
// direction pull can't cover when THIS instance has no public address of its
// own (see instance_sync.go doc comment and sync_serve.go).
func pushEvents(ctx context.Context, peer, token string) {
	syncPushPages(ctx, peer, token, "/api/sync/events", "events", "sync_push_cursor_events",
		store.ListPlayEventsSince, func(e store.SyncEvent) time.Time { return e.UpdatedAt })
}

func pullEvents(ctx context.Context, peer string) {
	var applied, failed int
	syncCursorPages(ctx, peer, "/api/sync/events", "sync_cursor_events", func(page json.RawMessage) (string, bool, error) {
		var body struct {
			Events    []store.SyncEvent `json:"events"`
			NextSince string            `json:"next_since"`
			HasMore   bool              `json:"has_more"`
		}
		if err := json.Unmarshal(page, &body); err != nil {
			return "", false, err
		}
		for _, e := range body.Events {
			if err := store.UpsertSyncedPlayEvent(ctx, e); err != nil {
				failed++
				log.Printf("tasks: instance_sync pull events: upsert %s/%s/%s: %v", e.CardID, e.Ident, e.Date, err)
				continue
			}
			applied++
		}
		return body.NextSince, body.HasMore, nil
	})
	if applied > 0 || failed > 0 {
		log.Printf("tasks: instance_sync pull events from %s: applied %d, failed %d", peer, applied, failed)
	}
}
