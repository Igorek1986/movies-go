package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"movies-api/db/store"
)

// GET /api/admin/sync
func handleAPIAdminSyncGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	peerURL, _ := store.GetSetting(ctx, "sync_peer_url")
	token, _ := store.GetSetting(ctx, "sync_token")
	interval := store.GetSettingInt(ctx, "sync_interval_minutes")
	if interval < 1 {
		interval = 15
	}

	JSON(w, http.StatusOK, map[string]any{
		"peer_url":         peerURL,
		"token":            token,
		"interval_minutes": interval,
	})
}

// POST /api/admin/sync
// Body: {"peer_url": "...", "token": "...", "interval_minutes": 15} — all
// fields optional, only given ones change. Empty peer_url = this instance is
// the hub; empty token = push neither sent nor accepted (see settings.go).
func handleAPIAdminSyncSave(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PeerURL         *string `json:"peer_url"`
		Token           *string `json:"token"`
		IntervalMinutes *int    `json:"interval_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}

	ctx := context.Background()
	if body.PeerURL != nil {
		store.SetSetting(ctx, "sync_peer_url", *body.PeerURL)
	}
	if body.Token != nil {
		store.SetSetting(ctx, "sync_token", *body.Token)
	}
	if body.IntervalMinutes != nil && *body.IntervalMinutes > 0 {
		store.SetSetting(ctx, "sync_interval_minutes", strconv.Itoa(*body.IntervalMinutes))
	}

	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /api/admin/sync/token — (re)generate sync_token, for the hub side of
// the relationship (peer_url empty) to hand out to its spokes. Clearing the
// token (to revoke) is just a normal save with an empty string.
func handleAPIAdminSyncRotateToken(w http.ResponseWriter, r *http.Request) {
	token := generateAPIKey()
	store.SetSetting(r.Context(), "sync_token", token)
	JSON(w, http.StatusOK, map[string]string{"token": token})
}
