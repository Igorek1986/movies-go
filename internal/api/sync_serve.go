package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"movies-api/db/store"
)

// This file is the "serve" side of instance-to-instance sync — see
// dev/instance-sync.md. GET always answers, for anyone (no auth, same trust
// level as /np_popular and /api/view) — read-only, harmless if abused (worst
// case someone scrapes the catalog, same as /np_popular today). POST is
// different: it WRITES into media_cards/media_play_events, so it's gated by
// sync_token instead of being open — this is the path an instance with no
// public address of its own uses to push its own finds out to a reachable
// peer, since it can make outbound requests even though nobody can pull
// from it. The "pull"/"push" client side lives in internal/tasks/instance_sync.go.

const syncPageDefaultLimit = 500
const syncPageMaxLimit = 2000

// parseSyncSinceLimit reads the incremental-pull cursor: since (timestamp)
// plus sinceTie, the tiebreaker needed when multiple rows share the exact
// same updated_at (see ListCardsSince's comment) — without it, pagination
// silently drops every row past the first page that lands on a shared
// boundary timestamp.
func parseSyncSinceLimit(r *http.Request) (since time.Time, sinceTie string, limit int) {
	if s := r.URL.Query().Get("since"); s != "" {
		since, _ = time.Parse(time.RFC3339, s)
	}
	sinceTie = r.URL.Query().Get("since_tie")
	limit = syncPageDefaultLimit
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= syncPageMaxLimit {
		limit = l
	}
	return since, sinceTie, limit
}

// syncAdvertisedInterval returns this instance's own sync_interval_minutes —
// meaningless to a hub's own tick loop (it never pulls/pushes with an empty
// peer_url), so it exists purely to be advertised here: a spoke pulling from
// this instance adopts it as its own interval (see pullCards), so the
// interval only needs configuring in one place — the hub's admin page.
func syncAdvertisedInterval(ctx context.Context) int {
	interval := store.GetSettingInt(ctx, "sync_interval_minutes")
	if interval < 1 {
		interval = 15
	}
	return interval
}

// GET /api/sync/cards?since=<RFC3339>&since_tie=<card_id>&limit=500
func handleSyncCards(w http.ResponseWriter, r *http.Request) {
	since, sinceTie, limit := parseSyncSinceLimit(r)
	cards, err := store.ListCardsSince(r.Context(), since, sinceTie, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, "query failed")
		return
	}
	nextSince, nextTie := since, sinceTie
	if len(cards) > 0 {
		last := cards[len(cards)-1]
		nextSince, nextTie = last.UpdatedAt, last.CardID
	}
	JSON(w, http.StatusOK, map[string]any{
		"cards":            cards,
		"next_since":       nextSince.UTC().Format(time.RFC3339Nano),
		"next_tie":         nextTie,
		"has_more":         len(cards) == limit,
		"interval_minutes": syncAdvertisedInterval(r.Context()),
		"instance_name":    store.GetInstanceName(r.Context()),
	})
}

// GET /api/sync/events?since=<RFC3339>&since_tie=<card_id|ident|date>&limit=500
func handleSyncEvents(w http.ResponseWriter, r *http.Request) {
	since, sinceTie, limit := parseSyncSinceLimit(r)
	events, err := store.ListPlayEventsSince(r.Context(), since, sinceTie, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, "query failed")
		return
	}
	nextSince, nextTie := since, sinceTie
	if len(events) > 0 {
		last := events[len(events)-1]
		nextSince, nextTie = last.UpdatedAt, last.CardID+"|"+last.Ident+"|"+last.Date
	}
	JSON(w, http.StatusOK, map[string]any{
		"events":           events,
		"next_since":       nextSince.UTC().Format(time.RFC3339Nano),
		"next_tie":         nextTie,
		"has_more":         len(events) == limit,
		"interval_minutes": syncAdvertisedInterval(r.Context()),
		"instance_name":    store.GetInstanceName(r.Context()),
	})
}

// GET /api/sync/torrents?since=<RFC3339>&since_tie=<hash>&limit=500
func handleSyncTorrents(w http.ResponseWriter, r *http.Request) {
	since, sinceTie, limit := parseSyncSinceLimit(r)
	torrents, err := store.ListTorrentsSince(r.Context(), since, sinceTie, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, "query failed")
		return
	}
	nextSince, nextTie := since, sinceTie
	if len(torrents) > 0 {
		last := torrents[len(torrents)-1]
		nextSince, nextTie = last.FirstSeenAt, last.Hash
	}
	JSON(w, http.StatusOK, map[string]any{
		"torrents":         torrents,
		"next_since":       nextSince.UTC().Format(time.RFC3339Nano),
		"next_tie":         nextTie,
		"has_more":         len(torrents) == limit,
		"interval_minutes": syncAdvertisedInterval(r.Context()),
		"instance_name":    store.GetInstanceName(r.Context()),
	})
}

