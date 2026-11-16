package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"movies-api/db/store"
	"movies-api/internal/cubtop"
)

func handleCUBTop(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	profileID := q.Get("profile_id")
	f := store.CategoryFilter{}
	applyChildFilter(r, &f, profileID)
	applyAdultTextFilter(r, &f, profileID)
	applyHideWatched(r, &f, profileID)
	watched := make(map[string]bool, len(f.WatchedCardIDs))
	for _, id := range f.WatchedCardIDs {
		watched[id] = true
	}
	cards, err := cubtop.Read(r.Context())
	if err != nil {
		Error(w, http.StatusServiceUnavailable, "Топ CUB пока недоступен")
		return
	}
	filtered := make([]json.RawMessage, 0, len(cards))
	search := strings.ToLower(q.Get("search"))
	for _, card := range cards {
		if watched[fmt.Sprintf("%d_%s", card.ID, card.MediaType)] {
			continue
		}
		if q.Get("media_type") != "" && q.Get("media_type") != card.MediaType {
			continue
		}
		text := strings.ToLower(card.Title + " " + card.Name + " " + card.Overview)
		if search != "" && !strings.Contains(text, search) {
			continue
		}
		if q.Get("hide_unrated") == "1" && card.PG <= 0 {
			continue
		}
		if f.Child {
			if card.Adult || card.PG <= 0 || f.ChildAge < 0 || card.PG > f.ChildAge {
				continue
			}
			// CUB charts have no TMDB keywords; fail closed if keyword blocking is enabled.
			if len(f.ChildBlockedKeywords) > 0 {
				continue
			}
		}
		blocked := false
		for _, word := range f.ChildTextKeywords {
			if word != "" && strings.Contains(text, strings.ToLower(word)) {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		filtered = append(filtered, card.Raw)
	}
	total := len(filtered)
	pages := (total + perPage - 1) / perPage
	if pages < 1 {
		pages = 1
	}
	start := total
	if page <= pages {
		start = (page - 1) * perPage
		if start > total {
			start = total
		}
	}
	end := start + perPage
	if end > total {
		end = total
	}
	JSON(w, http.StatusOK, map[string]any{"page": page, "total_pages": pages, "total_results": total, "results": filtered[start:end]})
}
