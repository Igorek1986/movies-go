// Package myshows provides a client for the MyShows JSON-RPC API
// and helpers to sync TV-show episode data into the local DB.
package myshows

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"lampa-api/config"
	"lampa-api/db/store"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// ─── JSON-RPC client ──────────────────────────────────────────────────────────

func rpc(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
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
		return nil, fmt.Errorf("myshows rpc %s: %s", method, envelope.Error.Message)
	}
	return envelope.Result, nil
}

// ─── Title normalization ──────────────────────────────────────────────────────

var multiSpace = regexp.MustCompile(`\s+`)

func normalizeTitle(s string) string {
	s = norm.NFD.String(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) {
			continue // combining diacritics
		}
		b.WriteRune(r)
	}
	s = b.String()
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")

	var out strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			out.WriteRune(r)
		}
	}
	return strings.TrimSpace(multiSpace.ReplaceAllString(out.String(), " "))
}

// ─── FindShow ─────────────────────────────────────────────────────────────────

// FindShow searches MyShows for a TV show and returns its myshows_id.
// mc must have OriginalTitle set; titleEN is the English title (may be empty).
// Returns 0 if not found.
func FindShow(ctx context.Context, mc *store.MediaCardEpInfo, titleEN string) int {
	if mc.OriginalTitle == "" {
		return 0
	}

	orig := normalizeTitle(mc.OriginalTitle)
	year := mc.Year // "2020" from first_air_date

	// Helper: normalise titleOriginal, falling back to title (like Python: titleOriginal or title).
	normShowTitle := func(titleOriginal, title string) string {
		if titleOriginal != "" {
			return normalizeTitle(titleOriginal)
		}
		return normalizeTitle(title)
	}

	// 1. Search by IMDB ID
	if mc.ImdbID != nil && *mc.ImdbID != "" {
		imdbNum := strings.TrimLeft(*mc.ImdbID, "t")
		id, _ := strconv.Atoi(imdbNum)
		if id > 0 {
			res, err := rpc(ctx, "shows.GetByExternalId", map[string]any{
				"id": id, "source": "imdb",
			})
			if err == nil {
				var show struct {
					ID            int    `json:"id"`
					TitleOriginal string `json:"titleOriginal"`
					Title         string `json:"title"`
				}
				if json.Unmarshal(res, &show) == nil && show.ID > 0 {
					found := normShowTitle(show.TitleOriginal, show.Title)
					origEN := normalizeTitle(titleEN)
					titleNorm := normalizeTitle(mc.Title)
					if found == orig || strings.Contains(found, orig) || strings.Contains(orig, found) ||
						(titleNorm != "" && (found == titleNorm || strings.Contains(found, titleNorm) || strings.Contains(titleNorm, found))) ||
						(origEN != "" && (found == origEN || strings.Contains(found, origEN) || strings.Contains(origEN, found))) {
						log.Printf("myshows: %s → show_id=%d (imdb)", mc.CardID, show.ID)
						return show.ID
					}
					log.Printf("myshows: %s imdb match rejected %q != %q / %q", mc.CardID, found, orig, origEN)
				}
			}
		}
	}

	// Helper: search GetCatalog by title+year
	type msShow struct {
		ID            int    `json:"id"`
		TitleOriginal string `json:"titleOriginal"`
		Title         string `json:"title"`
		Year          int    `json:"year"`
	}

	searchCatalog := func(query, yr string) []msShow {
		params := map[string]any{"search": map[string]any{"query": query}}
		if yr != "" {
			if y, err := strconv.Atoi(yr); err == nil {
				params["search"].(map[string]any)["year"] = y
			}
		}
		res, err := rpc(ctx, "shows.GetCatalog", params)
		if err != nil || res == nil {
			return nil
		}
		var raw []json.RawMessage
		json.Unmarshal(res, &raw) //nolint:errcheck
		var shows []msShow
		for _, item := range raw {
			var wrapped struct {
				Show msShow `json:"show"`
			}
			if json.Unmarshal(item, &wrapped) == nil && wrapped.Show.ID > 0 {
				shows = append(shows, wrapped.Show)
				continue
			}
			var s msShow
			if json.Unmarshal(item, &s) == nil && s.ID > 0 {
				shows = append(shows, s)
			}
		}
		return shows
	}

	// findIn checks normalized title match against both titleOriginal and title, with optional year.
	findIn := func(shows []msShow, norm string, yr string) int {
		matches := func(s msShow) bool {
			t := normShowTitle(s.TitleOriginal, s.Title)
			tRu := normalizeTitle(s.Title)
			return t == norm || tRu == norm
		}
		// Pass 1: exact match with year
		for _, s := range shows {
			if matches(s) && (yr == "" || strconv.Itoa(s.Year) == yr) {
				return s.ID
			}
		}
		// Pass 2: exact match without year (year may differ by 1 between TMDB and MyShows)
		if yr != "" {
			for _, s := range shows {
				if matches(s) {
					return s.ID
				}
			}
		}
		return 0
	}

	logTop := func(shows []msShow) {
		n := 3
		if len(shows) < n {
			n = len(shows)
		}
		parts := make([]string, n)
		for i, s := range shows[:n] {
			parts[i] = fmt.Sprintf("%q(%d)", normShowTitle(s.TitleOriginal, s.Title), s.Year)
		}
		log.Printf("myshows: catalog top-%d: %v", n, parts)
	}

	// 2. Search by original title + year
	shows := searchCatalog(mc.OriginalTitle, year)
	if sid := findIn(shows, orig, year); sid > 0 {
		log.Printf("myshows: %s → show_id=%d (catalog orig)", mc.CardID, sid)
		return sid
	}

	// 2b. Also search by Russian title if different from original
	titleNorm := normalizeTitle(mc.Title)
	if mc.Title != "" && mc.Title != mc.OriginalTitle {
		ruShows := searchCatalog(mc.Title, year)
		if sid := findIn(ruShows, titleNorm, year); sid > 0 {
			log.Printf("myshows: %s → show_id=%d (catalog ru)", mc.CardID, sid)
			return sid
		}
	}

	// 3. Search by English title
	if titleEN != "" {
		origEN := normalizeTitle(titleEN)
		enShows := searchCatalog(titleEN, year)
		if sid := findIn(enShows, origEN, year); sid > 0 {
			log.Printf("myshows: %s → show_id=%d (catalog en)", mc.CardID, sid)
			return sid
		}
		// Year ±1 (TMDB and MyShows sometimes differ)
		if year != "" {
			if y, err := strconv.Atoi(year); err == nil {
				for _, adj := range []string{strconv.Itoa(y - 1), strconv.Itoa(y + 1)} {
					adjShows := searchCatalog(titleEN, adj)
					if sid := findIn(adjShows, origEN, adj); sid > 0 {
						log.Printf("myshows: %s → show_id=%d (catalog en, year±1=%s)", mc.CardID, sid, adj)
						return sid
					}
				}
			}
		}
		// Short title before ":"
		if idx := strings.Index(titleEN, ":"); idx > 0 {
			short := strings.TrimSpace(titleEN[:idx])
			shortNorm := normalizeTitle(short)
			sShows := searchCatalog(short, year)
			if sid := findIn(sShows, shortNorm, year); sid > 0 {
				log.Printf("myshows: %s → show_id=%d (catalog en short)", mc.CardID, sid)
				return sid
			}
		}
	}

	logTop(shows)
	log.Printf("myshows: %s not found (orig=%q ru=%q en=%q year=%s)", mc.CardID, mc.OriginalTitle, mc.Title, titleEN, year)
	return 0
}

