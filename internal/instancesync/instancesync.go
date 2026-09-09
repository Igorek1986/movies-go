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

// pullItemEnabled reports whether this instance is a configured sync client
// AND the given per-item pull toggle (e.g. "sync_pull_runtime") is on.
func pullItemEnabled(ctx context.Context, settingKey string) (gwURL string, ok bool) {
	gw, ok := enabled(ctx)
	if !ok {
		return "", false
	}
	if v, _ := store.GetSetting(ctx, settingKey); v != "1" {
		return "", false
	}
	return gw, true
}

// cardFields mirrors the subset of GET /api/media-card/{card_id}'s JSON this
// package cares about (see internal/api/card.go handleMediaCard).
type cardFields struct {
	Runtime          int    `json:"runtime"`
	EpisodeRunTime   int    `json:"episode_run_time"`
	ImdbID           string `json:"imdb_id"`
	BestVideoQuality int    `json:"best_video_quality"`
	TorrentDate      string `json:"torrent_date"`
	InCatalog        bool   `json:"in_catalog"`
}

// fetchCard GETs gw+"/api/media-card/"+cardID and parses the fields this
// package uses. Returns ok=false on any network/parse error or non-200.
func fetchCard(ctx context.Context, gw, cardID string) (cardFields, bool) {
	var out cardFields
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		gw+"/api/media-card/"+url.PathEscape(cardID), nil)
	if err != nil {
		return out, false
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return out, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return out, false
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, false
	}
	return out, true
}

// FetchRuntime asks the configured gateway for the given card's
// runtime/episode_run_time (whichever applies to isMovie). Returns 0 if this
// instance isn't a configured sync client, the "pull runtime" item is off,
// the gateway has nothing, or the request fails.
func FetchRuntime(ctx context.Context, cardID string, isMovie bool) int {
	gw, ok := pullItemEnabled(ctx, "sync_pull_runtime")
	if !ok {
		return 0
	}
	c, ok := fetchCard(ctx, gw, cardID)
	if !ok {
		return 0
	}
	if isMovie {
		return c.Runtime
	}
	return c.EpisodeRunTime
}

// FetchImdbID asks the configured gateway for the given card's imdb_id.
// Returns "" if the "pull ids" item is off, disabled, or nothing found.
func FetchImdbID(ctx context.Context, cardID string) string {
	gw, ok := pullItemEnabled(ctx, "sync_pull_ids")
	if !ok {
		return ""
	}
	c, ok := fetchCard(ctx, gw, cardID)
	if !ok {
		return ""
	}
	return c.ImdbID
}

// QualitySignal is the gateway's "best I've seen" discovery signal for a
// card — see dev/instance-sync.md: not a promise this instance can itself
// hand out, just a witness that content in this quality was found somewhere,
// as of this date, worth searching for (actual playback lookup is separate —
// Jackett/Jacred-style — so this doesn't have to match what our own
// media_cards.best_video_quality would otherwise say).
type QualitySignal struct {
	BestVideoQuality int
	TorrentDate      string // "2020-01-15", may be ""
}

// FetchQuality asks the configured gateway for the given card's
// best_video_quality/torrent_date. ok=false if the "pull quality" item is
// off or nothing usable was found.
func FetchQuality(ctx context.Context, cardID string) (QualitySignal, bool) {
	gw, ok := pullItemEnabled(ctx, "sync_pull_quality")
	if !ok {
		return QualitySignal{}, false
	}
	c, ok := fetchCard(ctx, gw, cardID)
	if !ok || (c.BestVideoQuality == 0 && c.TorrentDate == "") {
		return QualitySignal{}, false
	}
	return QualitySignal{BestVideoQuality: c.BestVideoQuality, TorrentDate: c.TorrentDate}, true
}

// PopularSourceURL returns the gateway URL to proxy the "Популярное" category
// from, if this instance is a client with the "pull popular" item on — this
// is what consolidates the old standalone popular_source_url setting onto
// the same gateway (see getPopularSourceURL in internal/api/content.go,
// which falls back to popular_source_url when this returns "").
func PopularSourceURL(ctx context.Context) string {
	gw, ok := pullItemEnabled(ctx, "sync_pull_popular")
	if !ok {
		return ""
	}
	return gw
}
