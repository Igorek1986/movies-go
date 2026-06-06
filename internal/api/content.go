package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"movies-api/db/models"
	"movies-api/db/store"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func getPopularSourceURL(ctx context.Context) string {
	v, _ := store.GetSetting(ctx, "popular_source_url")
	return strings.TrimRight(v, "/")
}

func proxyToPopularSource(w http.ResponseWriter, r *http.Request) {
	target := getPopularSourceURL(r.Context()) + "/np_popular"
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		Error(w, http.StatusBadGateway, "popular source unavailable")
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		Error(w, http.StatusBadGateway, "popular source unavailable")
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body) //nolint:errcheck
}

// popularSourceResp is the np_popular payload returned by another instance.
type popularSourceResp struct {
	Page         int               `json:"page"`
	Results      []json.RawMessage `json:"results"`
	TotalPages   int               `json:"total_pages"`
	TotalResults int               `json:"total_results"`
}

// fetchPopularSource pulls one page of the configured external popular source.
func fetchPopularSource(ctx context.Context, page int) (*popularSourceResp, error) {
	src := getPopularSourceURL(ctx)
	if src == "" {
		return nil, fmt.Errorf("popular source not configured")
	}
	url := fmt.Sprintf("%s/np_popular?page=%d", src, page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("popular source status %d", resp.StatusCode)
	}
	var out popularSourceResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// handleAPIAdminPopularSource returns the full external source's popular list
// (admin view), aggregating all pages so the client can sort/filter locally.
func handleAPIAdminPopularSource(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	first, err := fetchPopularSource(ctx, 1)
	if err != nil {
		Error(w, http.StatusBadGateway, "popular source unavailable")
		return
	}
	results := first.Results
	for page := 2; page <= first.TotalPages && page <= 100; page++ {
		next, err := fetchPopularSource(ctx, page)
		if err != nil {
			break // return what we have
		}
		results = append(results, next.Results...)
	}
	JSON(w, http.StatusOK, map[string]any{
		"source_url":    getPopularSourceURL(r.Context()),
		"results":       results,
		"total_results": first.TotalResults,
	})
}

func forwardPlayEvent(cardID, uid string, pct int) {
	src := getPopularSourceURL(context.Background())
	if src == "" {
		return
	}
	url := fmt.Sprintf("%s/api/view?card_id=%s&uid=%s&percent=%d",
		src,
		cardID, uid, pct,
	)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

func realIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ─── Category route map ───────────────────────────────────────────────────────

// categoryRoutes maps URL path (after stripping lm_ prefix) to a store.CategoryFilter preset.
// Keys must match exactly what lm.js sends: lm_KEY → /KEY
var categoryRoutes = map[string]store.CategoryFilter{
	// Movies — старые (year < currentYear-1), сортировка по release_date
	"movies": {MediaTypes: []string{"movie"}, Categories: []string{models.CatMovie}, Language: "notru", OldOnly: true},
	// Новые фильмы — только последние YearDelta лет, MinVideoQuality+, сортировка по дате торрента
	"movies_new": {MediaTypes: []string{"movie"}, Categories: []string{models.CatMovie}, Language: "notru", NewOnly: true, MinVideoQuality: 200},
	// Русские
	"movies_ru":     {MediaTypes: []string{"movie"}, Categories: []string{models.CatMovie}, Language: "ru", OldOnly: true},
	"movies_ru_new": {MediaTypes: []string{"movie"}, Categories: []string{models.CatMovie}, Language: "ru", NewOnly: true, MinVideoQuality: 200},
	// 4K — yearDelta=4 в NUMParser: новые = 2023–2026, старые = до 2023
	"movies_4k":     {MediaTypes: []string{"movie"}, Categories: []string{models.CatMovie}, MinVideoQuality: 300, OldOnly: true, YearDelta: 4},
	"movies_4k_new": {MediaTypes: []string{"movie"}, Categories: []string{models.CatMovie}, MinVideoQuality: 300, NewOnly: true, YearDelta: 4},
	// Легенды — рейтинговые
	"legends_id": {MediaTypes: []string{"movie"}, Categories: []string{models.CatMovie}, MinVoteCount: 1000, OrderByRating: true},
	// TV — no OldOnly because there is no separate tv_shows_new category
	"tv_shows":    {MediaTypes: []string{"tv"}, Categories: []string{models.CatSeries}, Language: "notru"},
	"tv_shows_ru": {MediaTypes: []string{"tv"}, Categories: []string{models.CatSeries}, Language: "ru"},
	// Cartoons
	"cartoon_movies": {MediaTypes: []string{"movie"}, Categories: []string{models.CatCartoonMovie}},
	"cartoon_series": {MediaTypes: []string{"tv"}, Categories: []string{models.CatCartoonSeries}},
	// Anime
	"anime": {MediaTypes: []string{"tv"}, Categories: []string{models.CatAnime}},

	// Последние поступления — контент за N дней (tracker_new_days), рандом при каждом запросе
	"tracker_new": {RandomOrder: true, RequirePoster: true},

	// Genre collections — random order, all media types
	"genre_comedy":      {Genres: []string{"комедия"}, RandomOrder: true},
	"genre_action":      {Genres: []string{"боевик", "Боевик и Приключения"}, RandomOrder: true},
	"genre_thriller":    {Genres: []string{"триллер"}, RandomOrder: true},
	"genre_crime":       {Genres: []string{"криминал"}, RandomOrder: true},
	"genre_horror":      {Genres: []string{"ужасы"}, RandomOrder: true},
	"genre_romance":     {Genres: []string{"мелодрама"}, RandomOrder: true},
	"genre_adventure":   {Genres: []string{"приключения"}, RandomOrder: true},
	"genre_scifi":       {Genres: []string{"фантастика", "НФ и Фэнтези"}, RandomOrder: true},
	"genre_fantasy":     {Genres: []string{"фэнтези"}, RandomOrder: true},
	"genre_detective":   {Genres: []string{"детектив"}, RandomOrder: true},
	"genre_history":     {Genres: []string{"история"}, RandomOrder: true},
	"genre_war":         {Genres: []string{"военный", "Война и Политика"}, RandomOrder: true},
	"genre_documentary": {Genres: []string{"документальный"}, RandomOrder: true},
	"genre_western":     {Genres: []string{"вестерн"}, RandomOrder: true},
	"genre_random":      {RandomOrder: true},
}

// handleCategory handles /{category}?page=&token=&profile_id=&search=
func handleCategory(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimPrefix(r.URL.Path, "/")
	category = strings.SplitN(category, "/", 2)[0]

	ip := realIP(r)
	go store.TrackAPIUser(ip)
	go store.TrackCategoryRequest(category, ip)

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	searchQ := q.Get("search")
	profileID := q.Get("profile_id")

	// ── continues — watch-in-progress ────────────────────────────────────────
	if category == "continues" || strings.HasPrefix(category, "continues_") {
		handleContinues(w, r, category, profileID, page, perPage)
		return
	}

	// ── np_popular ────────────────────────────────────────────────────────────
	if category == "np_popular" {
		if getPopularSourceURL(r.Context()) != "" {
			proxyToPopularSource(w, r)
		} else {
			handlePopular(w, r, page, perPage, searchQ)
		}
		return
	}

	// ── actor_{person_id} ────────────────────────────────────────────────────
	if strings.HasPrefix(category, "actor_") {
		personID, err := strconv.ParseInt(strings.TrimPrefix(category, "actor_"), 10, 64)
		if err != nil || personID <= 0 {
			http.NotFound(w, r)
			return
		}
		rows, total := store.ListActorCategory(r.Context(), personID, page, perPage)
		sendCategoryResponse(w, rows, total, page, perPage)
		return
	}

	// ── director_{person_id} ─────────────────────────────────────────────────
	if strings.HasPrefix(category, "director_") {
		personID, err := strconv.ParseInt(strings.TrimPrefix(category, "director_"), 10, 64)
		if err != nil || personID <= 0 {
			http.NotFound(w, r)
			return
		}
		rows, total := store.ListDirectorCategory(r.Context(), personID, page, perPage)
		sendCategoryResponse(w, rows, total, page, perPage)
		return
	}

	// ── movies_id_{year} ─────────────────────────────────────────────────────
	if strings.HasPrefix(category, "movies_id_") {
		yearStr := chi.URLParam(r, "year")
		if yearStr == "" {
			yearStr = strings.TrimPrefix(category, "movies_id_")
		}
		year, err := strconv.Atoi(yearStr)
		if err != nil || year < 1900 || year > 2100 {
			http.NotFound(w, r)
			return
		}
		f := store.CategoryFilter{
			MediaTypes: []string{"movie"},
			Year:       year,
			Page:       page,
			PerPage:    perPage,
		}
		applyHideWatched(r, &f, profileID)
		applyCatalogTrackers(&f)
		rows, total := store.ListCategory(f)
		sendCategoryResponse(w, rows, total, page, perPage)
		return
	}

	// ── standard category ─────────────────────────────────────────────────────
	preset, ok := categoryRoutes[category]
	if !ok {
		http.NotFound(w, r)
		return
	}

	f := preset
	if category == "tracker_new" {
		f.RecentDays = store.GetSettingInt(r.Context(), "tracker_new_days")
	}
	f.Page = page
	f.PerPage = perPage
	if searchQ != "" {
		f.Search = searchQ
	}
	if q.Get("hide_unrated") == "1" {
		f.HideUnrated = true
	}
	applyHideWatched(r, &f, profileID)
	applyChildFilter(r, &f, profileID)
	applyAdultTextFilter(r, &f, profileID)
	applyCatalogTrackers(&f)

	rows, total := store.ListCategory(f)
	sendCategoryResponse(w, rows, total, page, perPage)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func applyCatalogTrackers(f *store.CategoryFilter) {
	for _, t := range strings.Split(cachedTrackers(), ",") {
		if t = strings.TrimSpace(t); t != "" {
			f.TrackerFilter = append(f.TrackerFilter, t)
		}
	}
	f.RequirePoster = cachedRequirePoster()
}

// lampaAgeToChildAge maps Lampa's "до N лет" age to our cert level.
func lampaAgeToChildAge(maxAge int) int {
	switch {
	case maxAge <= 6:
		return 0  // only 0+
	case maxAge <= 12:
		return 6  // up to 6+
	case maxAge <= 16:
		return 12 // up to 12+
	default:
		return 16 // up to 16+
	}
}

func applyChildFilter(r *http.Request, f *store.CategoryFilter, profileID string) {
	q := r.URL.Query()

	// Priority: query params sent by plugin (works for all profile types: Lampa, np_profiles)
	if childAge, err := strconv.Atoi(q.Get("child_age")); err == nil && childAge > 0 {
		f.Child = true
		f.ChildAge = lampaAgeToChildAge(childAge)
	} else {
		// Fallback: DB lookup via token+profile_id (our server profiles)
		d := deviceFromRequest(r)
		if d == nil || profileID == "" {
			return
		}
		child, birthYear := store.GetProfileChildInfo(r.Context(), d.ID, profileID)
		if !child {
			return
		}
		f.Child = true
		f.ChildAge = -1
		if birthYear != nil && *birthYear > 0 {
			age := time.Now().Year() - *birthYear
			switch {
			case age < 6:
				f.ChildAge = 0
			case age < 12:
				f.ChildAge = 6
			case age < 16:
				f.ChildAge = 12
			default:
				f.ChildAge = 16
			}
		}
	}

	f.ChildBlockedKeywords = cachedChildKeywords()
	// Apply text keyword filter only for configured age levels
	if words := cachedChildTextKeywords(); len(words) > 0 {
		for _, age := range cachedChildTextAges() {
			if f.ChildAge == age {
				f.ChildTextKeywords = words
				break
			}
		}
	}
}

// applyAdultTextFilter applies text keyword filtering for non-child profiles
// when age group 99 ("Взрослые профили") is enabled in settings.
func applyAdultTextFilter(r *http.Request, f *store.CategoryFilter, profileID string) {
	words := cachedChildTextKeywords()
	if len(words) == 0 {
		return
	}
	adultEnabled := false
	for _, age := range cachedChildTextAges() {
		if age == 99 {
			adultEnabled = true
			break
		}
	}
	if !adultEnabled {
		return
	}
	// Only apply for non-child profiles (skip if already set by child filter)
	if f.Child {
		return
	}
	d := deviceFromRequest(r)
	if d == nil {
		// No token — apply to anonymous requests too
		f.ChildTextKeywords = words
		return
	}
	if profileID == "" {
		f.ChildTextKeywords = words
		return
	}
	child, _ := store.GetProfileChildInfo(r.Context(), d.ID, profileID)
	if !child {
		f.ChildTextKeywords = words
	}
}

func applyHideWatched(r *http.Request, f *store.CategoryFilter, profileID string) {
	q := r.URL.Query()
	// Plugin sends hide_watched=1 and percent=90
	hideWatched := q.Get("hide_watched") == "1" || q.Get("hide_watched") == "true"
	if d := deviceFromRequest(r); d != nil && hideWatched {
		pct, _ := strconv.Atoi(q.Get("percent"))
		if pct < 1 {
			pct = 90
		}
		f.HideWatched = true
		f.DeviceID = d.ID
		f.ProfileID = profileID
		f.WatchedPercent = pct
	}
}

func sendCategoryResponse(w http.ResponseWriter, rows []store.MediaRow, total, page, perPage int) {
	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		results = append(results, toMediaItem(row))
	}
	JSON(w, http.StatusOK, map[string]any{
		"page":          page,
		"results":       results,
		"total_pages":   totalPages,
		"total_results": total,
	})
}

// ─── Continues ────────────────────────────────────────────────────────────────

func handleContinues(w http.ResponseWriter, r *http.Request, category, profileID string, page, perPage int) {
	d := deviceFromRequest(r)
	if d == nil {
		JSON(w, http.StatusOK, emptyPage(page))
		return
	}

	minPct, _ := strconv.Atoi(r.URL.Query().Get("min_progress"))
	if minPct < 1 {
		minPct = 90
	}

	var mediaFilter string
	switch {
	case category == "continues_movie":
		mediaFilter = "movie"
	case category == "continues_tv" || category == "continues_anime":
		mediaFilter = "tv"
	}

	entries, total := store.GetContinues(r.Context(), d.ID, profileID, mediaFilter, minPct, page, perPage)
	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	JSON(w, http.StatusOK, map[string]any{
		"page":          page,
		"results":       entries,
		"total_pages":   totalPages,
		"total_results": total,
	})
}

// ─── POST /api/view ───────────────────────────────────────────────────────────
// Records a play event for popularity tracking.
// With token: ident = profile_id or device_id (server-verified).
// Without token: ident = uid query param (client-provided, deduplicated per day).

func handleView(w http.ResponseWriter, r *http.Request) {
	cardID := r.URL.Query().Get("card_id")
	uid := r.URL.Query().Get("uid")
	pct, _ := strconv.Atoi(r.URL.Query().Get("percent"))

	if cardID != "" && uid != "" && pct >= 30 {
		store.RecordPlayEvent(r.Context(), cardID, uid, pct)
		go forwardPlayEvent(cardID, uid, pct)
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ─── Popular ──────────────────────────────────────────────────────────────────

func handlePopular(w http.ResponseWriter, r *http.Request, page, perPage int, search string) {
	rows, total := store.GetPopular(r.Context(), page, perPage, search)
	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := toMediaItem(row)
		if row.Plays > 0 {
			item["plays"] = row.Plays
			item["viewers"] = row.Viewers
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

// ─── Search ───────────────────────────────────────────────────────────────────

func handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		query = r.URL.Query().Get("query")
	}
	if query == "" {
		query = r.URL.Query().Get("search")
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	rows := store.SearchMedia(query, limit, offset)
	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		results = append(results, toMediaItem(row))
	}
	totalPages := page
	if len(rows) == limit {
		totalPages = page + 1
	}
	JSON(w, http.StatusOK, map[string]any{
		"page":          page,
		"results":       results,
		"total_pages":   totalPages,
		"total_results": len(results),
	})
}

// ─── Response builder ─────────────────────────────────────────────────────────

func toMediaItem(row store.MediaRow) map[string]any {
	releaseDate := ""
	if row.ReleaseDate != nil {
		releaseDate = *row.ReleaseDate
	}
	firstAirDate := ""
	if row.FirstAirDate != nil {
		firstAirDate = *row.FirstAirDate
	}
	lastAirDate := ""
	if row.LastAirDate != nil {
		lastAirDate = *row.LastAirDate
	}

	var seasons any
	if row.Seasons != nil {
		var s any
		json.Unmarshal(row.Seasons, &s) //nolint:errcheck
		seasons = s
	}

	origLang := ""
	if row.OriginalLanguage != nil {
		origLang = *row.OriginalLanguage
	}
	status := ""
	if row.Status != nil {
		status = *row.Status
	}

	var lastEp any
	if row.LastEpSeason != nil && row.LastEpNumber != nil {
		lastEp = map[string]any{
			"season_number":  *row.LastEpSeason,
			"episode_number": *row.LastEpNumber,
		}
	}

	createDate := ""
	if row.LatestTorrentDate != nil {
		createDate = row.LatestTorrentDate.Format(time.RFC3339)
	}

	certRU := ""
	if row.CertificationRU != nil {
		certRU = *row.CertificationRU
	}
	certUS := ""
	if row.CertificationUS != nil {
		certUS = *row.CertificationUS
	}

	return map[string]any{
		"id":                  row.TmdbID,
		"media_type":          row.MediaType,
		"name":                row.Title,
		"title":               row.Title,
		"original_name":       row.OriginalTitle,
		"original_title":      row.OriginalTitle,
		"overview":            row.Overview,
		"poster_path":         row.PosterPath,
		"backdrop_path":       row.BackdropPath,
		"release_date":        releaseDate,
		"first_air_date":      firstAirDate,
		"last_air_date":       lastAirDate,
		"vote_average":        row.VoteAverage,
		"vote_count":          row.VoteCount,
		"original_language":   origLang,
		"adult":               row.Adult,
		"status":              status,
		"number_of_seasons":   row.NumberOfSeasons,
		"seasons":             seasons,
		"last_episode_to_air": lastEp,
		"release_quality":     qualityText(row.VideoQuality),
		"create_date":         createDate,
		"certification_ru":    certRU,
		"certification_us":    certUS,
		"source":              "Movies API",
	}
}

func qualityText(q int) string {
	switch {
	case q >= 300:
		switch q {
		case 300:
			return "WEBDL 2160p"
		case 301:
			return "WEBDL HDR 2160p"
		case 302:
			return "WEBDL DV 2160p"
		case 303:
			return "BDRip 2160p"
		case 304:
			return "BDRip HDR 2160p"
		case 306:
			return "Remux 2160p"
		default:
			return "2160p"
		}
	case q >= 200:
		switch q {
		case 200:
			return "WEBDL 1080p"
		case 201:
			return "BDRip 1080p"
		case 203:
			return "Remux 1080p"
		default:
			return "1080p"
		}
	case q >= 100:
		switch q {
		case 100:
			return "WEBDL 720p"
		case 101:
			return "BDRip 720p"
		default:
			return "720p"
		}
	default:
		return "SD"
	}
}

func emptyPage(page int) map[string]any {
	return map[string]any{
		"page": page, "results": []any{},
		"total_pages": 1, "total_results": 0,
	}
}

// InitCategorySettings reads configurable category filter values from app_settings
// and applies them to categoryRoutes. Called once at startup — requires restart to take effect.
func InitCategorySettings() {
	ctx := context.Background()

	set := func(key string, delta int) {
		f := categoryRoutes[key]
		f.YearDelta = delta
		categoryRoutes[key] = f
	}
	setQ := func(key string, q int) {
		f := categoryRoutes[key]
		f.MinVideoQuality = q
		categoryRoutes[key] = f
	}

	if d := store.GetSettingInt(ctx, "movies_new_year_delta"); d > 0 {
		set("movies_new", d)
		set("movies_ru_new", d)
		set("movies", d)
		set("movies_ru", d)
	}
	if d := store.GetSettingInt(ctx, "movies_4k_year_delta"); d > 0 {
		set("movies_4k_new", d)
		set("movies_4k", d)
	}
	if q := store.GetSettingInt(ctx, "movies_new_min_quality"); q > 0 {
		setQ("movies_new", q)
		setQ("movies_ru_new", q)
	}
}
