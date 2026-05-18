package api

import (
	"encoding/json"
	"movies-api/db/store"
	"net/http"
)

// GET /api/plugin-settings?token=&plugin=&lampa_profile_id=
func handleGetPluginSettings(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	plugin := r.URL.Query().Get("plugin")
	if plugin == "" {
		Error(w, http.StatusBadRequest, "plugin required")
		return
	}
	profileID := r.URL.Query().Get("lampa_profile_id")
	data := store.GetPluginSettings(r.Context(), d.UserID, profileID, plugin)
	JSON(w, http.StatusOK, data)
}

// PATCH /api/plugin-settings?token=&plugin=&lampa_profile_id=
// Body: {"key": "...", "value": ...}
func handlePatchPluginSettings(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	plugin := r.URL.Query().Get("plugin")
	if plugin == "" {
		Error(w, http.StatusBadRequest, "plugin required")
		return
	}
	profileID := r.URL.Query().Get("lampa_profile_id")

	var body struct {
		Key   string `json:"key"`
		Value any    `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Key == "" {
		Error(w, http.StatusBadRequest, "key required")
		return
	}

	if err := store.PatchPluginSetting(r.Context(), d.UserID, profileID, plugin, body.Key, body.Value); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	go func() {
		msg, _ := json.Marshal(map[string]any{
			"plugin":           plugin,
			"key":              body.Key,
			"value":            body.Value,
			"lampa_profile_id": profileID,
		})
		SettingsHub.Broadcast(d.UserID, d.ID, msg)
	}()
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
