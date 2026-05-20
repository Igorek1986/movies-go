package tmdb

import (
	"movies-api/config"
	"movies-api/db/models"
	"movies-api/db/store"
	"movies-api/utils"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	tmdbEndpoint  = "https://api.themoviedb.org/3/"
	imageEndpoint = "http://image.tmdb.org/t/p/"
)

var (
	genres      []*models.Genre
	TMDBAuthKey string
	tmdbClient  = &http.Client{Timeout: 30 * time.Second}
)

// HTTPClient returns the configured TMDB HTTP client (proxy-aware).
func HTTPClient() *http.Client { return tmdbClient }

func Init() {
	log.Println("Init tmdb")

	cfg := config.Get()
	if cfg.TmdbToken == "" {
		log.Println("TMDB token not set — enrichment disabled")
		return
	}
	TMDBAuthKey = strings.TrimSpace(cfg.TmdbToken)

	if cfg.ProxyURL != "" {
		if t, err := buildSocks5Transport(cfg.ProxyURL, cfg.ProxyUser, cfg.ProxyPass); err == nil {
			tmdbClient = &http.Client{Transport: t, Timeout: 30 * time.Second}
			log.Printf("TMDB: using SOCKS5 proxy %s", cfg.ProxyURL)
		} else {
			log.Println("TMDB: proxy setup failed:", err)
		}
	}

	go func() {
		lstmg := GetGenres("movie")
		lsttvg := GetGenres("tv")
		if lstmg == nil && lsttvg == nil {
			return
		}
		merged := append(lstmg, lsttvg...)
		sort.Slice(merged, func(i, j int) bool {
			return merged[i].Name < merged[j].Name
		})
		genres = merged
		log.Println("TMDB: genres loaded")
	}()

}

func GetVideoDetails(isMovie bool, id int64) *models.Entity {
	mediaType := "tv"
	if isMovie {
		mediaType = "movie"
	}
	if ent := store.GetMediaCard(id, mediaType); ent != nil {
		// For TV shows: if seasons are missing, fall through to TMDB re-fetch.
		if isMovie || len(ent.Seasons) > 0 {
			return ent
		}
	}

	params := map[string]string{}
	//params["api_key"] = apiKey

	if _, ok := params["language"]; !ok {
		params["language"] = "ru"
	}

	ids := strconv.FormatInt(id, 10)

	endpoint := ""
	if isMovie {
		endpoint = "movie/" + ids
	} else {
		endpoint = "tv/" + ids
	}

	var ent *models.Entity
	err := readPageTmdb(endpoint, params, &ent)
	if err != nil || ent == nil {
		return nil
	}
	fixEntity(ent)

	titles := alternativeTitles(isMovie, id)
	ent.Titles = titles

	return ent
}

// FetchRuntime fetches runtime (movies) or episode_run_time[0] (TV) directly from TMDB,
// bypassing the local DB cache. Returns 0 if unavailable.
// If the default (English) response has runtime=0 and the content has a non-English
// original language, retries with that language (e.g. "ru", "fr", "de").
func FetchRuntime(isMovie bool, tmdbID int64) int {
	if TMDBAuthKey == "" {
		return 0
	}
	endpoint := "tv/" + strconv.FormatInt(tmdbID, 10)
	if isMovie {
		endpoint = "movie/" + strconv.FormatInt(tmdbID, 10)
	}

	runtime := func(ent *models.Entity) int {
		if isMovie {
			return ent.Runtime
		}
		if len(ent.EpisodeRunTime) > 0 {
			return ent.EpisodeRunTime[0]
		}
		return 0
	}

	var ent *models.Entity
	if err := readPageTmdb(endpoint, map[string]string{}, &ent); err != nil || ent == nil {
		return 0
	}
	if v := runtime(ent); v > 0 {
		return v
	}

	// Fallback: retry with original_language if non-English.
	if ent.OriginalLanguage != "" && ent.OriginalLanguage != "en" {
		var loc *models.Entity
		if err := readPageTmdb(endpoint, map[string]string{"language": ent.OriginalLanguage}, &loc); err == nil && loc != nil {
			return runtime(loc)
		}
	}
	return 0
}

func Search(isMovie bool, query string) []*models.Entity {
	return SearchWithYear(isMovie, query, 0)
}

func SearchWithYear(isMovie bool, query string, year int) []*models.Entity {
	var st = "movie"
	if !isMovie {
		st = "tv"
	}

	params := map[string]string{"query": query}
	if year > 0 {
		key := "first_air_date_year"
		if isMovie {
			key = "year"
		}
		params[key] = strconv.Itoa(year)
	}

	return listVideoPages("search/"+st, params)
}

func FindByID(isMovie bool, id string, idType string) *models.Entity {
	if ent := store.FindByIMDB(id); ent != nil {
		return ent
	}

	params := map[string]string{}
	params["external_source"] = idType
	params["language"] = "ru"

	var results *models.FindResult

	err := readPageTmdb("find/"+id, params, &results)
	if err != nil {
		return nil
	}

	if results == nil {
		return nil
	}

	var ent *models.Entity
	if isMovie {
		if len(results.MovieResults) > 0 {
			ent = results.MovieResults[0]
		}
	} else {
		if len(results.TVResults) > 0 {
			ent = results.TVResults[0]
		}
	}

	if ent == nil {
		return nil
	}

	ent = GetVideoDetails(isMovie, ent.ID)
	if ent == nil {
		return nil
	}
	return ent
}

