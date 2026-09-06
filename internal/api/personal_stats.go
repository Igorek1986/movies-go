package api

import (
	"net/http"

	"movies-api/db/store"
)

// GET /api/stats/personal?token=&profile_id=
// Personal viewing stats for "Моя статистика" — token-based like
// /media-library and /api/media-library/search, not session-based.
func handleProfileStats(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}
	profileID := r.URL.Query().Get("profile_id")
	stats := store.GetProfileStats(r.Context(), d.ID, profileID)
	JSON(w, http.StatusOK, stats)
}
