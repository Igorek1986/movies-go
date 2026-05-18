package api

import (
	"encoding/json"
	"movies-api/db/store"
	"net/http"
	"strconv"
)

// ─── Request types ────────────────────────────────────────────────────────────

type myshowsStatusReq struct {
	MyshowsID      int     `json:"myshows_id"`
	TmdbID         int64   `json:"tmdb_id"`
	MediaType      string  `json:"media_type"`
	CacheType      string  `json:"cache_type"`
	UnwatchedCount *int    `json:"unwatched_count"`
	NextEpisode    *string `json:"next_episode"`
	ProgressMarker *string `json:"progress_marker"`
}

// Only myshows_id + cache_type (for serial_status / movie_status paths).
type myshowsShortReq struct {
	MyshowsID int    `json:"myshows_id"`
	CacheType string `json:"cache_type"`
}

func toStoreItems(reqs []myshowsStatusReq) []store.MyshowsStatusItem {
	out := make([]store.MyshowsStatusItem, 0, len(reqs))
	for _, r := range reqs {
		if r.MyshowsID == 0 {
			continue
		}
		out = append(out, store.MyshowsStatusItem{
			MyshowsID:      r.MyshowsID,
			TmdbID:         r.TmdbID,
			MediaType:      r.MediaType,
			CacheType:      r.CacheType,
			UnwatchedCount: r.UnwatchedCount,
			NextEpisode:    r.NextEpisode,
			ProgressMarker: r.ProgressMarker,
		})
	}
	return out
}

// ─── Device auth helper ───────────────────────────────────────────────────────

func myshowsDeviceAuth(w http.ResponseWriter, r *http.Request) (*deviceCtx, string) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return nil, ""
	}
	profileID := r.URL.Query().Get("profile_id")
	return d, profileID
}

// ─── Watching ─────────────────────────────────────────────────────────────────

func handleMyshowsWatchingGet(w http.ResponseWriter, r *http.Request) {
	d, profileID := myshowsDeviceAuth(w, r)
	if d == nil {
		return
	}
	cards, total, err := store.GetWatching(r.Context(), d.ID, profileID)
	if err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]any{
		"results":       nilSlice(cards),
		"page":          1,
		"total_pages":   1,
		"total_results": total,
	})
}

func handleMyshowsWatchingPost(w http.ResponseWriter, r *http.Request) {
	d, profileID := myshowsDeviceAuth(w, r)
	if d == nil {
		return
	}
	var reqs []myshowsStatusReq
	if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := store.UpsertWatching(r.Context(), d.ID, profileID, toStoreItems(reqs)); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	cards, total, err := store.GetWatching(r.Context(), d.ID, profileID)
	if err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]any{
		"results":       nilSlice(cards),
		"page":          1,
		"total_pages":   1,
		"total_results": total,
	})
}

// ─── Watchlist / Watched / Cancelled ─────────────────────────────────────────

func handleMyshowsStatusGet(cacheType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d, profileID := myshowsDeviceAuth(w, r)
		if d == nil {
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		cards, total, totalPages, err := store.GetStatusPage(r.Context(), d.ID, profileID, cacheType, page)
		if err != nil {
			Error(w, http.StatusInternalServerError, "db error")
			return
		}
		JSON(w, http.StatusOK, map[string]any{
			"results":       nilSlice(cards),
			"page":          page,
			"total_pages":   totalPages,
			"total_results": total,
		})
	}
}

func handleMyshowsStatusPost(cacheType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d, profileID := myshowsDeviceAuth(w, r)
		if d == nil {
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		var reqs []myshowsStatusReq
		if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
			Error(w, http.StatusBadRequest, "invalid json")
			return
		}
		if err := store.UpsertStatus(r.Context(), d.ID, profileID, cacheType, toStoreItems(reqs)); err != nil {
			Error(w, http.StatusInternalServerError, "db error")
			return
		}
		cards, total, totalPages, err := store.GetStatusPage(r.Context(), d.ID, profileID, cacheType, page)
		if err != nil {
			Error(w, http.StatusInternalServerError, "db error")
			return
		}
		JSON(w, http.StatusOK, map[string]any{
			"results":       nilSlice(cards),
			"page":          page,
			"total_pages":   totalPages,
			"total_results": total,
		})
	}
}

// ─── Serial / Movie status (only myshows_id + cache_type) ────────────────────

func handleMyshowsSerialStatus(w http.ResponseWriter, r *http.Request) {
	myshowsAggStatusPost(w, r)
}

func handleMyshowsMovieStatus(w http.ResponseWriter, r *http.Request) {
	myshowsAggStatusPost(w, r)
}

func myshowsAggStatusPost(w http.ResponseWriter, r *http.Request) {
	d, profileID := myshowsDeviceAuth(w, r)
	if d == nil {
		return
	}
	var reqs []myshowsShortReq
	if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	items := make([]store.MyshowsStatusItem, 0, len(reqs))
	for _, req := range reqs {
		if req.MyshowsID == 0 || req.CacheType == "" {
			continue
		}
		items = append(items, store.MyshowsStatusItem{
			MyshowsID: req.MyshowsID,
			CacheType: req.CacheType,
		})
	}
	if err := store.UpsertStatusByMyshowsID(r.Context(), d.ID, profileID, items); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ─── Single-item set_status / status ─────────────────────────────────────────

func handleMyshowsSetStatus(w http.ResponseWriter, r *http.Request) {
	d, profileID := myshowsDeviceAuth(w, r)
	if d == nil {
		return
	}
	var req myshowsStatusReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.MyshowsID == 0 || req.CacheType == "" {
		Error(w, http.StatusBadRequest, "myshows_id and cache_type required")
		return
	}
	err := store.SetSingleStatus(r.Context(), d.ID, profileID, store.MyshowsStatusItem{
		MyshowsID:      req.MyshowsID,
		TmdbID:         req.TmdbID,
		MediaType:      req.MediaType,
		CacheType:      req.CacheType,
		UnwatchedCount: req.UnwatchedCount,
		NextEpisode:    req.NextEpisode,
		ProgressMarker: req.ProgressMarker,
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func handleMyshowsGetStatus(w http.ResponseWriter, r *http.Request) {
	d, profileID := myshowsDeviceAuth(w, r)
	if d == nil {
		return
	}
	tmdbID, _ := strconv.ParseInt(r.URL.Query().Get("tmdb_id"), 10, 64)
	mediaType := r.URL.Query().Get("media_type")
	if tmdbID == 0 {
		Error(w, http.StatusBadRequest, "tmdb_id required")
		return
	}
	ct := store.GetSingleStatus(r.Context(), d.ID, profileID, tmdbID, mediaType)
	JSON(w, http.StatusOK, map[string]any{"cache_type": nilStr(ct)})
}

// ─── Utils ────────────────────────────────────────────────────────────────────

func nilSlice[T any](s []T) any {
	if s == nil {
		return []T{}
	}
	return s
}

func nilStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
