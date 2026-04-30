package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"lampa-api/db/postgres"
	"lampa-api/movies/tmdb"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// GET /api/media-card/{card_id}
func handleMediaCard(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "card_id")

	ctx := r.Context()
	row := postgres.Pool.QueryRow(ctx, `
		SELECT card_id, tmdb_id, media_type, title, original_title, overview,
		       poster_path, backdrop_path, release_date::text, first_air_date::text, last_air_date::text,
		       vote_average, vote_count, runtime, episode_run_time, original_language,
		       adult, status, number_of_seasons, number_of_episodes, seasons,
		       age_rating, certification_ru, genres, best_video_quality,
		       latest_torrent_date, rutor_category, imdb_id
		FROM media_cards WHERE card_id = $1`, cardID)

	var (
		cid, mediaType, title, origTitle     string
		overview, poster, backdrop           *string
		releaseDate, firstAirDate, lastAirDate *string
		voteAvg                              float64
		voteCount, runtime, epRunTime        *int
		origLang, status, imdbID             *string
		adult                                bool
		numSeasons, numEpisodes              *int
		seasonsJSON                          []byte
		ageRating                            *int
		certRU                               *string
		genresJSON                           []byte
		bestQuality                          *int
		latestTorrent                        *time.Time
		rutorCat                             *string
		tmdbID                               int64
	)

	if err := row.Scan(
		&cid, &tmdbID, &mediaType, &title, &origTitle, &overview,
		&poster, &backdrop, &releaseDate, &firstAirDate, &lastAirDate,
		&voteAvg, &voteCount, &runtime, &epRunTime, &origLang,
		&adult, &status, &numSeasons, &numEpisodes, &seasonsJSON,
		&ageRating, &certRU, &genresJSON, &bestQuality,
		&latestTorrent, &rutorCat, &imdbID,
	); err != nil {
		Error(w, http.StatusNotFound, "not found")
		return
	}

	year := ""
	if releaseDate != nil && len(*releaseDate) >= 4 {
		year = (*releaseDate)[:4]
	} else if firstAirDate != nil && len(*firstAirDate) >= 4 {
		year = (*firstAirDate)[:4]
	}

	var genres any
	if genresJSON != nil {
		json.Unmarshal(genresJSON, &genres) //nolint:errcheck
	}
	if string(seasonsJSON) == "null" {
		seasonsJSON = nil
	}
	var seasons any
	if seasonsJSON != nil {
		json.Unmarshal(seasonsJSON, &seasons) //nolint:errcheck
	} else if mediaType == "tv" && tmdb.TMDBAuthKey != "" {
		seasons = fetchAndPersistTVSeasons(ctx, cid, tmdbID)
	}

	torrentDate := ""
	if latestTorrent != nil {
		torrentDate = latestTorrent.Format("2006-01-02")
	}

	JSON(w, http.StatusOK, map[string]any{
		"card_id":           cid,
		"tmdb_id":           tmdbID,
		"media_type":        mediaType,
		"title":             title,
		"original_title":    origTitle,
		"overview":          strVal(overview),
		"poster_path":       strVal(poster),
		"backdrop_path":     strVal(backdrop),
		"release_date":      strVal(releaseDate),
		"first_air_date":    strVal(firstAirDate),
		"last_air_date":     strVal(lastAirDate),
		"year":              year,
		"vote_average":      voteAvg,
		"vote_count":        intVal(voteCount),
		"runtime":           intVal(runtime),
		"episode_run_time":  intVal(epRunTime),
		"original_language": strVal(origLang),
		"adult":             adult,
		"status":            strVal(status),
		"number_of_seasons": intVal(numSeasons),
		"number_of_episodes": intVal(numEpisodes),
		"seasons":           seasons,
		"age_rating":        intVal(ageRating),
		"certification_ru":  strVal(certRU),
		"genres":            genres,
		"best_video_quality": intVal(bestQuality),
		"torrent_date":      torrentDate,
		"rutor_category":    strVal(rutorCat),
		"imdb_id":           strVal(imdbID),
		"movie_item":        func() string {
			if mediaType == "movie" && origTitle != "" {
				return lampaHash(origTitle)
			}
			return ""
		}(),
	})
}

