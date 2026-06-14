package api

import (
	"context"
	"encoding/json"
	"movies-api/db/models"
	"movies-api/db/store"
	"movies-api/internal/myshows"
	"movies-api/movies/tmdb"
	"net/http"
	"strconv"
	"time"
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

// ─── Lazy TMDB enrichment ─────────────────────────────────────────────────────

// enrichMissingMyshowsCards дозагружает из TMDB карточки, которые есть в списке
// MyShows, но отсутствуют в media_cards (пустой title/name). Найденные карточки
// сохраняются через UpsertMediaCard, поэтому следующий запрос отдаётся уже из БД.
// Торренты при этом не создаются — такая карточка скрыта из Каталога (см.
// guard EXISTS torrents в categoryWhere) до появления реальной раздачи.
// Возвращает true, если хотя бы одна карточка была сохранена (нужно перечитать).
func enrichMissingMyshowsCards(cards []store.MyshowsCard) bool {
	enriched := false
	for _, c := range cards {
		if c.TmdbID <= 0 || c.Title != "" || c.Name != "" {
			continue
		}
		isMovie := c.MediaType == "movie"
		ent := tmdb.GetVideoDetails(isMovie, c.TmdbID)
		if ent == nil {
			continue
		}
		ent.MediaType = c.MediaType // authoritative type from myshows_items
		store.UpsertMediaCard(ent, &models.TorrentDetails{})
		enriched = true
	}
	return enriched
}

// myshowsEnsureCards дозагружает пустые карточки и, если что-то сохранилось,
// перечитывает список через reread для единообразного форматирования.
func myshowsEnsureCards(cards []store.MyshowsCard, total int,
	reread func() ([]store.MyshowsCard, int, error)) ([]store.MyshowsCard, int) {
	if enrichMissingMyshowsCards(cards) {
		if c2, t2, err := reread(); err == nil {
			return c2, t2
		}
	}
	return cards, total
}

// myshowsEnsureStatusCards — вариант для GetStatusPage (с totalPages).
func myshowsEnsureStatusCards(cards []store.MyshowsCard, total, totalPages int,
	reread func() ([]store.MyshowsCard, int, int, error)) ([]store.MyshowsCard, int, int) {
	if enrichMissingMyshowsCards(cards) {
		if c2, t2, tp2, err := reread(); err == nil {
			return c2, t2, tp2
		}
	}
	return cards, total, totalPages
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
	cards, total = myshowsEnsureCards(cards, total, func() ([]store.MyshowsCard, int, error) {
		return store.GetWatching(r.Context(), d.ID, profileID)
	})
	JSON(w, http.StatusOK, map[string]any{
		"results":       nilSlice(cards),
		"page":          1,
		"total_pages":   1,
		"total_results": total,
	})
}

// ─── Profile shows ────────────────────────────────────────────────────────────

func handleMyshowsProfileShowsGet(w http.ResponseWriter, r *http.Request) {
	d, profileID := myshowsDeviceAuth(w, r)
	if d == nil {
		return
	}
	cards, total, err := store.GetProfileShows(r.Context(), d.ID, profileID)
	if err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	cards, total = myshowsEnsureCards(cards, total, func() ([]store.MyshowsCard, int, error) {
		return store.GetProfileShows(r.Context(), d.ID, profileID)
	})
	JSON(w, http.StatusOK, map[string]any{
		"results":       nilSlice(cards),
		"page":          1,
		"total_pages":   1,
		"total_results": total,
	})
}

func handleMyshowsProfileShowsPost(w http.ResponseWriter, r *http.Request) {
	d, profileID := myshowsDeviceAuth(w, r)
	if d == nil {
		return
	}
	var reqs []myshowsStatusReq
	if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := store.UpsertProfileShows(r.Context(), d.ID, profileID, toStoreItems(reqs)); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
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
	cards, total = myshowsEnsureCards(cards, total, func() ([]store.MyshowsCard, int, error) {
		return store.GetWatching(r.Context(), d.ID, profileID)
	})
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
		cards, total, totalPages = myshowsEnsureStatusCards(cards, total, totalPages, func() ([]store.MyshowsCard, int, int, error) {
			return store.GetStatusPage(r.Context(), d.ID, profileID, cacheType, page)
		})
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
		cards, total, totalPages = myshowsEnsureStatusCards(cards, total, totalPages, func() ([]store.MyshowsCard, int, int, error) {
			return store.GetStatusPage(r.Context(), d.ID, profileID, cacheType, page)
		})
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

// ─── Timetable ────────────────────────────────────────────────────────────────

type timetableReqItem struct {
	TmdbID    int64 `json:"tmdb_id"`
	MyshowsID int   `json:"myshows_id"`
}

// handleMyshowsTimetablePost принимает список непросмотренных сериалов
// [{tmdb_id, myshows_id}] и возвращает {episodes:[...]} с будущими сериями.
//
// Без ?sync=1 — быстрый read из БД (фаза 1, мгновенный показ из прогретого кэша).
// С ?sync=1 — фаза 2: принудительно перетягивает эпизоды всех сериалов из MyShows
// (холодный долив + свежие серии для уже известных шоу), затем возвращает.
func handleMyshowsTimetablePost(w http.ResponseWriter, r *http.Request) {
	d, _ := myshowsDeviceAuth(w, r)
	if d == nil {
		return
	}
	var reqs []timetableReqItem
	if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}

	doSync := r.URL.Query().Get("sync") == "1"

	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()

	ids := make([]int64, 0, len(reqs))
	for _, req := range reqs {
		if req.TmdbID <= 0 {
			continue
		}
		ids = append(ids, req.TmdbID)
		if doSync && req.MyshowsID > 0 {
			myshows.SyncEpisodesByID(ctx, req.TmdbID, req.MyshowsID, true) //nolint:errcheck
		}
	}

	eps, err := store.GetTimetableByTmdbIDs(ctx, ids)
	if err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]any{"episodes": nilTimetable(eps)})
}

func nilTimetable(eps []store.TimetableEpisode) []store.TimetableEpisode {
	if eps == nil {
		return []store.TimetableEpisode{}
	}
	return eps
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
