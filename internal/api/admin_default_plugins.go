package api

// Admin management of the default Lampa plugin set applied to newly created
// devices (store.SeedDefaultDevicePlugins) and backfilled to devices with no
// plugins at all (see the migration in schema.sql). Empty on a fresh clone —
// deliberately not shipped with any hardcoded plugin URLs.

import (
	"encoding/json"
	"movies-api/db/store"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// GET /api/admin/default-plugins
func handleAPIAdminDefaultPluginsGet(w http.ResponseWriter, r *http.Request) {
	plugins := store.ListDefaultDevicePlugins(r.Context())
	if plugins == nil {
		plugins = []store.DevicePluginRow{}
	}
	JSON(w, http.StatusOK, plugins)
}

// POST /api/admin/default-plugins  {"url": "...", "name": "...", "enabled": true}
func handleAPIAdminDefaultPluginsAdd(w http.ResponseWriter, r *http.Request) {
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
	p, err := store.AddDefaultDevicePlugin(r.Context(), req.URL, strings.TrimSpace(req.Name), enabled)
	if err != nil {
		if strings.Contains(err.Error(), "default_device_plugins_url_key") {
			Error(w, http.StatusConflict, "url already added")
		} else {
			Error(w, http.StatusInternalServerError, "db error")
		}
		return
	}
	JSON(w, http.StatusOK, p)
}

// PATCH /api/admin/default-plugins/{plugin_id}
func handleAPIAdminDefaultPluginsUpdate(w http.ResponseWriter, r *http.Request) {
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
	if err := store.UpdateDefaultDevicePlugin(r.Context(), pluginID, req.Name, req.URL, req.Enabled); err != nil {
		if strings.Contains(err.Error(), "default_device_plugins_url_key") {
			Error(w, http.StatusConflict, "url already added")
		} else {
			Error(w, http.StatusInternalServerError, "db error")
		}
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// DELETE /api/admin/default-plugins/{plugin_id}
func handleAPIAdminDefaultPluginsDelete(w http.ResponseWriter, r *http.Request) {
	pluginID, err := strconv.ParseInt(chi.URLParam(r, "plugin_id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid plugin id")
		return
	}
	if err := store.DeleteDefaultDevicePlugin(r.Context(), pluginID); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
