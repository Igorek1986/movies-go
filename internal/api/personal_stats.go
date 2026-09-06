package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"movies-api/db/store"
)

// isMovieCard/cardIDOf mirror the same tiny helpers store.isMovieCardID and
// handleUnwatched's inline fmt.Sprintf use — kept local since they're
// api-package-only (store.isMovieCardID is unexported).
func isMovieCard(cardID string) bool {
	return strings.HasSuffix(cardID, "_movie")
}

func cardIDOf(row store.MediaRow) string {
	return fmt.Sprintf("%d_%s", row.TmdbID, row.MediaType)
}

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

func statsPageParams(r *http.Request) (page, perPage int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ = strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 100 {
		perPage = 60
	}
	return
}

// GET /api/stats/personal/list?token=&profile_id=&kind=&page=&per_page=
// Backs every clickable tile on "Статистика" with one endpoint instead of
// one route per tile — kind selects which card_id set + which extra
// per-card fields (if any) get merged onto toMediaItem's output, same
// merge-extra-fields pattern as handleUnwatched (content.go).
//
//	movies         — "Фильмов просмотрено": percent + view_count (rewatch count)
//	series         — "Сериалов завершено"/"смотрю сейчас" combined: watched/total episodes
//	planned_movies — "Буду смотреть — фильмы"
//	planned_series — "Буду смотреть — сериалы"
//	stopped        — "Брошено"
//	favorites      — "В избранном"
func handleProfileStatsList(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		JSON(w, http.StatusOK, emptyPage(1))
		return
	}
	profileID := r.URL.Query().Get("profile_id")
	kind := r.URL.Query().Get("kind")
	page, perPage := statsPageParams(r)

	var cardIDs []string
	switch kind {
	case "movies":
		for _, id := range store.ListCompletedCardIDs(r.Context(), d.ID, profileID, 0) {
			if isMovieCard(id) {
				cardIDs = append(cardIDs, id)
			}
		}
	case "series":
		// ListWatchingCardIDs/ListCompletedCardIDs are independent, each its
		// own non-trivial query (LATERAL joins over episodes) — sequentially
		// they were a big chunk of this endpoint's ~1.2s, same fix as
		// handleAdminStats's own goroutine+WaitGroup pattern.
		var watchingIDs, completedIDs []string
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); watchingIDs = store.ListWatchingCardIDs(r.Context(), d.ID, profileID, 0) }()
		go func() { defer wg.Done(); completedIDs = store.ListCompletedCardIDs(r.Context(), d.ID, profileID, 0) }()
		wg.Wait()
		cardIDs = watchingIDs
		for _, id := range completedIDs {
			if !isMovieCard(id) {
				cardIDs = append(cardIDs, id)
			}
		}
	case "planned_movies":
		for _, id := range store.ListCardIDsByStatus(r.Context(), d.ID, profileID, store.StatusPlanned) {
			if isMovieCard(id) {
				cardIDs = append(cardIDs, id)
			}
		}
	case "planned_series":
		for _, id := range store.ListCardIDsByStatus(r.Context(), d.ID, profileID, store.StatusPlanned) {
			if !isMovieCard(id) {
				cardIDs = append(cardIDs, id)
			}
		}
	case "stopped":
		cardIDs = store.ListCardIDsByStatus(r.Context(), d.ID, profileID, store.StatusStopped)
	case "favorites":
		cardIDs = store.ListFavoriteCardIDs(r.Context(), d.ID, profileID)
	default:
		Error(w, http.StatusBadRequest, "invalid kind")
		return
	}
	if len(cardIDs) == 0 {
		JSON(w, http.StatusOK, emptyPage(page))
		return
	}

	f := store.CategoryFilter{CardIDs: cardIDs, Page: page, PerPage: perPage}
	applyCatalogTrackers(&f)

	// ListCategory (this page's rows) and the per-card detail batch both only
	// need cardIDs, not each other's output — run them side by side instead
	// of back-to-back.
	var rows []store.MediaRow
	var total int
	var movieDetails map[string]store.MovieWatchDetail
	var seriesProgress map[string]store.SeriesProgress
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); rows, total = store.ListCategory(f) }()
	go func() {
		defer wg.Done()
		switch kind {
		case "movies":
			movieDetails = store.GetMovieWatchDetails(r.Context(), d.ID, profileID, cardIDs)
		case "series":
			seriesProgress = store.GetSeriesProgressBatch(r.Context(), d.ID, profileID, cardIDs)
		}
	}()
	wg.Wait()

	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}

	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := toMediaItem(row)
		id := cardIDOf(row)
		if det, ok := movieDetails[id]; ok {
			item["percent"] = det.Percent
			item["view_count"] = det.ViewCount
		}
		if p, ok := seriesProgress[id]; ok {
			item["watched_episodes"] = p.Watched
			item["total_episodes"] = p.Total
		}
		results = append(results, item)
	}

	resp := map[string]any{
		"page":          page,
		"results":       results,
		"total_pages":   totalPages,
		"total_results": total,
	}
	if kind == "series" {
		// Summed over the FULL card_id set (seriesProgress covers every show
		// in the list, not just this page's rows) — a header summary based
		// only on already-loaded pages would grow as the user scrolls
		// through an infinite-scrolled list, which read as "wrong" numbers.
		var watchedEp, totalEp int
		for _, p := range seriesProgress {
			watchedEp += p.Watched
			totalEp += p.Total
		}
		resp["watched_episodes_total"] = watchedEp
		resp["total_episodes_total"] = totalEp
	}
	JSON(w, http.StatusOK, resp)
}
