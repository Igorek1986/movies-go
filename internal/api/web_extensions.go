package api

// Web (session-auth) management of the user's web-SPA extension list
// (/profiles → «Расширения»). See scripturl.go for the shared URL-reachability
// check, and web_extensions_rpc.go for the sandboxed-iframe → backend bridge
// these extensions call into at runtime.

import (
	"encoding/json"
	"movies-api/db/store"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// GET /api/extensions
func handleWebListExtensions(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	list := store.ListWebExtensions(r.Context(), u.ID)
	if list == nil {
		list = []store.WebExtensionRow{}
	}
	JSON(w, http.StatusOK, map[string]any{"extensions": list})
}

// POST /api/extensions
func handleWebAddExtension(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
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
	if req.URL == "" || !validScriptURLShape(req.URL) {
		Error(w, http.StatusBadRequest, "valid http(s) or same-origin \"/path\" url required")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	status, code, _ := checkScriptURL(resolveScriptURL(r, req.URL))
	e, err := store.AddWebExtension(r.Context(), u.ID, req.URL, strings.TrimSpace(req.Name), enabled, status, code)
	if err != nil {
		if strings.Contains(err.Error(), "uq_web_extensions_user_url") {
			Error(w, http.StatusConflict, "url already added")
		} else {
			Error(w, http.StatusInternalServerError, "db error")
		}
		return
	}
	broadcastExtensionsChanged(u.ID, r.URL.Query().Get("client_id"))
	JSON(w, http.StatusOK, e)
}

// PATCH /api/extensions/{id}
func handleWebUpdateExtension(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Name    *string `json:"name"`
		Enabled *bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := store.UpdateWebExtension(r.Context(), id, u.ID, req.Name, req.Enabled); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	broadcastExtensionsChanged(u.ID, r.URL.Query().Get("client_id"))
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// DELETE /api/extensions/{id}
func handleWebDeleteExtension(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := store.DeleteWebExtension(r.Context(), id, u.ID); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	broadcastExtensionsChanged(u.ID, r.URL.Query().Get("client_id"))
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// POST /api/extensions/{id}/check
func handleWebCheckExtension(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	url := store.GetWebExtensionURL(r.Context(), id, u.ID)
	if url == "" {
		Error(w, http.StatusNotFound, "not found")
		return
	}
	status, code, reason := checkScriptURL(resolveScriptURL(r, url))
	if err := store.SetWebExtensionStatus(r.Context(), id, u.ID, status, code); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	broadcastExtensionsChanged(u.ID, r.URL.Query().Get("client_id"))
	JSON(w, http.StatusOK, map[string]any{"status": status, "status_code": code, "reason": reason})
}

// broadcastExtensionsChanged nudges every other open tab/device of this
// account to refetch the extension list — same SettingsHub (ws.Hub) already
// used for plugin_settings live-sync (see web_plugin_settings.go), just with
// an explicit "type" so useLiveSync can route it without profile filtering
// (extensions are per-user, not per-profile).
func broadcastExtensionsChanged(userID int64, clientID string) {
	go func() {
		msg, _ := json.Marshal(map[string]any{"type": "extensions_changed"})
		SettingsHub.Broadcast(userID, 0, clientID, msg)
	}()
}