func alternativeTitles(isMovie bool, id int64) []string {
	params := map[string]string{}

	var st = "movie"
	if !isMovie {
		st = "tv"
	}
	var results *models.AlternativeTitles

	err := readPageTmdb(st+"/"+strconv.FormatInt(id, 10)+"/alternative_titles", params, &results)
	if err != nil {
		return nil
	}

	if results == nil {
		return nil
	}

	var list []string

	for _, title := range results.Titles {
		if title.Title != "" {
			list = append(list, title.Title)
		}
	}

	return list
}

func listVideoPages(endpoint string, params map[string]string) []*models.Entity {
	p := map[string]string{}
	for k, v := range params {
		p[k] = v
	}
	p["page"] = "1"
	lst, pages := listVideo(endpoint, p)
	if pages > 10 {
		pages = 10
	}
	if pages > 1 {
		lsts := make([][]*models.Entity, pages-1)
		utils.ParallelFor(2, pages+1, func(i int) {
			p := map[string]string{}
			for k, v := range params {
				p[k] = v
			}
			p["page"] = strconv.Itoa(i)
			lsts[i-2], _ = listVideo(endpoint, p)
		})
		for _, l := range lsts {
			lst = append(lst, l...)
		}
	}

	return lst
}

func listVideo(endpoint string, params map[string]string) ([]*models.Entity, int) {
	if _, ok := params["language"]; !ok {
		params["language"] = "ru"
	}

	var results *models.EntityRequest
	pageParams := map[string]string{}
	for k, v := range params {
		pageParams[k] = v
	}

	err := readPageTmdb(endpoint, params, &results)
	if err != nil {
		return nil, 0
	}

	if results == nil {
		return nil, 0
	}

	for _, v := range results.Results {
		fixEntity(v)
	}

	return results.Results, results.TotalPages
}

// ─── Person / Actor ───────────────────────────────────────────────────────────

type PersonDetails struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Biography   string `json:"biography"`
	Birthday    string `json:"birthday"`
	ProfilePath string `json:"profile_path"`
}

type PersonCreditItem struct {
	ID           int64   `json:"id"`
	MediaType    string  `json:"media_type"`
	Title        string  `json:"title"`
	OriginalTitle string `json:"original_title"`
	PosterPath   string  `json:"poster_path"`
	Year         string  `json:"year"`
	VoteAverage  float64 `json:"vote_average"`
	Character    string  `json:"character"`
	Popularity   float64 `json:"-"`
}

type personCreditsRaw struct {
	Cast []struct {
		ID           int64   `json:"id"`
		MediaType    string  `json:"media_type"`
		Title        string  `json:"title"`
		Name         string  `json:"name"`
		OriginalTitle string `json:"original_title"`
		OriginalName  string `json:"original_name"`
		PosterPath   string  `json:"poster_path"`
		ReleaseDate  string  `json:"release_date"`
		FirstAirDate string  `json:"first_air_date"`
		VoteAverage  float64 `json:"vote_average"`
		Character    string  `json:"character"`
		Popularity   float64 `json:"popularity"`
	} `json:"cast"`
}

func GetPerson(personID int64) (*PersonDetails, []PersonCreditItem, error) {
	params := map[string]string{"language": "ru-RU"}

	var person PersonDetails
	if err := readPageTmdb("person/"+strconv.FormatInt(personID, 10), params, &person); err != nil {
		return nil, nil, err
	}

	var credits personCreditsRaw
	_ = readPageTmdb("person/"+strconv.FormatInt(personID, 10)+"/combined_credits", params, &credits)

	seen := map[int64]bool{}
	works := make([]PersonCreditItem, 0, len(credits.Cast))
	type sortItem struct {
		item PersonCreditItem
		pop  float64
	}
	sortItems := make([]sortItem, 0, len(credits.Cast))
	for _, c := range credits.Cast {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		title := c.Title
		if title == "" {
			title = c.Name
		}
		origTitle := c.OriginalTitle
		if origTitle == "" {
			origTitle = c.OriginalName
		}
		date := c.ReleaseDate
		if date == "" {
			date = c.FirstAirDate
		}
		year := ""
		if len(date) >= 4 {
			year = date[:4]
		}
		sortItems = append(sortItems, sortItem{
			pop: c.Popularity,
			item: PersonCreditItem{
				ID: c.ID, MediaType: c.MediaType, Title: title,
				OriginalTitle: origTitle, PosterPath: c.PosterPath,
				Year: year, VoteAverage: c.VoteAverage, Character: c.Character,
			},
		})
	}
	sort.Slice(sortItems, func(i, j int) bool { return sortItems[i].pop > sortItems[j].pop })
	for _, s := range sortItems {
		works = append(works, s.item)
		if len(works) >= 100 {
			break
		}
	}
	return &person, works, nil
}
