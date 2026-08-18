package api

// Web (session-auth) management of Lampa plugin URLs per device, with
// per-profile enable/disable overrides. Consumed by the web UI; the actual
// merged list is served to Lampa via GET /device/plugins (device_plugins.go).

import (
	"encoding/json"
	"movies-api/db/store"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		Error(w, http.StatusBadRequest, "url required")
		return
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
