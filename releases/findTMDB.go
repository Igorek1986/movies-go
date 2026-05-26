package releases

import (
	"bytes"
	"movies-api/db/models"
	"movies-api/movies/tmdb"
	"movies-api/utils"
	"github.com/PuerkitoBio/goquery"
	"strconv"
	"strings"
)

// filterByYear filters movie search results by release year (±1).
// For TV shows year filtering is skipped: first_air_date is the debut year of the show,
// not the current season, so it can differ from the torrent year by many years.
// Note: utils.Filter removes items where fn=true, so the predicate is inverted.
func filterByYear(isMovie bool, list []*models.Entity, torrYear int) []*models.Entity {
	if !isMovie || torrYear == 0 {
		return list
	}
	return utils.Filter(list, func(i int, e *models.Entity) bool {
		if len(e.ReleaseDate) > 6 {
			year, _ := strconv.Atoi(e.ReleaseDate[6:])
			return utils.Abs(year-torrYear) > 1 // remove if year is far from torrent year
		}
		return false // no release date — keep the candidate
	})
}

// searchQueries builds a prioritized list of search queries for a torrent.
func searchQueries(name string, names []string) []string {
	seen := map[string]bool{}
	var queries []string
	add := func(q string) {
		q = strings.TrimSpace(q)
		if q != "" && !seen[q] {
			seen[q] = true
			queries = append(queries, q)
		}
	}

	add(name)

	var b strings.Builder
	for _, r := range name {
		if r == '.' || r == ':' || r == ';' || r == ',' {
			b.WriteRune(' ')
		} else {
			b.WriteRune(r)
		}
	}
	add(strings.Join(strings.Fields(b.String()), " "))

	if idx := strings.IndexAny(name, ".:"); idx > 0 {
		add(name[:idx])
	}
	if idx := strings.Index(name, "."); idx > 0 {
		add(name[idx+1:])
	}

	for _, n := range names {
		add(n)
	}

	return queries
}

// nameMatches returns true if any torrent name is similar to the entity title.
func nameMatches(names []string, e *models.Entity) bool {
	for _, name := range names {
		cn := utils.ClearStr(name)
		if cn == "" {
			continue
		}
		if utils.SimilarStr(cn, utils.ClearStr(e.Title)) ||
			utils.SimilarStr(cn, utils.ClearStr(e.OriginalTitle)) {
			return true
		}
		for _, title := range e.Titles {
			if utils.SimilarStr(cn, utils.ClearStr(title)) {
				return true
			}
		}
	}
	return false
}

// FindTMDBID tries to find a TMDB entity via IMDB/KP ID from the torrent page.
// Kinozal and NNMClub use pseudo-hashes; their detail pages are not needed — title search suffices.
func FindTMDBID(isMovie bool, torr *models.TorrentDetails) *models.Entity {
	if torr.IMDBID != "" {
		return tmdb.FindByID(isMovie, torr.IMDBID, "imdb_id")
	}
	if torr.Tracker == "kinozal" || torr.Tracker == "nnmclub" {
		return nil
	}
	body := GetBodyLink(torr)
	doc, err := goquery.NewDocumentFromReader(bytes.NewBufferString(body))
	if err != nil {
		return nil
	}

	imdbID := ""
	kpID := ""

	doc.Find("table#details").Find("a").Each(func(i int, selection *goquery.Selection) {
		if link, ok := selection.Attr("href"); ok {
			if strings.Contains(link, "www.imdb.com") {
				link = strings.TrimRight(link, "/")
				arr := strings.Split(link, "/")
				if len(arr) > 0 {
					imdbID = arr[len(arr)-1]
				}
			}
			if strings.Contains(link, "www.kinopoisk.ru") {
				link = strings.TrimRight(link, "/")
				arr := strings.Split(link, "/")
				if len(arr) > 0 {
					kpID = arr[len(arr)-1]
				}
			}
		}
	})
	if imdbID == "" && kpID == "" {
		return nil
	}

	torr.IMDBID = imdbID
	torr.KPID = kpID

	if imdbID != "" {
		return tmdb.FindByID(isMovie, imdbID, "imdb_id")
	}
	return nil
}

// tvYearDist returns how many years before the torrent year the show started.
// Returns 9999 if the show started after the torrent year or has no date.
// Handles both "YYYY-MM-DD" and "DD.MM.YYYY" (post-FixDate) formats.
func tvYearDist(e *models.Entity, torrYear int) int {
	date := e.FirstAirDate
	if len(date) < 4 {
		return 9999
	}
	var year int
	var err error
	if len(date) == 10 && date[2] == '.' {
		// DD.MM.YYYY format (after FixDate)
		year, err = strconv.Atoi(date[6:])
	} else {
		year, err = strconv.Atoi(date[:4])
	}
	if err != nil || year > torrYear {
		return 9999
	}
	return torrYear - year
}

// FindTMDB searches TMDB by name, tries queries in priority order,
// returns the best matching candidate. For TV shows with multiple name
// matches, prefers the show whose first_air_date year is closest to the
// torrent year (tiebreaker for same-name shows from different years).
func FindTMDB(isMovie bool, torr *models.TorrentDetails) *models.Entity {
	names := append([]string{torr.Name}, torr.Names...)

	var matches []*models.Entity
	seen := map[int64]bool{}

	collect := func(candidates []*models.Entity) {
		for _, cand := range candidates {
			if nameMatches(names, cand) && !seen[cand.ID] {
				seen[cand.ID] = true
				matches = append(matches, cand)
			}
		}
	}

	for _, query := range searchQueries(torr.Name, torr.Names) {
		candidates := tmdb.Search(isMovie, query)
		candidates = filterByYear(isMovie, candidates, torr.Year)
		collect(candidates)
	}

	// For TV shows with a known year, also search with first_air_date_year
	// to surface new shows that rank below older same-name shows in general search.
	if !isMovie && torr.Year > 0 {
		for _, query := range searchQueries(torr.Name, torr.Names) {
			collect(tmdb.SearchWithYear(isMovie, query, torr.Year))
		}
	}

	if len(matches) == 0 {
		return nil
	}
	if len(matches) == 1 || isMovie || torr.Year == 0 {
		return matches[0]
	}

	// Multiple TV candidates: prefer the one whose first_air_date year is
	// closest to (but not after) the torrent year.
	best := matches[0]
	bestDist := tvYearDist(best, torr.Year)
	for _, cand := range matches[1:] {
		if d := tvYearDist(cand, torr.Year); d < bestDist {
			bestDist = d
			best = cand
		}
	}
	return best
}
