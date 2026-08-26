package api

// Web (session-auth) management of Lampa plugin URLs per device, with
// per-profile enable/disable overrides. Consumed by the web UI; the actual
// merged list is served to Lampa via GET /device/plugins (device_plugins.go).

import (
	"encoding/json"
	"fmt"
	"io"
	"movies-api/db/store"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

var pluginCheckClient = &http.Client{Timeout: 6 * time.Second}

// checkPluginURL does a best-effort GET to catch typos/dead links at save
// time — Lampa fetches the URL directly and just silently no-ops on a bad
// one, which is confusing enough to be worth a round-trip here. Returns ""
// on success, a user-facing reason otherwise.
func checkPluginURL(url string) string {
	// Lampac-style template URLs (e.g. "http://{localhost}/my_plugins/actors.js")
	// have their host substituted by Lampac itself at request time — there's
	// nothing reachable from here to check, so skip the round-trip.
	if strings.Contains(url, "{") {
		return ""
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "некорректный URL"
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; movies-go-plugin-check/1.0)")
	resp, err := pluginCheckClient.Do(req)
	if err != nil {
		return "не удалось загрузить URL: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("сервер вернул статус %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	head := strings.TrimSpace(strings.ToLower(string(body)))
	if strings.HasPrefix(head, "<!doctype") || strings.HasPrefix(head, "<html") {
		return "по ссылке HTML-страница, а не JS-файл"
	}
	return ""
}

// GET /api/devices/{id}/plugins
func handleWebListDevicePlugins(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	deviceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	plugins := store.ListDevicePlugins(r.Context(), deviceID)
	if plugins == nil {
		plugins = []store.DevicePluginRow{}
	}
	JSON(w, http.StatusOK, map[string]any{"plugins": plugins})
}

// POST /api/devices/{id}/plugins
func handleWebAddDevicePlugin(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	deviceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	var req struct {
		URL     string `json:"url"`
		Name    string `json:"name"`
		Enabled *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" || (!strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://")) {
		Error(w, http.StatusBadRequest, "valid http(s) url required")
		return
	}
	if msg := checkPluginURL(req.URL); msg != "" {
		Error(w, http.StatusBadRequest, msg)
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	p, err := store.AddDevicePlugin(r.Context(), deviceID, u.ID, req.URL, strings.TrimSpace(req.Name), enabled)
	if err != nil {
		if strings.Contains(err.Error(), "uq_device_plugins_url") {
			Error(w, http.StatusConflict, "url already added")
		} else {
			Error(w, http.StatusInternalServerError, "db error")
		}
		return
	}
	JSON(w, http.StatusOK, p)
}

// PATCH /api/devices/{id}/plugins/{plugin_id}
func handleWebUpdateDevicePlugin(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	deviceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	pluginID, err := strconv.ParseInt(chi.URLParam(r, "plugin_id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid plugin id")
		return
	}
	var req struct {
		Name    *string `json:"name"`
		URL     *string `json:"url"`
		Enabled *bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.URL != nil {
		trimmed := strings.TrimSpace(*req.URL)
		if trimmed == "" || (!strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://")) {
			Error(w, http.StatusBadRequest, "valid http(s) url required")
			return
		}
		if msg := checkPluginURL(trimmed); msg != "" {
			Error(w, http.StatusBadRequest, msg)
			return
		}
		req.URL = &trimmed
	}
	if err := store.UpdateDevicePlugin(r.Context(), pluginID, deviceID, u.ID, req.Name, req.URL, req.Enabled); err != nil {
		if strings.Contains(err.Error(), "uq_device_plugins_url") {
			Error(w, http.StatusConflict, "url already added")
		} else {
			Error(w, http.StatusInternalServerError, "db error")
		}
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// DELETE /api/devices/{id}/plugins/{plugin_id}
func handleWebDeleteDevicePlugin(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	deviceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	pluginID, err := strconv.ParseInt(chi.URLParam(r, "plugin_id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid plugin id")
		return
	}
	if err := store.DeleteDevicePlugin(r.Context(), pluginID, deviceID, u.ID); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// GET /api/devices/{id}/profiles/{profile_id}/plugins
func handleWebListProfilePlugins(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	deviceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	profileID := chi.URLParam(r, "profile_id")
	if profileID == defaultProfileURLParam {
		profileID = ""
	}
	plugins := store.ListProfilePlugins(r.Context(), deviceID, profileID)
	if plugins == nil {
		plugins = []store.ProfilePluginRow{}
	}
	JSON(w, http.StatusOK, map[string]any{"plugins": plugins})
}

// PUT /api/devices/{id}/profiles/{profile_id}/plugins
// Body: {"url": "...", "enabled": true|false} — upserts an explicit override.
func handleWebSetProfilePluginOverride(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	deviceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	profileID := chi.URLParam(r, "profile_id")
	if profileID == defaultProfileURLParam {
		profileID = ""
	}
	var req struct {
		URL     string  `json:"url"`
		Enabled bool    `json:"enabled"`
		Name    *string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" || (!strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://")) {
		Error(w, http.StatusBadRequest, "valid http(s) url required")
		return
	}
	// Only re-check URLs this profile doesn't already know about — an
	// enable/disable toggle on an existing entry re-sends the same URL and
	// shouldn't pay for (or fail on) a fresh network round-trip.
	known := false
	for _, p := range store.ListProfilePlugins(r.Context(), deviceID, profileID) {
		if p.URL == req.URL {
			known = true
			break
		}
	}
	if !known {
		if msg := checkPluginURL(req.URL); msg != "" {
			Error(w, http.StatusBadRequest, msg)
			return
		}
	}
	if err := store.SetProfilePluginOverride(r.Context(), deviceID, u.ID, profileID, req.URL, req.Enabled, req.Name); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// DELETE /api/devices/{id}/profiles/{profile_id}/plugins?url=...
// Removes the override, reverting the profile to inherit the device-level flag.
func handleWebClearProfilePluginOverride(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	deviceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	profileID := chi.URLParam(r, "profile_id")
	if profileID == defaultProfileURLParam {
		profileID = ""
	}
	url := r.URL.Query().Get("url")
	if url == "" {
		Error(w, http.StatusBadRequest, "url required")
		return
	}
	if err := store.ClearProfilePluginOverride(r.Context(), deviceID, u.ID, profileID, url); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
