package api

import (
	"crypto/subtle"
	"encoding/json"
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

func parseSyncSinceLimit(r *http.Request) (since time.Time, limit int) {
	if s := r.URL.Query().Get("since"); s != "" {
		since, _ = time.Parse(time.RFC3339, s)
	}
	limit = syncPageDefaultLimit
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= syncPageMaxLimit {
		limit = l
	}
	return since, limit
}

// GET /api/sync/cards?since=<RFC3339>&limit=500
func handleSyncCards(w http.ResponseWriter, r *http.Request) {
	since, limit := parseSyncSinceLimit(r)
	cards, err := store.ListCardsSince(r.Context(), since, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, "query failed")
		return
	}
	nextSince := since
	if len(cards) > 0 {
		nextSince = cards[len(cards)-1].UpdatedAt
	}
	JSON(w, http.StatusOK, map[string]any{
		"cards":      cards,
		"next_since": nextSince.UTC().Format(time.RFC3339Nano),
		"has_more":   len(cards) == limit,
	})
}

// GET /api/sync/events?since=<RFC3339>&limit=500
func handleSyncEvents(w http.ResponseWriter, r *http.Request) {
	since, limit := parseSyncSinceLimit(r)
	events, err := store.ListPlayEventsSince(r.Context(), since, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, "query failed")
		return
	}
	nextSince := since
	if len(events) > 0 {
		nextSince = events[len(events)-1].UpdatedAt
	}
	JSON(w, http.StatusOK, map[string]any{
		"events":     events,
		"next_since": nextSince.UTC().Format(time.RFC3339Nano),
		"has_more":   len(events) == limit,
	})
}

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
		Cards []store.SyncCard `json:"cards"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}
	var applied, failed int
	for _, c := range body.Cards {
		if c.CardID == "" {
			failed++
			continue
		}
		if err := store.UpsertSyncedCard(r.Context(), c); err != nil {
			failed++
			continue
		}
		applied++
	}
	JSON(w, http.StatusOK, map[string]int{"applied": applied, "failed": failed})
}

// POST /api/sync/events — push path (see file comment above). Body:
// {"events": [{"card_id":"...","ident":"...","date":"2026-01-15","max_percent":85}, ...]}
func handleSyncEventsPush(w http.ResponseWriter, r *http.Request) {
	if !syncPushTokenValid(r) {
		Error(w, http.StatusForbidden, "invalid or missing sync token")
		return
	}
	var body struct {
		Events []store.SyncEvent `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}
	var applied, failed int
	for _, e := range body.Events {
		if e.CardID == "" || e.Ident == "" || e.Date == "" {
			failed++
			continue
		}
		if err := store.UpsertSyncedPlayEvent(r.Context(), e); err != nil {
			failed++
			continue
		}
		applied++
	}
	JSON(w, http.StatusOK, map[string]int{"applied": applied, "failed": failed})
}