// applyPushBatch applies each item in order via apply, returning applied/failed
// counts plus the 0-based index of the first item that failed (-1 if none).
// The pushing side reports this back so its own cursor can stop right there
// instead of at the batch's last item regardless of failures — see
// internal/tasks/instance_sync.go's syncPushPages doc comment.
func applyPushBatch[T any](items []T, apply func(T) error) (applied, failed, firstFailed int) {
	firstFailed = -1
	for i, item := range items {
		if err := apply(item); err != nil {
			failed++
			if firstFailed == -1 {
				firstFailed = i
			}
			continue
		}
		applied++
	}
	return applied, failed, firstFailed
}

var errPushMissingKey = errors.New("missing required key field")

// syncPushTokenValid checks the X-Sync-Token header against sync_token. An
// unset sync_token means this instance never accepts pushes — the safe
// default (nobody can push in until you set/generate a token).
func syncPushTokenValid(r *http.Request) bool {
	provided := r.Header.Get("X-Sync-Token")
	if provided == "" {
		return false
	}
	stored, _ := store.GetSetting(r.Context(), "sync_token")
	if stored == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(stored)) == 1
}

// POST /api/sync/cards — push path (see file comment above). Body:
// {"cards": [<store.SyncCard>, ...]} — same shape GET /api/sync/cards returns,
// so a spoke pushing its own local finds (including cards this instance has
// never seen) uses the exact same merge as the pull side (UpsertSyncedCard):
// fills only what's empty locally, GREATEST for quality, inserts if unknown.
func handleSyncCardsPush(w http.ResponseWriter, r *http.Request) {
	if !syncPushTokenValid(r) {
		Error(w, http.StatusForbidden, "invalid or missing sync token")
		return
	}
	var body struct {
		InstanceName string           `json:"instance_name"`
		Cards        []store.SyncCard `json:"cards"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}
	applied, failed, firstFailed := applyPushBatch(body.Cards, func(c store.SyncCard) error {
		if c.CardID == "" {
			return errPushMissingKey
		}
		return store.UpsertSyncedCard(r.Context(), c)
	})
	if applied > 0 || failed > 0 {
		store.LogSyncActivity(r.Context(), "push_in", "cards", body.InstanceName, "", applied, failed)
	}
	JSON(w, http.StatusOK, map[string]int{"applied": applied, "failed": failed, "first_failed_index": firstFailed})
}

// POST /api/sync/torrents — push path (see file comment above). Body:
// {"torrents": [<store.SyncTorrent>, ...]} — same shape GET /api/sync/torrents
// returns.
func handleSyncTorrentsPush(w http.ResponseWriter, r *http.Request) {
	if !syncPushTokenValid(r) {
		Error(w, http.StatusForbidden, "invalid or missing sync token")
		return
	}
	var body struct {
		InstanceName string              `json:"instance_name"`
		Torrents     []store.SyncTorrent `json:"torrents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}
	applied, failed, firstFailed := applyPushBatch(body.Torrents, func(t store.SyncTorrent) error {
		if t.Hash == "" || t.CardID == "" {
			return errPushMissingKey
		}
		return store.UpsertSyncedTorrent(r.Context(), t)
	})
	if applied > 0 || failed > 0 {
		store.LogSyncActivity(r.Context(), "push_in", "torrents", body.InstanceName, "", applied, failed)
	}
	JSON(w, http.StatusOK, map[string]int{"applied": applied, "failed": failed, "first_failed_index": firstFailed})
}

// POST /api/sync/events — push path (see file comment above). Body:
// {"events": [{"card_id":"...","ident":"...","date":"2026-01-15","max_percent":85}, ...]}
func handleSyncEventsPush(w http.ResponseWriter, r *http.Request) {
	if !syncPushTokenValid(r) {
		Error(w, http.StatusForbidden, "invalid or missing sync token")
		return
	}
	var body struct {
		InstanceName string            `json:"instance_name"`
		Events       []store.SyncEvent `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}
	applied, failed, firstFailed := applyPushBatch(body.Events, func(e store.SyncEvent) error {
		if e.CardID == "" || e.Ident == "" || e.Date == "" {
			return errPushMissingKey
		}
		return store.UpsertSyncedPlayEvent(r.Context(), e)
	})
	if applied > 0 || failed > 0 {
		store.LogSyncActivity(r.Context(), "push_in", "events", body.InstanceName, "", applied, failed)
	}
	JSON(w, http.StatusOK, map[string]int{"applied": applied, "failed": failed, "first_failed_index": firstFailed})
}
