// Package instancesync implements the "pull" half of instance-to-instance
// sync described in dev/instance-sync.md: a client instance can ask its
// configured gateway for fields it doesn't have locally yet, using the
// gateway's own public, unauthenticated /api/media-card/{card_id} endpoint
// (the exact same one the web app itself uses) — no bespoke protocol.
//
// The "push" half (forwarding play events with player-reported duration to
// the gateway) lives in internal/api/content.go (forwardPlayEvent).
package instancesync

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"movies-api/db/store"
)

var httpClient = &http.Client{Timeout: 8 * time.Second}

// enabled reports whether this instance is configured as a sync client with
// a gateway URL set. gwURL is "" if not.
func enabled(ctx context.Context) (gwURL string, ok bool) {
	role, _ := store.GetSetting(ctx, "sync_role")
	if role != "client" {
		return "", false
	}
	gw, _ := store.GetSetting(ctx, "sync_gateway_url")
	gw = strings.TrimRight(gw, "/")
	if gw == "" {
		return "", false
	}
	return gw, true
}

// FetchRuntime asks the configured gateway for the given card's
// runtime/episode_run_time (whichever applies to isMovie) via its public
// GET /api/media-card/{card_id}. Returns 0 if this instance isn't a
// configured sync client, the gateway has nothing, or the request fails.
func FetchRuntime(ctx context.Context, cardID string, isMovie bool) int {
	gw, ok := enabled(ctx)
	if !ok {
		return 0
	}
	if v, _ := store.GetSetting(ctx, "sync_pull_runtime"); v != "1" {
		return 0
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		gw+"/api/media-card/"+url.PathEscape(cardID), nil)
	if err != nil {
		return 0
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0
	}

	var out struct {
		Runtime        int `json:"runtime"`
		EpisodeRunTime int `json:"episode_run_time"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return 0
	}
	if isMovie {
		return out.Runtime
	}
	return out.EpisodeRunTime
}
