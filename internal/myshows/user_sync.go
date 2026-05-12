package myshows

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"lampa-api/config"
	"net/http"
	"strings"
)

// rpcAuth sends an authenticated JSON-RPC call to MyShows.
// MyShows requires the "authorization2" header (not "Authorization").
func rpcAuth(ctx context.Context, token, method string, params map[string]any) (json.RawMessage, error) {
	apiURL := config.Get().MyShowsAPI
	if apiURL == "" {
		apiURL = "https://myshows.me/v3/rpc/"
	}

	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("authorization2", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("myshows rpc %s: status %d", method, resp.StatusCode)
	}

	raw, _ := io.ReadAll(resp.Body)
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("myshows: %s", envelope.Error.Message)
	}
	return envelope.Result, nil
}

// LoginUser authenticates with MyShows via /api/session and returns the access token.
func LoginUser(ctx context.Context, login, password string) (string, error) {
	authURL := config.Get().MyShowsAuthURL
	if authURL == "" {
		authURL = "https://myshows.me/api/session"
	}

	body, _ := json.Marshal(map[string]string{"login": login, "password": password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("неверный логин или пароль (status %d)", resp.StatusCode)
	}

	raw, _ := io.ReadAll(resp.Body)
	var data struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &data); err != nil || data.Token == "" {
		return "", fmt.Errorf("MyShows не вернул токен")
	}
	return data.Token, nil
}

// WatchedMovie is a movie the user has marked as watched on MyShows.
type WatchedMovie struct {
	Title      string // Russian title from MyShows
	OrigTitle  string // Original title (used for hash computation)
	ImdbID     string // "tt0123456" format
	TmdbID     int64
	Year       int
	WatchedAt  string // ISO date string, e.g. "2024-03-15" (empty if unknown)
}

// WatchedEpisode is a single watched episode with season/episode numbers.
type WatchedEpisode struct {
	Season    int
	Episode   int
	WatchedAt string // "YYYY-MM-DD", from profile.Episodes watchDate
}

// EpisodeInfo holds full episode metadata returned by shows.GetById.
type EpisodeInfo struct {
	MyshowsEpID int
	Season      int
	Episode     int
	Title       string
	RuntimeMin  int    // minutes; 0 if unknown
	IsSpecial   bool
	AirDate     string // "YYYY-MM-DD"; empty if unknown
}

// WatchedShow is a TV show with a list of watched episodes.
type WatchedShow struct {
	Title       string
	OrigTitle   string
	ImdbID      string
	TmdbID      int64
	Year        int
	MyshowsID   int
	Episodes    []WatchedEpisode
	AllEpisodes []EpisodeInfo // full episode list for populating episodes table
}

// GetWatchedMovies returns all movies the user has marked as watched on MyShows.
// Uses the profile.WatchedMovies method.
func GetWatchedMovies(ctx context.Context, token string) ([]WatchedMovie, error) {
	res, err := rpcAuth(ctx, token, "profile.WatchedMovies", map[string]any{})
	if err != nil {
		return nil, err
	}

	var raw []json.RawMessage
	if err := json.Unmarshal(res, &raw); err != nil {
		return nil, err
	}

	var movies []WatchedMovie
	for _, item := range raw {
		var m struct {
			Title         string `json:"title"`
			TitleOriginal string `json:"titleOriginal"`
			ImdbID        any    `json:"imdbId"` // can be int or string
			TmdbID        int64  `json:"tmdbId"`
			Year          int    `json:"year"`
			UserMovie     struct {
				WatchDate string `json:"watchDate"` // "2025-09-26T21:49:15+0300"
			} `json:"userMovie"`
		}
		if err := json.Unmarshal(item, &m); err != nil {
			continue
		}
		// Normalise to "YYYY-MM-DD"
		watchedDate := ""
		if len(m.UserMovie.WatchDate) >= 10 {
			watchedDate = m.UserMovie.WatchDate[:10]
		}
		movies = append(movies, WatchedMovie{
			Title:     m.Title,
			OrigTitle: m.TitleOriginal,
			ImdbID:    formatImdbID(m.ImdbID),
			TmdbID:    m.TmdbID,
			Year:      m.Year,
			WatchedAt: watchedDate,
		})
	}
	return movies, nil
}

