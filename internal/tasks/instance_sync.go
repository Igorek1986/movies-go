package tasks

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"movies-api/db/store"
)

var syncHTTPClient = &http.Client{Timeout: 20 * time.Second}

// OnSyncApplied is called whenever pullCards or pullTorrents actually wrote
// new local data (applied > 0) — wired in cmd/main.go to
// api.InvalidateCategoryCache, the same hook parser.OnComplete uses after a
// parser run. Without it, a card/torrent that arrived via sync wouldn't show
// up in the catalog until the next local parser run happened to invalidate
// the cache for an unrelated reason. A function var, not a direct import,
// because internal/api already imports internal/tasks (see admin.go), so
// the reverse import would be circular — same pattern as parser.OnComplete.
var OnSyncApplied func()

// StartInstanceSyncLoop runs instance sync's client side (see
// dev/instance-sync.md) on a timer, using the admin-configured
// sync_interval_minutes. A no-op tick if sync_peer_url isn't set — a hub
// (empty peer_url) has nothing to pull or push, it only serves (see
// internal/api/sync_serve.go, always on, no toggle).
//
// Ticks once immediately on start, before the first wait — a freshly
// configured spoke (or one restarting after the app was down) doesn't sit
// idle for a full interval before its first real attempt. If the hub is
// unreachable, this first tick just fails harmlessly (logged, nothing
// applied) and the loop falls back to sync_interval_minutes' stored default
// (15) for the retry, same as any other tick — no special-casing needed,
// since a never-successful pull never overwrites that default (see
// pullCards' interval-learning).
func StartInstanceSyncLoop(ctx context.Context) {
	runTickBounded(ctx)
	for {
		minutes := store.GetSettingInt(ctx, "sync_interval_minutes")
		if minutes < 1 {
			minutes = 15
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(minutes) * time.Minute):
			runTickBounded(ctx)
		}
	}
}

// syncTickTimeout bounds one full tick (pull+push across all datasets) as an
// overall safety net — see syncCallTimeout for the per-dataset budget that
// actually matters day to day. Sized comfortably above 6×syncCallTimeout (9
// minutes) so it only ever bites as defense in depth (e.g. a per-call
// timeout failing to propagate somewhere), never as the normal brake.
const syncTickTimeout = 10 * time.Minute

// syncCallTimeout bounds each individual pull/push call within a tick. Once
// torrents started syncing (2026-09), its one-time historical backlog
// (hundreds of thousands of rows, all sharing the tick's old single deadline)
// ran pullTorrents right up against that shared deadline on every tick,
// which starved pullEvents completely — its GET never even got a chance to
// fire before the shared context was already past its deadline, so
// media_play_events (and "Популярное") saw zero updates for as long as the
// backlog lasted. Giving each call its own slice means a big backlog in one
// dataset can no longer block progress on the others within the same tick.
const syncCallTimeout = 90 * time.Second

func runTickBounded(ctx context.Context) {
	tickCtx, cancel := context.WithTimeout(ctx, syncTickTimeout)
	defer cancel()
	runInstanceSyncTick(tickCtx)
}

// withCallBudget runs fn with its own syncCallTimeout deadline, still capped
// by ctx's own (outer, whole-tick) deadline if that's tighter.
func withCallBudget(ctx context.Context, fn func(context.Context)) {
	callCtx, cancel := context.WithTimeout(ctx, syncCallTimeout)
	defer cancel()
	fn(callCtx)
}

