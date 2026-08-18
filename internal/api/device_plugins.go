package api

import (
	"movies-api/db/store"
	"net/http"
)

// GET /device/plugins?token=&profile_id=
// Called by the lampainit-invc.js bootstrap module on appload() to get the
// list of plugin script URLs to load for this device+profile.
func handleGetDevicePlugins(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	profileID := r.URL.Query().Get("profile_id")
	plugins := store.GetDevicePlugins(r.Context(), d.ID, profileID)
	JSON(w, http.StatusOK, map[string]any{"plugins": plugins})
}