// GetWatchedShows returns TV shows and their watched episodes from the user's MyShows profile.
// For each show it calls profile.Episodes and shows.GetById to resolve season/episode numbers.
func GetWatchedShows(ctx context.Context, token string) ([]WatchedShow, error) {
	res, err := rpcAuth(ctx, token, "profile.Shows", map[string]any{
		"page":     0,
		"pageSize": 1000,
	})
	if err != nil {
		return nil, err
	}

	var raw []json.RawMessage
	if err := json.Unmarshal(res, &raw); err != nil {
		return nil, err
	}

	var shows []WatchedShow
	for _, item := range raw {
		var entry struct {
			Show struct {
				ID            int    `json:"id"`
				Title         string `json:"title"`
				TitleOriginal string `json:"titleOriginal"`
				ImdbID        any    `json:"imdbId"`
				TmdbID        int64  `json:"tmdbId"`
				Year          int    `json:"year"`
			} `json:"show"`
		}
		if err := json.Unmarshal(item, &entry); err != nil || entry.Show.ID == 0 {
			continue
		}

		eps, allEps, err := getWatchedEpisodesForShow(ctx, token, entry.Show.ID)
		if err != nil || len(eps) == 0 {
			continue
		}

		shows = append(shows, WatchedShow{
			Title:       entry.Show.Title,
			OrigTitle:   entry.Show.TitleOriginal,
			ImdbID:      formatImdbID(entry.Show.ImdbID),
			TmdbID:      entry.Show.TmdbID,
			Year:        entry.Show.Year,
			MyshowsID:   entry.Show.ID,
			Episodes:    eps,
			AllEpisodes: allEps,
		})
	}
	return shows, nil
}

// getWatchedEpisodesForShow fetches watched episode IDs via profile.Episodes,
// then resolves season/episode numbers and full metadata via shows.GetById.
// Returns watched episodes, all episodes (for upsert), and any error.
func getWatchedEpisodesForShow(ctx context.Context, token string, showID int) ([]WatchedEpisode, []EpisodeInfo, error) {
	// 1. Get watched episode IDs
	watchedRes, err := rpcAuth(ctx, token, "profile.Episodes", map[string]any{"showId": showID})
	if err != nil {
		return nil, nil, err
	}

	var watchedRaw []json.RawMessage
	if err := json.Unmarshal(watchedRes, &watchedRaw); err != nil {
		return nil, nil, err
	}

	// Build map: episode ID → watch date
	watchedDates := map[int]string{}
	for _, w := range watchedRaw {
		var ep struct {
			ID        int    `json:"id"`
			WatchDate string `json:"watchDate"`
		}
		if json.Unmarshal(w, &ep) == nil && ep.ID > 0 {
			date := ""
			if len(ep.WatchDate) >= 10 {
				date = ep.WatchDate[:10]
			}
			watchedDates[ep.ID] = date
		}
	}
	if len(watchedDates) == 0 {
		return nil, nil, nil
	}

	// 2. Get full episode details (season/episode numbers + runtime)
	detailRes, err := rpcAuth(ctx, token, "shows.GetById", map[string]any{
		"showId":       showID,
		"withEpisodes": true,
	})
	if err != nil {
		return nil, nil, err
	}

	var showData struct {
		Episodes []struct {
			ID            int    `json:"id"`
			SeasonNumber  *int   `json:"seasonNumber"`
			EpisodeNumber *int   `json:"episodeNumber"`
			Title         string `json:"title"`
			Runtime       int    `json:"runtime"`
			IsSpecial     bool   `json:"isSpecial"`
			AirDate       string `json:"airDate"`
			AirDateUTC    string `json:"airDateUTC"`
		} `json:"episodes"`
	}
	if err := json.Unmarshal(detailRes, &showData); err != nil {
		return nil, nil, err
	}

	var eps []WatchedEpisode
	var all []EpisodeInfo
	for _, ep := range showData.Episodes {
		if ep.SeasonNumber == nil || ep.ID == 0 {
			continue
		}
		snum := *ep.SeasonNumber
		enum := 0
		if ep.EpisodeNumber != nil {
			enum = *ep.EpisodeNumber
		}

		airDate := ep.AirDateUTC
		if airDate == "" {
			airDate = ep.AirDate
		}
		if len(airDate) > 10 {
			airDate = airDate[:10]
		}

		all = append(all, EpisodeInfo{
			MyshowsEpID: ep.ID,
			Season:      snum,
			Episode:     enum,
			Title:       ep.Title,
			RuntimeMin:  ep.Runtime,
			IsSpecial:   ep.IsSpecial,
			AirDate:     airDate,
		})

		date, watched := watchedDates[ep.ID]
		if watched && snum > 0 && enum > 0 {
			eps = append(eps, WatchedEpisode{
				Season:    snum,
				Episode:   enum,
				WatchedAt: date,
			})
		}
	}
	return eps, all, nil
}

// formatImdbID normalises a raw MyShows imdbId (may be int or "tt..." string) to "tt0123456".
func formatImdbID(raw any) string {
	if raw == nil {
		return ""
	}
	var s string
	switch v := raw.(type) {
	case float64:
		s = fmt.Sprintf("%d", int(v))
	case string:
		s = strings.TrimPrefix(v, "tt")
	default:
		return ""
	}
	if s == "" || s == "0" {
		return ""
	}
	// Ensure "tt" prefix with zero-padding to 7 digits if numeric
	if _, err := fmt.Sscanf(s, "%s", &s); err == nil {
		return "tt" + fmt.Sprintf("%07s", s)
	}
	return "tt" + s
}
