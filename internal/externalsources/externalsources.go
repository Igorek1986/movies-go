// Package externalsources provides runtime fallbacks for internal/tasks/fix_runtime.go
// when TMDB has no data. Each source is independently enabled/disabled and
// keyed via the admin panel (see db/store/external_sources.go) — a source
// with no row, or enabled=false, is silently skipped.
//
// Movies: poiskkino.dev, then Kinopoisk Api Unofficial (both matched by
// imdb_id — exact, no fuzzy title matching needed).
//
// Series: TheTVDB, then TVmaze (matched by title — media_cards.imdb_id is
// currently never populated for TV, see project notes — with a loose
// premiere-year sanity check to reject obviously-wrong matches for common
// titles). Kinopoisk Api Unofficial is deliberately NOT used for series —
// verified empirically that it never fills film_length for TV_SERIES, even
// for hits like "Игра престолов"/"Друзья". poiskkino.dev is also skipped for
// series — it only exposes totalSeriesLength (sum across all episodes, not
// per-episode) and needs a second call to compute an average, for middling
// gain over TheTVDB+TVmaze alone.
package externalsources

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"movies-api/db/store"
	iproxy "movies-api/internal/proxy"
)

func httpClientFor(ctx context.Context, route string) *http.Client {
	c := iproxy.Default.ClientFor(ctx, route)
	timeout := 10 * time.Second
	if c.Timeout == 0 || c.Timeout > timeout {
		clone := *c
		clone.Timeout = timeout
		return &clone
	}
	return c
}

func getJSON(ctx context.Context, route, rawURL string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClientFor(ctx, route).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return nil //nolint:nilerr // treat non-200 as "no data", not a hard error
	}
	return json.Unmarshal(body, out)
}

// yearOf extracts the leading 4-digit year from a date-ish string
// ("2011-04-17", "2011", "" → 0).
func yearOf(s string) int {
	if len(s) < 4 {
		return 0
	}
	y, err := strconv.Atoi(s[:4])
	if err != nil {
		return 0
	}
	return y
}

// yearMatches reports whether a candidate's year is close enough to the
// card's year to accept a title-based match. Either side being unknown (0)
// skips the check (accepts the match) rather than rejecting it.
func yearMatches(cardYear, candidateYear int) bool {
	if cardYear == 0 || candidateYear == 0 {
		return true
	}
	diff := cardYear - candidateYear
	if diff < 0 {
		diff = -diff
	}
	return diff <= 1
}

// ─── Movies ────────────────────────────────────────────────────────────────

// FetchMovieRuntime tries poiskkino.dev then Kinopoisk Api Unofficial, both
// matched by exact imdb_id. Returns 0 if none configured/enabled or no hit.
func FetchMovieRuntime(ctx context.Context, imdbID string) int {
	if imdbID == "" {
		return 0
	}
	if rt := poiskkinoMovieRuntime(ctx, imdbID); rt > 0 {
		return rt
	}
	return kinopoiskUnofficialMovieRuntime(ctx, imdbID)
}

func poiskkinoMovieRuntime(ctx context.Context, imdbID string) int {
	token, ok := store.GetExternalSource(ctx, "poiskkino")
	if !ok || token == "" {
		return 0
	}
	var resp struct {
		Docs []struct {
			MovieLength int `json:"movieLength"`
		} `json:"docs"`
	}
	u := "https://api.poiskkino.dev/v1.4/movie?limit=1&externalId.imdb=" + url.QueryEscape(imdbID)
	if err := getJSON(ctx, iproxy.RoutePoiskkino, u, map[string]string{"X-API-KEY": token}, &resp); err != nil {
		return 0
	}
	if len(resp.Docs) == 0 {
		return 0
	}
	return resp.Docs[0].MovieLength
}

func kinopoiskUnofficialMovieRuntime(ctx context.Context, imdbID string) int {
	token, ok := store.GetExternalSource(ctx, "kinopoisk_unofficial")
	if !ok || token == "" {
		return 0
	}
	headers := map[string]string{"X-API-KEY": token}

	var search struct {
		Items []struct {
			KinopoiskID int `json:"kinopoiskId"`
		} `json:"items"`
	}
	searchURL := "https://kinopoiskapiunofficial.tech/api/v2.2/films?imdbId=" + url.QueryEscape(imdbID)
	if err := getJSON(ctx, iproxy.RouteKinopoiskUnofficial, searchURL, headers, &search); err != nil {
		return 0
	}
	if len(search.Items) == 0 {
		return 0
	}

	var detail struct {
		FilmLength int `json:"filmLength"`
	}
	detailURL := "https://kinopoiskapiunofficial.tech/api/v2.2/films/" + strconv.Itoa(search.Items[0].KinopoiskID)
	if err := getJSON(ctx, iproxy.RouteKinopoiskUnofficial, detailURL, headers, &detail); err != nil {
		return 0
	}
	return detail.FilmLength
}