// runInstanceSyncTick does everything there is to do with a peer — no
// separate per-dataset toggles: pull (cards, then torrents, then events —
// cards first so torrents/events have something to attach to; events last
// since they reference card_id via FK) always runs once a peer is set; push
// (same order) additionally requires sync_token — the credential this
// instance presents when pushing, which must match the peer's own
// sync_token (see internal/api/sync_serve.go). Each call gets its own
// syncCallTimeout slice (see its doc comment) instead of sharing one budget
// for the whole tick.
func runInstanceSyncTick(ctx context.Context) {
	peer, _ := store.GetSetting(ctx, "sync_peer_url")
	peer = strings.TrimRight(peer, "/")
	if peer == "" {
		return
	}
	withCallBudget(ctx, func(c context.Context) { pullCards(c, peer) })
	withCallBudget(ctx, func(c context.Context) { pullTorrents(c, peer) })
	withCallBudget(ctx, func(c context.Context) { pullEvents(c, peer) })
	if token, _ := store.GetSetting(ctx, "sync_token"); token != "" {
		withCallBudget(ctx, func(c context.Context) { pushCards(c, peer, token) })
		withCallBudget(ctx, func(c context.Context) { pushTorrents(c, peer, token) })
		withCallBudget(ctx, func(c context.Context) { pushEvents(c, peer, token) })
	}
}

// syncCursorPages drives one dataset's incremental pull: repeatedly GETs
// peer+path?since=cursor&since_tie=tie&limit=..., applying each page via
// apply, advancing and persisting the cursor (settingKey + settingKey+"_tie")
// only after a page's rows were all successfully written locally.
// Persisting after (not before) the write is what gives this natural
// resilience to a mid-page network/write failure — the next tick just
// retries the same page (see JacRed's lastsync pattern, dev/instance-sync.md).
//
// The tie half of the cursor is load-bearing, not an extra nicety — see
// ListCardsSince's comment in db/store/sync.go for why a bare timestamp
// cursor silently drops rows once more than a page's worth share the exact
// same updated_at (a migration backfilling a new column with DEFAULT now()
// does exactly this to every pre-existing row at once).
func syncCursorPages(ctx context.Context, peer, path, settingKey string, apply func(page json.RawMessage, since, sinceTie string) (nextSince, nextTie string, hasMore bool, err error)) {
	since, _ := store.GetSetting(ctx, settingKey)
	sinceTie, _ := store.GetSetting(ctx, settingKey+"_tie")
	for {
		u := peer + path + "?since=" + url.QueryEscape(since) + "&since_tie=" + url.QueryEscape(sinceTie) + "&limit=500"
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

		nextSince, nextTie, hasMore, err := apply(body, since, sinceTie)
		if err != nil {
			log.Printf("tasks: instance_sync %s: apply page: %v", path, err)
			return
		}
		if nextSince == since && nextTie == sinceTie {
			return // no progress — caught up
		}
		since, sinceTie = nextSince, nextTie
		store.SetSetting(ctx, settingKey, since)
		store.SetSetting(ctx, settingKey+"_tie", sinceTie)
		if !hasMore {
			return
		}
	}
}

