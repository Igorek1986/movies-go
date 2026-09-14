package externalsources

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"movies-api/db/store"
	"movies-api/internal/myshows"
	iproxy "movies-api/internal/proxy"
)

type tvmazeEpisode struct {
	ID      int    `json:"id"`
	Season  int    `json:"season"`
	Number  *int   `json:"number"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Airdate string `json:"airdate"`
	Runtime *int   `json:"runtime"`
}

// FetchSeriesEpisodes fetches a TV show's full episode list (regular +
// specials) from TVmaze and returns it ready for store.UpsertEpisodes.
// Matches by imdb_id first (exact, via TVmaze's /lookup/shows) when known,
// falling back to title+year search (same approach as FetchSeriesRuntime)
// otherwise. Returns nil if TVmaze is disabled, no confident match, or the
// match has no episodes.
//
// TVmaze gives specials no episode number, only a season + airdate — unlike
// TMDB/TheTVDB, which flatten every special into one un-attributed season 0,
// TVmaze attributes each special to the season it actually belongs to, same
// as MyShows (verified empirically — see dev/instance-sync.md). We
// synthesize a number the same way MyShows does: specials within a season
// are numbered downward by airdate, ending at -1 for the most recent — same
// shape as MyShows' own specials numbering (see episodes.go), so rows from
// either source sort and display the same way.
func FetchSeriesEpisodes(ctx context.Context, mc *store.MediaCardEpInfo) []store.EpisodeRow {
	if _, ok := store.GetExternalSource(ctx, "tvmaze"); !ok {
		return nil
	}

	imdbID := ""
	if mc.ImdbID != nil {
		imdbID = strings.TrimSpace(*mc.ImdbID)
	}

	showID := 0
	if imdbID != "" {
		showID = tvmazeLookupByImdb(ctx, imdbID)
	}
	if showID == 0 {
		query := strings.TrimSpace(mc.OriginalTitle)
		if query == "" {
			query = strings.TrimSpace(mc.Title)
		}
		if query == "" {
			return nil
		}
		year, _ := strconv.Atoi(mc.Year)
		showID = tvmazeSearchShowID(ctx, query, year)
	}
	if showID == 0 {
		return nil
	}

	eps := tvmazeShowEpisodes(ctx, showID)
	if len(eps) == 0 {
		return nil
	}

	// Synthesize episode numbers for specials — grouped by season, ordered
	// by airdate, numbered -n..-1 within each season (see doc comment above).
	specialsBySeason := map[int][]tvmazeEpisode{}
	for _, e := range eps {
		if e.Type != "regular" {
			specialsBySeason[e.Season] = append(specialsBySeason[e.Season], e)
		}
	}
	specialNum := map[int]map[int]int{} // season -> tvmaze episode id -> synthetic number
	for season, list := range specialsBySeason {
		sort.Slice(list, func(i, j int) bool { return list[i].Airdate < list[j].Airdate })
		n := len(list)
		m := make(map[int]int, n)
		for i, e := range list {
			m[e.ID] = i - n // 0-based i over n items => -n..-1
		}
		specialNum[season] = m
	}

	rows := make([]store.EpisodeRow, 0, len(eps))
	for _, e := range eps {
		isSpecial := e.Type != "regular"
		season := int16(e.Season)
		var episode int16
		if isSpecial {
			episode = int16(specialNum[e.Season][e.ID])
		} else if e.Number != nil {
			episode = int16(*e.Number)
		} else {
			continue // regular episode with no number — shouldn't happen, skip defensively
		}

		var title *string
		if e.Name != "" {
			t := e.Name
			title = &t
		}
		var durSec *int
		if e.Runtime != nil && *e.Runtime > 0 {
			d := *e.Runtime * 60
			durSec = &d
		}
		var airDate *time.Time
		if t, err := time.Parse("2006-01-02", e.Airdate); err == nil {
			airDate = &t
		}

		rows = append(rows, store.EpisodeRow{
			Season:      season,
			Episode:     episode,
			Title:       title,
			DurationSec: durSec,
			IsSpecial:   isSpecial,
			Hash:        myshows.EpisodeHash(int(season), int(episode), mc.OriginalTitle),
			AirDate:     airDate,
		})
	}
	return rows
}

func tvmazeLookupByImdb(ctx context.Context, imdbID string) int {
	var show struct {
		ID int `json:"id"`
	}
	u := "https://api.tvmaze.com/lookup/shows?imdb=" + url.QueryEscape(imdbID)
	if err := getJSON(ctx, iproxy.RouteTVmaze, u, nil, &show); err != nil {
		return 0
	}
	return show.ID
}

func tvmazeSearchShowID(ctx context.Context, query string, cardYear int) int {
	var show struct {
		ID        int    `json:"id"`
		Premiered string `json:"premiered"`
	}
	u := "https://api.tvmaze.com/singlesearch/shows?q=" + url.QueryEscape(query)
	if err := getJSON(ctx, iproxy.RouteTVmaze, u, nil, &show); err != nil {
		return 0
	}
	if !yearMatches(cardYear, yearOf(show.Premiered)) {
		return 0
	}
	return show.ID
}

func tvmazeShowEpisodes(ctx context.Context, showID int) []tvmazeEpisode {
	var eps []tvmazeEpisode
	u := "https://api.tvmaze.com/shows/" + strconv.Itoa(showID) + "/episodes?specials=1"
	if err := getJSON(ctx, iproxy.RouteTVmaze, u, nil, &eps); err != nil {
		return nil
	}
	return eps
}
