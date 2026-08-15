package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"movies-api/db/store"
)

// validStatusesFor returns the allowed status values for a card_id's media type
// (derived from the "<tmdb_id>_movie"/"<tmdb_id>_tv" suffix, no DB lookup needed).
// "" (unknown/malformed card_id) allows nothing.
func validStatusesFor(cardID string) map[string]bool {
	m := cardIDRe.FindStringSubmatch(cardID)
	if m == nil {
		return nil
	}
	if m[2] == "movie" {
		return map[string]bool{store.StatusWatched: true, store.StatusPlanned: true, store.StatusNotWatching: true}
	}
	return map[string]bool{store.StatusWatching: true, store.StatusPlanned: true, store.StatusStopped: true, store.StatusNotWatching: true}
}

// GET /timecode/status?token=&profile_id=&card_id=
func handleGetSubjectiveStatus(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}
	cardID := r.URL.Query().Get("card_id")
	if cardID == "" {
		Error(w, http.StatusBadRequest, "card_id required")
		return
	}
	profileID := r.URL.Query().Get("profile_id")
	status := store.GetSubjectiveStatus(r.Context(), d.ID, profileID, cardID)
	JSON(w, http.StatusOK, map[string]string{"status": status})
}

// PUT /timecode/status?token=&profile_id=&card_id=
// Body: {"status": "watching"|"planned"|"stopped"|"watched"|"not_watching"}
func handleSetSubjectiveStatus(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}
	cardID := r.URL.Query().Get("card_id")
	if cardID == "" {
		Error(w, http.StatusBadRequest, "card_id required")
		return
	}
	valid := validStatusesFor(cardID)
	if valid == nil {
		Error(w, http.StatusBadRequest, "invalid card_id")
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !valid[body.Status] {
		Error(w, http.StatusBadRequest, "invalid status for this media type")
		return
	}
	profileID := r.URL.Query().Get("profile_id")
	if err := store.SetSubjectiveStatus(r.Context(), d.ID, profileID, cardID, body.Status); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// DELETE /timecode/status?token=&profile_id=&card_id=
// Clears the explicit status — EnsureImpliedStatus is then free to re-derive one
// from actual watch activity the next time a timecode is saved.
func handleDeleteSubjectiveStatus(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}
	cardID := r.URL.Query().Get("card_id")
	if cardID == "" {
		Error(w, http.StatusBadRequest, "card_id required")
		return
	}
	profileID := r.URL.Query().Get("profile_id")
	store.ClearSubjectiveStatus(r.Context(), d.ID, profileID, cardID)
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// GET /media-library?token=&profile_id=&status=watching&page=&per_page=
// "Моё" — cards with a given subjective status (Смотрю/Буду смотреть/Бросил).
// "watched" (Просмотрел, movies) shares this same endpoint; "История" on the web
// page is the existing /api/history endpoint, not this one — it's factual watch
// data, not a subjective status.
func handleMediaLibrary(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		JSON(w, http.StatusOK, emptyPage(1))
		return
	}
	status := r.URL.Query().Get("status")
	if !map[string]bool{
		store.StatusWatching: true, store.StatusPlanned: true,
		store.StatusStopped: true, "completed": true,
	}[status] {
		Error(w, http.StatusBadRequest, "invalid status")
		return
	}
	profileID := r.URL.Query().Get("profile_id")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	percent, _ := strconv.Atoi(r.URL.Query().Get("percent"))

	var ids []string
	switch status {
	case store.StatusWatching:
		// "Смотрю" excludes shows that finished airing and were fully watched —
		// those surface under "completed" instead (see ListWatchingCardIDs).
		ids = store.ListWatchingCardIDs(r.Context(), d.ID, profileID, percent)
	case "completed":
		ids = store.ListCompletedCardIDs(r.Context(), d.ID, profileID, percent)
	default:
		ids = store.ListCardIDsByStatus(r.Context(), d.ID, profileID, status)
	}
	if len(ids) == 0 {
		JSON(w, http.StatusOK, emptyPage(page))
		return
	}

	f := store.CategoryFilter{CardIDs: ids, Page: page, PerPage: perPage}
	applyCatalogTrackers(&f)
	rows, total := store.ListCategory(f)
	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := toMediaItem(row)
		item["watch_status"] = status
		results = append(results, item)
	}
	JSON(w, http.StatusOK, map[string]any{
		"page":          page,
		"results":       results,
		"total_pages":   totalPages,
		"total_results": total,
	})
}