func readAndClose(resp *http.Response) (json.RawMessage, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// applyPulledPage upserts items in page order and decides where the cursor
// actually lands — the page's own end (pageSince/pageTie/pageHasMore) only if
// every item applied cleanly. On a failure the cursor stops right before the
// first failing item (or, if that's the page's very first item, holds at the
// same (since, sinceTie) the page was fetched with, so syncCursorPages' "no
// progress" check leaves it untouched) instead of jumping to the page's end
// regardless of failures, which is what silently dropped 9 cards for good in
// production (2026-09) — a peer-outage 502 mid-page is what surfaced it, but
// the same jump-to-page-end would just as happily skip a card that fails to
// upsert for its own reason (bad data, a transient DB error) while its
// neighbors in the same page succeed. hasMore is forced false on any
// failure: paging on into further, unrelated new data would spend the rest
// of this tick's fixed page budget (see syncTickTimeout) on a dataset that
// can't fully advance anyway, at the other dataset/push calls' expense.
// Retried every tick until the item succeeds, same as a whole-page failure.
func applyPulledPage[T any](items []T, since, sinceTie, pageSince, pageTie string, pageHasMore bool,
	updatedAt func(T) time.Time, tieOf func(T) string, upsert func(T) error) (nextSince, nextTie string, hasMore bool, applied, failed int) {
	firstFailure := -1
	for i, item := range items {
		if err := upsert(item); err != nil {
			failed++
			if firstFailure == -1 {
				firstFailure = i
			}
			continue
		}
		applied++
	}
	switch {
	case firstFailure == -1:
		return pageSince, pageTie, pageHasMore, applied, failed
	case firstFailure == 0:
		return since, sinceTie, false, applied, failed
	default:
		last := items[firstFailure-1]
		return updatedAt(last).UTC().Format(time.RFC3339Nano), tieOf(last), false, applied, failed
	}
}

func pullCards(ctx context.Context, peer string) {
	var applied, failed int
	var peerName string
	syncCursorPages(ctx, peer, "/api/sync/cards", "sync_cursor_cards", func(page json.RawMessage, since, sinceTie string) (string, string, bool, error) {
		var body struct {
			Cards           []store.SyncCard `json:"cards"`
			NextSince       string           `json:"next_since"`
			NextTie         string           `json:"next_tie"`
			HasMore         bool             `json:"has_more"`
			IntervalMinutes int              `json:"interval_minutes"`
			InstanceName    string           `json:"instance_name"`
		}
		if err := json.Unmarshal(page, &body); err != nil {
			return "", "", false, err
		}
		peerName = body.InstanceName
		// Adopt the hub's advertised interval as our own — the hub's copy of
		// this setting is otherwise unused (its own tick loop never runs
		// with an empty peer_url), so it's the one place this needs
		// configuring; a spoke just follows along (see sync_serve.go).
		if body.IntervalMinutes > 0 {
			if cur := store.GetSettingInt(ctx, "sync_interval_minutes"); cur != body.IntervalMinutes {
				store.SetSetting(ctx, "sync_interval_minutes", strconv.Itoa(body.IntervalMinutes))
			}
		}
		nextSince, nextTie, hasMore, a, f := applyPulledPage(body.Cards, since, sinceTie, body.NextSince, body.NextTie, body.HasMore,
			func(c store.SyncCard) time.Time { return c.UpdatedAt }, func(c store.SyncCard) string { return c.CardID },
			func(c store.SyncCard) error {
				if err := store.UpsertSyncedCard(ctx, c); err != nil {
					log.Printf("tasks: instance_sync pull cards: upsert %s: %v", c.CardID, err)
					return err
				}
				return nil
			})
		applied += a
		failed += f
		return nextSince, nextTie, hasMore, nil
	})
	if applied > 0 || failed > 0 {
		log.Printf("tasks: instance_sync pull cards from %s: applied %d, failed %d", peer, applied, failed)
		store.LogSyncActivity(ctx, "pull", "cards", peerName, peer, applied, failed)
	}
	if applied > 0 && OnSyncApplied != nil {
		OnSyncApplied()
	}
}

// pullTorrents pulls the peer's hash→card_id dedup index — what actually
// makes a card that arrived via pullCards show up in the catalog rather than
// sit hidden as metadata-only (categoryWhere requires EXISTS torrents; see
// SyncTorrent's doc comment in db/store/sync.go).
func pullTorrents(ctx context.Context, peer string) {
	var applied, failed int
	var peerName string
	syncCursorPages(ctx, peer, "/api/sync/torrents", "sync_cursor_torrents", func(page json.RawMessage, since, sinceTie string) (string, string, bool, error) {
		var body struct {
			Torrents     []store.SyncTorrent `json:"torrents"`
			NextSince    string              `json:"next_since"`
			NextTie      string              `json:"next_tie"`
			HasMore      bool                `json:"has_more"`
			InstanceName string              `json:"instance_name"`
		}
		if err := json.Unmarshal(page, &body); err != nil {
			return "", "", false, err
		}
		peerName = body.InstanceName
		nextSince, nextTie, hasMore, a, f := applyPulledPage(body.Torrents, since, sinceTie, body.NextSince, body.NextTie, body.HasMore,
			func(t store.SyncTorrent) time.Time { return t.FirstSeenAt }, func(t store.SyncTorrent) string { return t.Hash },
			func(t store.SyncTorrent) error {
				if err := store.UpsertSyncedTorrent(ctx, t); err != nil {
					log.Printf("tasks: instance_sync pull torrents: upsert %s: %v", t.Hash, err)
					return err
				}
				return nil
			})
		applied += a
		failed += f
		return nextSince, nextTie, hasMore, nil
	})
	if applied > 0 || failed > 0 {
		log.Printf("tasks: instance_sync pull torrents from %s: applied %d, failed %d", peer, applied, failed)
		store.LogSyncActivity(ctx, "pull", "torrents", peerName, peer, applied, failed)
	}
	if applied > 0 && OnSyncApplied != nil {
		OnSyncApplied()
	}
}

// syncPushPages drives one dataset's push: repeatedly reads local rows above
// the persisted cursor (via list, the same tie-aware compound cursor as
// syncCursorPages — see ListCardsSince's comment for why the tie half is
// required, not optional), POSTs them under wrapKey to peer+path with the
// given token, and advances+persists the cursor (settingKey + settingKey+
// "_tie") only after a batch is accepted — same after-not-before resilience
// as syncCursorPages, mirrored for the outbound direction. tieOf extracts
// each item's own tiebreak value since Go generics can't reach a common
// field/method across two unrelated structs.
func syncPushPages[T any](ctx context.Context, peer, token, path, wrapKey, settingKey string, list func(ctx context.Context, since time.Time, sinceTie string, limit int) ([]T, error), updatedAt func(T) time.Time, tieOf func(T) string) {
	since, _ := store.GetSetting(ctx, settingKey)
	sinceTie, _ := store.GetSetting(ctx, settingKey+"_tie")
	var sinceTime time.Time
	if since != "" {
		sinceTime, _ = time.Parse(time.RFC3339Nano, since)
	}

	var applied, failed int
	for {
		items, err := list(ctx, sinceTime, sinceTie, 500)
		if err != nil {
			log.Printf("tasks: instance_sync push %s: query: %v", path, err)
			break
		}
		if len(items) == 0 {
			break
		}

		payload, err := json.Marshal(map[string]any{wrapKey: items, "instance_name": store.GetInstanceName(ctx)})
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
		// Validate the response actually came from our own push handler
		// (applied+failed must account for every item we sent) before
		// trusting it enough to advance the cursor — a 200 with an
		// unrelated/malformed body (e.g. this instance's own SPA fallback
		// page, if the peer's route doesn't exist yet — version skew during
		// a rolling deploy is a real scenario, not just a test artifact)
		// would otherwise unmarshal into a zero-valued result and get
		// treated as "batch accepted", permanently skipping these items.
		var result struct {
			Applied          int  `json:"applied"`
			Failed           int  `json:"failed"`
			FirstFailedIndex *int `json:"first_failed_index"`
		}
		if err := json.Unmarshal(body, &result); err != nil || result.Applied+result.Failed != len(items) {
			log.Printf("tasks: instance_sync push %s: unexpected response (unmarshal err=%v, applied=%d failed=%d for %d items) — not advancing cursor",
				path, err, result.Applied, result.Failed, len(items))
			break
		}
		applied += result.Applied
		failed += result.Failed

		// firstFailed pins the cursor to the last item the peer actually
		// applied, not to the batch's end regardless of failures — the same
		// class of bug applyPulledPage fixes for pull, mirrored here for
		// push (see applyPushBatch's doc comment). Without this, an item
		// that fails to apply on the peer (bad data, a transient DB error —
		// this is exactly how 30 torrents got permanently stuck on one prod
		// pair in 2026-09) gets silently skipped forever: the cursor moves
		// past it and a forward-only cursor never revisits. A nil
		// FirstFailedIndex (peer running pre-fix code, mid rolling deploy)
		// falls back to the old page-end behavior rather than blocking push
		// entirely.
		firstFailed := -1
		if result.FirstFailedIndex != nil {
			firstFailed = *result.FirstFailedIndex
		}
		if firstFailed == 0 {
			// Nothing in this batch actually landed — retry the identical
			// batch next tick instead of looping on it within this one.
			break
		}
		last := items[len(items)-1]
		if firstFailed > 0 {
			last = items[firstFailed-1]
		}
		sinceTime, sinceTie = updatedAt(last), tieOf(last)
		since = sinceTime.UTC().Format(time.RFC3339Nano)
		store.SetSetting(ctx, settingKey, since)
		store.SetSetting(ctx, settingKey+"_tie", sinceTie)

		if firstFailed > 0 || len(items) < 500 {
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
		store.ListCardsSince, func(c store.SyncCard) time.Time { return c.UpdatedAt }, func(c store.SyncCard) string { return c.CardID })
}

// pushTorrents sends local dedup-index rows this instance hasn't pushed yet
// — the "Одиссея" scenario one level down: a card can arrive via pullCards
// fine, but if a peer's own parser is the one that actually holds the
// torrent for it, this is what lets that peer's catalog un-hide it too.
func pushTorrents(ctx context.Context, peer, token string) {
	syncPushPages(ctx, peer, token, "/api/sync/torrents", "torrents", "sync_push_cursor_torrents",
		store.ListTorrentsSince, func(t store.SyncTorrent) time.Time { return t.FirstSeenAt }, func(t store.SyncTorrent) string { return t.Hash })
}

// pushEvents sends local play-events this instance hasn't pushed yet — the
// direction pull can't cover when THIS instance has no public address of its
// own (see instance_sync.go doc comment and sync_serve.go).
func pushEvents(ctx context.Context, peer, token string) {
	syncPushPages(ctx, peer, token, "/api/sync/events", "events", "sync_push_cursor_events",
		store.ListPlayEventsSince, func(e store.SyncEvent) time.Time { return e.UpdatedAt },
		func(e store.SyncEvent) string { return e.CardID + "|" + e.Ident + "|" + e.Date })
}

func pullEvents(ctx context.Context, peer string) {
	var applied, failed int
	var peerName string
	syncCursorPages(ctx, peer, "/api/sync/events", "sync_cursor_events", func(page json.RawMessage, since, sinceTie string) (string, string, bool, error) {
		var body struct {
			Events       []store.SyncEvent `json:"events"`
			NextSince    string            `json:"next_since"`
			NextTie      string            `json:"next_tie"`
			HasMore      bool              `json:"has_more"`
			InstanceName string            `json:"instance_name"`
		}
		if err := json.Unmarshal(page, &body); err != nil {
			return "", "", false, err
		}
		peerName = body.InstanceName
		nextSince, nextTie, hasMore, a, f := applyPulledPage(body.Events, since, sinceTie, body.NextSince, body.NextTie, body.HasMore,
			func(e store.SyncEvent) time.Time { return e.UpdatedAt },
			func(e store.SyncEvent) string { return e.CardID + "|" + e.Ident + "|" + e.Date },
			func(e store.SyncEvent) error {
				if err := store.UpsertSyncedPlayEvent(ctx, e); err != nil {
					log.Printf("tasks: instance_sync pull events: upsert %s/%s/%s: %v", e.CardID, e.Ident, e.Date, err)
					return err
				}
				return nil
			})
		applied += a
		failed += f
		return nextSince, nextTie, hasMore, nil
	})
	if applied > 0 || failed > 0 {
		log.Printf("tasks: instance_sync pull events from %s: applied %d, failed %d", peer, applied, failed)
		store.LogSyncActivity(ctx, "pull", "events", peerName, peer, applied, failed)
	}
}
