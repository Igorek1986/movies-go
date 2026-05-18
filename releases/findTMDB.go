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
func filterByYear(isMovie bool, list []*models.Entity, torrYear int) []*models.Entity {
	if !isMovie || torrYear == 0 {
		return list
	}
	return utils.Filter(list, func(i int, e *models.Entity) bool {
		if len(e.ReleaseDate) > 6 {
			year, _ := strconv.Atoi(e.ReleaseDate[6:])
			return utils.Abs(year-torrYear) > 1
		}
		return true
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
func FindTMDBID(isMovie bool, torr *models.TorrentDetails) *models.Entity {
	if torr.IMDBID != "" {
		return tmdb.FindByID(isMovie, torr.IMDBID, "imdb_id")
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

// FindTMDB searches TMDB by name, tries queries in priority order,
// returns the first candidate whose title matches the torrent name.
func FindTMDB(isMovie bool, torr *models.TorrentDetails) *models.Entity {
	names := append([]string{torr.Name}, torr.Names...)

	for _, query := range searchQueries(torr.Name, torr.Names) {
		candidates := tmdb.Search(isMovie, query)
		candidates = filterByYear(isMovie, candidates, torr.Year)

		for _, cand := range candidates {
			if nameMatches(names, cand) {
				return cand
			}
		}
	}
	return nil
}
