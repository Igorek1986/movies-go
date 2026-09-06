package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

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

// GET /api/stats/personal/movies?token=&profile_id=&page=&per_page=
// "Фильмов просмотрено" detail list — each card annotated with its last
// known percent and rewatch count (timecodes.view_count), same
// merge-extra-fields-onto-toMediaItem pattern as handleUnwatched.
func handleProfileStatsMovies(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		JSON(w, http.StatusOK, emptyPage(1))
		return
	}
	profileID := r.URL.Query().Get("profile_id")
	page, perPage := statsPageParams(r)

	var movieIDs []string
	for _, id := range store.ListCompletedCardIDs(r.Context(), d.ID, profileID, 0) {
		if isMovieCard(id) {
			movieIDs = append(movieIDs, id)
		}
	}
	if len(movieIDs) == 0 {
		JSON(w, http.StatusOK, emptyPage(page))
		return
	}

	f := store.CategoryFilter{CardIDs: movieIDs, Page: page, PerPage: perPage}
	applyCatalogTrackers(&f)
	rows, total := store.ListCategory(f)
	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}

	details := store.GetMovieWatchDetails(r.Context(), d.ID, profileID, movieIDs)
	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := toMediaItem(row)
		if det, ok := details[cardIDOf(row)]; ok {
			item["percent"] = det.Percent
			item["view_count"] = det.ViewCount
		}
		results = append(results, item)
	}
	JSON(w, http.StatusOK, map[string]any{
		"page":          page,
		"results":       results,
		"total_pages":   totalPages,
		"total_results": total,
	})
}

// GET /api/stats/personal/series?token=&profile_id=&page=&per_page=
// Combined "Смотрю" + "Завершено" TV list — each card annotated with
// watched/total aired-episode counts.
func handleProfileStatsSeries(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		JSON(w, http.StatusOK, emptyPage(1))
		return
	}
	profileID := r.URL.Query().Get("profile_id")
	page, perPage := statsPageParams(r)

	seriesIDs := store.ListWatchingCardIDs(r.Context(), d.ID, profileID, 0)
	for _, id := range store.ListCompletedCardIDs(r.Context(), d.ID, profileID, 0) {
		if !isMovieCard(id) {
			seriesIDs = append(seriesIDs, id)
		}
	}
	if len(seriesIDs) == 0 {
		JSON(w, http.StatusOK, emptyPage(page))
		return
	}

	f := store.CategoryFilter{CardIDs: seriesIDs, Page: page, PerPage: perPage}
	applyCatalogTrackers(&f)
	rows, total := store.ListCategory(f)
	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}

	progress := store.GetSeriesProgressBatch(r.Context(), d.ID, profileID, seriesIDs)
	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := toMediaItem(row)
		if p, ok := progress[cardIDOf(row)]; ok {
			item["watched_episodes"] = p.Watched
			item["total_episodes"] = p.Total
		}
		results = append(results, item)
	}
	JSON(w, http.StatusOK, map[string]any{
		"page":          page,
		"results":       results,
		"total_pages":   totalPages,
		"total_results": total,
	})
}