// ─── ShouldSync ───────────────────────────────────────────────────────────────

// ShouldSync returns true if the episode list needs refreshing.
func ShouldSync(mc *store.MediaCardEpInfo) bool {
	if mc.MyshowsID == nil {
		return false
	}
	if mc.EpisodesSyncedAt == nil {
		return true
	}
	// Completed shows — sync only once.
	if mc.NextEpAirDate == nil || *mc.NextEpAirDate == "" {
		return false
	}
	// Ongoing: re-sync if the next episode air date has passed since last sync.
	nextAir, err := time.Parse("2006-01-02", *mc.NextEpAirDate)
	if err != nil {
		return false
	}
	return nextAir.Before(time.Now()) && mc.EpisodesSyncedAt.Before(nextAir)
}

// ─── SyncEpisodes ─────────────────────────────────────────────────────────────

// SyncEpisodes fetches episode data from MyShows and upserts into the DB.
func SyncEpisodes(ctx context.Context, mc *store.MediaCardEpInfo) error {
	if mc.MyshowsID == nil {
		return fmt.Errorf("no myshows_id for %s", mc.CardID)
	}

	res, err := rpc(ctx, "shows.GetById", map[string]any{
		"showId":       *mc.MyshowsID,
		"withEpisodes": true,
	})
	if err != nil {
		return err
	}

	var show struct {
		Episodes []struct {
			ID            int     `json:"id"`
			SeasonNumber  *int    `json:"seasonNumber"`
			EpisodeNumber *int    `json:"episodeNumber"`
			Title         string  `json:"title"`
			Runtime       *int    `json:"runtime"`
			IsSpecial     bool    `json:"isSpecial"`
			AirDate       string  `json:"airDate"`
			AirDateUTC    string  `json:"airDateUTC"`
		} `json:"episodes"`
	}
	if err := json.Unmarshal(res, &show); err != nil {
		return err
	}
	if len(show.Episodes) == 0 {
		return fmt.Errorf("no episodes returned for show_id=%d", *mc.MyshowsID)
	}

	seenID := map[int]bool{}       // deduplicate by MyShows episode ID
	seenKey := map[[2]int16]bool{} // guard against PK conflicts
	specialSeq := map[int16]int16{} // season → next counter for ep-0 specials
	var rows []store.EpisodeRow

	for _, ep := range show.Episodes {
		if ep.SeasonNumber == nil || ep.ID == 0 {
			continue
		}
		if seenID[ep.ID] {
			continue
		}
		seenID[ep.ID] = true

		// Regular episodes must have an episode number and air date.
		// Specials (isSpecial:true) are kept even without an episode number or air date.
		if !ep.IsSpecial {
			if ep.EpisodeNumber == nil {
				continue
			}
			if ep.AirDate == "" && ep.AirDateUTC == "" {
				continue
			}
		}

		snum := int16(*ep.SeasonNumber)
		var enum int16
		if ep.EpisodeNumber != nil {
			enum = int16(*ep.EpisodeNumber)
		}

		// Multiple specials in the same season share episodeNumber=0 in MyShows.
		// Assign unique negative episode numbers (-1, -2, …) to avoid PK conflicts.
		if ep.IsSpecial && enum == 0 {
			specialSeq[snum]++
			enum = -specialSeq[snum]
		}

		key := [2]int16{snum, enum}
		if seenKey[key] {
			continue
		}
		seenKey[key] = true

		var airDate *time.Time
		for _, s := range []string{ep.AirDateUTC, ep.AirDate} {
			if len(s) >= 10 {
				if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
					airDate = &t
					break
				}
			}
		}

		var durSec *int
		if ep.Runtime != nil && *ep.Runtime > 0 {
			d := *ep.Runtime * 60
			durSec = &d
		}

		title := ep.Title
		var titlePtr *string
		if title != "" {
			titlePtr = &title
		}

		isSpecial := ep.IsSpecial || enum <= 0 || snum == 0
		h := EpisodeHash(int(snum), int(enum), mc.OriginalTitle)

		rows = append(rows, store.EpisodeRow{
			MyshowsEpID: ep.ID,
			Season:      snum,
			Episode:     enum,
			Title:       titlePtr,
			DurationSec: durSec,
			IsSpecial:   isSpecial,
			Hash:        h,
			AirDate:     airDate,
		})
	}

	if len(rows) == 0 {
		return fmt.Errorf("no valid episodes after filtering")
	}

	if err := store.UpsertEpisodes(ctx, mc.TmdbID, rows); err != nil {
		return fmt.Errorf("upsert episodes: %w", err)
	}

	log.Printf("myshows: synced %d episodes for %s", len(rows), mc.CardID)
	return nil
}

// ─── Hash helpers ─────────────────────────────────────────────────────────────

// EpisodeHash mirrors Lampa.Utils.hash(buildEpisodeHashString(s, e, origTitle)).
func EpisodeHash(season, episode int, origTitle string) string {
	var s string
	if season > 10 {
		s = fmt.Sprintf("%d:%d%s", season, episode, origTitle)
	} else {
		s = fmt.Sprintf("%d%d%s", season, episode, origTitle)
	}
	return lampaHash(s)
}

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