// ─── Series ────────────────────────────────────────────────────────────────

// FetchSeriesRuntime tries TheTVDB then TVmaze, matched by title (original
// title preferred — both index original-language names well) with a loose
// premiere-year sanity check. cardYear is the card's release/first-air year,
// or 0 if unknown. Returns 0 if none configured/enabled or no confident hit.
func FetchSeriesRuntime(ctx context.Context, title, originalTitle string, cardYear int) int {
	query := strings.TrimSpace(originalTitle)
	if query == "" {
		query = strings.TrimSpace(title)
	}
	if query == "" {
		return 0
	}
	if rt := thetvdbSeriesRuntime(ctx, query, cardYear); rt > 0 {
		return rt
	}
	return tvmazeSeriesRuntime(ctx, query, cardYear)
}

// ─── TheTVDB (v4, needs a session token from /login) ────────────────────────

var (
	tvdbTokenMu  sync.Mutex
	tvdbToken    string
	tvdbTokenKey string // apikey the cached token was issued for — invalidates the cache if the admin changes the key
	tvdbTokenAt  time.Time
)

func thetvdbToken(ctx context.Context, apikey string) string {
	tvdbTokenMu.Lock()
	defer tvdbTokenMu.Unlock()
	if tvdbToken != "" && tvdbTokenKey == apikey && time.Since(tvdbTokenAt) < time.Hour {
		return tvdbToken
	}

	body, _ := json.Marshal(map[string]string{"apikey": apikey})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api4.thetvdb.com/v4/login", strings.NewReader(string(body)))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClientFor(ctx, iproxy.RouteTheTVDB).Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var out struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Data.Token == "" {
		return ""
	}
	tvdbToken = out.Data.Token
	tvdbTokenKey = apikey
	tvdbTokenAt = time.Now()
	return tvdbToken
}

func thetvdbSeriesRuntime(ctx context.Context, query string, cardYear int) int {
	apikey, ok := store.GetExternalSource(ctx, "thetvdb")
	if !ok || apikey == "" {
		return 0
	}
	token := thetvdbToken(ctx, apikey)
	if token == "" {
		return 0
	}
	headers := map[string]string{"Authorization": "Bearer " + token}

	var search struct {
		Data []struct {
			TvdbID string `json:"tvdb_id"`
		} `json:"data"`
	}
	searchURL := "https://api4.thetvdb.com/v4/search?type=series&query=" + url.QueryEscape(query)
	if err := getJSON(ctx, iproxy.RouteTheTVDB, searchURL, headers, &search); err != nil {
		return 0
	}
	if len(search.Data) == 0 || search.Data[0].TvdbID == "" {
		return 0
	}

	var detail struct {
		Data struct {
			AverageRuntime int    `json:"averageRuntime"`
			Year           string `json:"year"`
		} `json:"data"`
	}
	detailURL := "https://api4.thetvdb.com/v4/series/" + url.PathEscape(search.Data[0].TvdbID)
	if err := getJSON(ctx, iproxy.RouteTheTVDB, detailURL, headers, &detail); err != nil {
		return 0
	}
	if !yearMatches(cardYear, yearOf(detail.Data.Year)) {
		return 0
	}
	return detail.Data.AverageRuntime
}

// ─── TVmaze (public API, no key needed for the free tier) ───────────────────

func tvmazeSeriesRuntime(ctx context.Context, query string, cardYear int) int {
	if _, ok := store.GetExternalSource(ctx, "tvmaze"); !ok {
		return 0
	}
	var show struct {
		Runtime        int    `json:"runtime"`
		AverageRuntime int    `json:"averageRuntime"`
		Premiered      string `json:"premiered"`
	}
	u := "https://api.tvmaze.com/singlesearch/shows?q=" + url.QueryEscape(query)
	if err := getJSON(ctx, iproxy.RouteTVmaze, u, nil, &show); err != nil {
		return 0
	}
	if !yearMatches(cardYear, yearOf(show.Premiered)) {
		return 0
	}
	if show.AverageRuntime > 0 {
		return show.AverageRuntime
	}
	return show.Runtime
}
