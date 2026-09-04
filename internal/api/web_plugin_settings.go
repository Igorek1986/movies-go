package api

// Web (session-auth) wrappers for plugin_settings — same table/keys np.js and
// np_unwatched.js already sync across Lampa devices via __NMSync
// (handleGetPluginSettings/handlePatchPluginSettings, device-token-auth).
// plugin_settings is keyed by (user_id, profile_id, plugin) — profile-wide
// across every device, not per-device — so writing here reaches Lampa
// immediately with no separate sync step, and vice versa.

import (
	"encoding/json"
	"movies-api/db/store"
	"net/http"
)

// pluginSettingKey mirrors np.js's getProfileKey (and np_unwatched.js's
// getProfileKey): the default "" profile uses the bare key, any other
// profile suffixes it with "_profile_<id>" — that's the literal key name
// Lampa.Storage syncs under, redundant with the profile_id column here but
// needed to read/write the exact same value.
func pluginSettingKey(profileID, baseKey string) string {
	if profileID == "" {
		return baseKey
	}
	return baseKey + "_profile_" + profileID
}

// GET /api/web/plugin-setting?plugin=&key=&profile_id=
func handleWebGetPluginSetting(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	q := r.URL.Query()
	plugin := q.Get("plugin")
	baseKey := q.Get("key")
	if plugin == "" || baseKey == "" {
		Error(w, http.StatusBadRequest, "plugin and key required")
		return
	}
	profileID := q.Get("profile_id")
	data := store.GetPluginSettings(r.Context(), u.ID, profileID, plugin)
	JSON(w, http.StatusOK, map[string]any{"value": data[pluginSettingKey(profileID, baseKey)]})
}

// PATCH /api/web/plugin-setting?plugin=&key=&profile_id=
// Body: {"value": ...}
func handleWebPatchPluginSetting(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	q := r.URL.Query()
	plugin := q.Get("plugin")
	baseKey := q.Get("key")
	if plugin == "" || baseKey == "" {
		Error(w, http.StatusBadRequest, "plugin and key required")
		return
	}
	profileID := q.Get("profile_id")
	var body struct {
		Value any `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}
	fullKey := pluginSettingKey(profileID, baseKey)
	if err := store.PatchPluginSetting(r.Context(), u.ID, profileID, plugin, fullKey, body.Value); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	// Толкаем Lampa-устройствам того же пользователя — тот же SettingsHub,
	// которым обмениваются друг с другом сами Lampa-устройства через
	// handlePatchPluginSettings/__NMSync. deviceID=0 — у веб-сессии нет
	// своего устройства (см. handleWebWS).
	clientID := q.Get("client_id")
	go func() {
		msg, _ := json.Marshal(map[string]any{
			"plugin":     plugin,
			"key":        fullKey,
			"value":      body.Value,
			"profile_id": profileID,
		})
		SettingsHub.Broadcast(u.ID, 0, clientID, msg)
	}()
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