// GET /api/media-card/{card_id}/credits
// Fetches cast+crew from TMDB API (proxied to avoid exposing the token to the frontend).
func handleMediaCardCredits(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "card_id")

	var tmdbID int64
	var mediaType string
	err := postgres.Pool.QueryRow(r.Context(),
		`SELECT tmdb_id, media_type FROM media_cards WHERE card_id = $1`, cardID,
	).Scan(&tmdbID, &mediaType)
	if err != nil {
		JSON(w, http.StatusOK, map[string]any{"cast": []any{}})
		return
	}

	token := tmdb.TMDBAuthKey
	if token == "" {
		JSON(w, http.StatusOK, map[string]any{"cast": []any{}})
		return
	}

	url := fmt.Sprintf("https://api.themoviedb.org/3/%s/%d/credits?language=ru", mediaType, tmdbID)
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	req.Header.Set("Authorization", token)
	req.Header.Set("Accept", "application/json")

	resp, err := tmdb.HTTPClient().Do(req)
	if err != nil || resp.StatusCode != 200 {
		JSON(w, http.StatusOK, map[string]any{"cast": []any{}})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result map[string]any
	if json.Unmarshal(body, &result) != nil {
		JSON(w, http.StatusOK, map[string]any{"cast": []any{}})
		return
	}

	// Return only cast (first 20) to keep payload small
	cast, _ := result["cast"].([]any)
	if len(cast) > 20 {
		cast = cast[:20]
	}
	JSON(w, http.StatusOK, map[string]any{"cast": cast})
}

// GET /api/media-card/{card_id}/similar
// Returns similar items from our DB (same category, similar language).
func handleMediaCardSimilar(w http.ResponseWriter, r *http.Request) {
	cardID := chi.URLParam(r, "card_id")

	var mediaType string
	var genresJSON []byte
	err := postgres.Pool.QueryRow(r.Context(),
		`SELECT media_type, genres FROM media_cards WHERE card_id = $1`, cardID,
	).Scan(&mediaType, &genresJSON)
	if err != nil {
		JSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}

	// Parse genre IDs to pass as a typed array (avoids pgx json-param issues).
	var genreObjs []struct{ ID int32 `json:"id"` }
	json.Unmarshal(genresJSON, &genreObjs) //nolint:errcheck
	var genreIDs []int32
	for _, g := range genreObjs {
		if g.ID > 0 {
			genreIDs = append(genreIDs, g.ID)
		}
	}

	var rows interface{ Next() bool; Scan(...any) error; Close() }
	if len(genreIDs) > 0 {
		rows, err = postgres.Pool.Query(r.Context(), `
			SELECT card_id, tmdb_id, media_type, title, poster_path,
			       COALESCE(release_date::text, first_air_date::text, '')
			FROM media_cards
			WHERE card_id <> $1
			  AND media_type = $2
			  AND vote_count > 20
			  AND genres IS NOT NULL
			  AND genres::text != 'null'
			  AND EXISTS (
			    SELECT 1 FROM json_array_elements(genres::json) g
			    WHERE (g->>'id')::int = ANY($3)
			  )
			ORDER BY vote_average DESC NULLS LAST
			LIMIT 24`,
			cardID, mediaType, genreIDs,
		)
	} else {
		rows, err = postgres.Pool.Query(r.Context(), `
			SELECT card_id, tmdb_id, media_type, title, poster_path,
			       COALESCE(release_date::text, first_air_date::text, '')
			FROM media_cards
			WHERE card_id <> $1
			  AND media_type = $2
			  AND vote_count > 20
			ORDER BY vote_average DESC NULLS LAST
			LIMIT 24`,
			cardID, mediaType,
		)
	}
	if err != nil {
		JSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	defer rows.Close()

	type item struct {
		ID        int64  `json:"id"`      // tmdb_id — used by JS to build card links
		CardID    string `json:"card_id"`
		TmdbID    int64  `json:"tmdb_id"`
		MediaType string `json:"media_type"`
		Title     string `json:"title"`
		Poster    string `json:"poster_path"`
		Year      string `json:"year"`
	}
	var items []item
	for rows.Next() {
		var it item
		var date string
		if err := rows.Scan(&it.CardID, &it.TmdbID, &it.MediaType, &it.Title, &it.Poster, &date); err == nil {
			it.ID = it.TmdbID
			if len(date) >= 4 {
				it.Year = date[:4]
			}
			// Normalize poster_path: strip full URL if present, keep only the /path part.
			if p := it.Poster; len(p) > 0 && p[0] != '/' {
				if idx := strings.LastIndex(p, "/t/p/"); idx >= 0 {
					// Remove base URL and size prefix: keep from the path after size
					rest := p[idx+5:]
					if sl := strings.Index(rest, "/"); sl >= 0 {
						it.Poster = rest[sl:]
					}
				}
			}
			items = append(items, it)
		}
	}
	if items == nil {
		items = []item{}
	}
	JSON(w, http.StatusOK, map[string]any{"items": items})
}

// fetchAndPersistTVSeasons fetches seasons from TMDB for a TV show whose
// seasons column is NULL in the DB, saves the result, and returns it.
func fetchAndPersistTVSeasons(ctx context.Context, cardID string, tmdbID int64) any {
	url := fmt.Sprintf("https://api.themoviedb.org/3/tv/%d?language=ru", tmdbID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", tmdb.TMDBAuthKey)
	req.Header.Set("Accept", "application/json")

	resp, err := tmdb.HTTPClient().Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var tv struct {
		Seasons          json.RawMessage `json:"seasons"`
		LastEpisodeToAir *struct {
			SeasonNumber  int `json:"season_number"`
			EpisodeNumber int `json:"episode_number"`
		} `json:"last_episode_to_air"`
	}
	if err := json.Unmarshal(body, &tv); err != nil || tv.Seasons == nil {
		return nil
	}

	// Persist so subsequent requests hit the DB cache.
	if tv.LastEpisodeToAir != nil && tv.LastEpisodeToAir.SeasonNumber > 0 {
		postgres.Pool.Exec(ctx, //nolint:errcheck
			`UPDATE media_cards SET seasons = $1,
			  last_ep_season = COALESCE(last_ep_season, $3),
			  last_ep_number = COALESCE(last_ep_number, $4)
			WHERE card_id = $2`,
			[]byte(tv.Seasons), cardID,
			tv.LastEpisodeToAir.SeasonNumber, tv.LastEpisodeToAir.EpisodeNumber,
		)
	} else {
		postgres.Pool.Exec(ctx, //nolint:errcheck
			`UPDATE media_cards SET seasons = $1 WHERE card_id = $2`,
			[]byte(tv.Seasons), cardID,
		)
	}

	var result any
	json.Unmarshal(tv.Seasons, &result) //nolint:errcheck
	return result
}

// ── helpers ──────────────────────────────────────────────────────────────────

// lampaHash mirrors Lampa.Utils.hash() — Java-style hashCode with multiplier 31.
func lampaHash(s string) string {
	var h uint32
	for _, c := range s {
		h = h*31 + uint32(c)
	}
	signed := int32(h)
	if signed < 0 {
		signed = -signed
	}
	return strconv.Itoa(int(signed))
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func intVal(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

// GET /api/actor/{person_id}
func handleActorAPI(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	personID, err := strconv.ParseInt(chi.URLParam(r, "person_id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid person_id")
		return
	}
	person, works, err := tmdb.GetPerson(personID)
	if err != nil {
		Error(w, http.StatusNotFound, "not found")
		return
	}
	JSON(w, http.StatusOK, map[string]any{
		"id":           person.ID,
		"name":         person.Name,
		"biography":    person.Biography,
		"birthday":     person.Birthday,
		"profile_path": person.ProfilePath,
		"works":        works,
	})
}
