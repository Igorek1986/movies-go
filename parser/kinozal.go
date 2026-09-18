package parser

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/text/encoding/charmap"
	"movies-api/db/models"
	"movies-api/db/store"
	"movies-api/internal/cfbypass"
	"movies-api/releases"
)

func getKinozalHost() string {
	if v, ok := store.GetSetting(context.Background(), "kinozal_host"); ok && v != "" {
		return v
	}
	return "https://kinozal.me"
}

type KinozalParser struct {
	mu       sync.Mutex
	isParse  bool
	loggedIn bool
}

func NewKinozal() *KinozalParser { return &KinozalParser{} }

func (k *KinozalParser) httpClient() *http.Client {
	return clientForRoute("parser_kinozal")
}

func (k *KinozalParser) Name() string { return "kinozal" }

// kinozal category id → (category, base models.Cat*)
// multiki/anime/"сериал" rows need title-based movie/series detection —
// kinozal's own category name is not a reliable signal (see 45/46 below).
type kzCatInfo struct {
	baseCat   string // non-empty = always this category, no title detection
	movieCat  string // baseCat=="" only: category assigned when title has no season/episode markers
	seriesCat string // baseCat=="" only: category assigned when title has season/episode markers
}

var kinozalCats = map[string]kzCatInfo{
	"8":  {baseCat: models.CatMovie},
	"9":  {baseCat: models.CatMovie},
	"10": {baseCat: models.CatMovie}, // was mislabeled Series — live sample: 86% standalone films, no season/episode markers (see dev/kinozal.md)
	"11": {baseCat: models.CatMovie}, // was mislabeled Series — live sample: 99% standalone films (Cutthroat Island, A Fistful of Dollars, ...)
	"12": {baseCat: models.CatAnime},
	"14": {movieCat: models.CatCartoonMovie, seriesCat: models.CatCartoonSeries},
	"17": {baseCat: models.CatMovie},
	// "Сериал" in the name is misleading — live sample (parser_not_found,
	// full scan) found ~21% (392/1833) of not_found entries from these two
	// categories are standalone movies with zero season/episode markers in
	// the title (Голый пистолет 2025, Чебурашка 2, Буратино 2025, Сказка о
	// царе Салтане 2025, Сто лет тому вперёд...), all verified to exist in
	// TMDB as movies. Forcing CatSeries sent them through search/tv, where
	// they can never match. Same title-based detection as cat 14 fixes it.
	"45": {movieCat: models.CatMovie, seriesCat: models.CatSeries}, // "Сериал - Русский"
	"46": {movieCat: models.CatMovie, seriesCat: models.CatSeries}, // "Сериал - Буржуйский"
}

type kzItem struct {
	torrentID string
	title     string
	date      time.Time
	seeds     int
	peers     int
	size      string
	cat       string // kinozal category id
}

func (k *KinozalParser) Parse() {
	k.mu.Lock()
	if k.isParse {
		k.mu.Unlock()
		return
	}
	k.isParse = true
	defer func() { k.isParse = false }()
	k.mu.Unlock()

	fullScan, cutoff := scanCutoff("kinozal")

	if cfbypass.Enabled() {
		log.Printf("kinozal: Cloudflare bypass enabled (FlareSolverr/cffetch)")
	}

	if err := k.login(); err != nil {
		log.Printf("kinozal: no auth: %v", err)
		// continue anyway — listing doesn't require auth; only .torrent download does
	}

	var processed atomic.Int64

	var wg sync.WaitGroup
	for catID, catInfo := range kinozalCats {
		catID, catInfo := catID, catInfo
		wg.Add(1)
		go func() {
			defer wg.Done()
			k.parseCategory(catID, catInfo, fullScan, cutoff, &processed)
		}()
	}
	wg.Wait()

	commitScan("kinozal", &processed)
}

func (k *KinozalParser) login() error {
	login, _ := store.GetSetting(context.Background(), "kinozal_login")
	password, _ := store.GetSetting(context.Background(), "kinozal_password")
	if login == "" {
		return errors.New("Kinozal: логин не задан (укажите в настройках парсеров)")
	}
	form := url.Values{
		"username": {login},
		"password": {password},
		"touser":   {"1"},
		"wact":     {"takerecover"},
	}
	resp, err := httpPostForm(k.httpClient(), getKinozalHost()+"/takelogin.php", form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.Request != nil && strings.Contains(resp.Request.URL.Path, "login") {
		return errors.New("login failed — redirected to login page")
	}
	k.loggedIn = true
	log.Printf("kinozal: logged in as %s", login)
	return nil
}

func (k *KinozalParser) parseCategory(catID string, catInfo kzCatInfo, fullScan bool, cutoff time.Time, processed *atomic.Int64) {
	label := "kinozal/" + catID
	// cfbypass returns pages already decoded to UTF-8 (FlareSolverr's browser
	// and cffetch both decode per the page's own charset before handing the
	// body back) — decodeWin1251 must be skipped, or it re-mangles that text.
	fetch := clientFetch(k.httpClient())
	decode := decodeWin1251
	if cfbypass.Enabled() {
		fetch = cfbypass.Get
		decode = func(b []byte) string { return string(b) }
	}
	runPageLoop(fetch, label, 20, 50,
		func(page int) string {
			return fmt.Sprintf(getKinozalHost()+"/browse.php?c=%s&page=%d", catID, page)
		},
		func(body []byte) ([]enrichJob, bool, int) {
			items := k.parseListing(decode(body), catID)
			var jobs []enrichJob
			for _, item := range items {
				if !fullScan && !item.date.IsZero() && item.date.Before(cutoff) {
					log.Printf("%s: reached cutoff at %s", label, item.date.Format("2006-01-02"))
					return jobs, true, len(items)
				}
				processed.Add(1)
				d := k.buildDetails(item, catInfo)
				if d.Categories == models.CatTVShow {
					store.CacheTorrent(d.Hash, "", "kinozal", d.CreateDate)
					continue
				}
				isMovie := isMovieCat(d.Categories, item.title)
				cached, cardID := store.TorrentStatus(d.Hash)
				if cached && cardID != "" {
					if d.VideoQuality > 0 {
						store.UpdateQuality(cardID, d.VideoQuality)
					}
					continue
				}
				jobs = append(jobs, enrichJob{d, isMovie})
			}
			return jobs, false, len(items)
		},
		func(job enrichJob) {
			releases.Enrich(label, job.isMovie, job.d)
		},
	)
}

func (k *KinozalParser) parseListing(utf8body, catID string) []kzItem {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(utf8body))
	if err != nil {
		log.Printf("kinozal: parse listing: %v", err)
		return nil
	}

	var items []kzItem
	doc.Find("tr").Each(func(_ int, row *goquery.Selection) {
		a := row.Find("td.nam a")
		if a.Length() == 0 {
			return
		}
		title := strings.TrimSpace(a.Text())
		href, _ := a.Attr("href")
		torrentID := extractKzID(href)
		if torrentID == "" || title == "" {
			return
		}

		tds := row.Find("td.s")
		dateStr := strings.TrimSpace(tds.Last().Text())
		date := parseKzDate(dateStr)
		seeds, _ := strconv.Atoi(strings.TrimSpace(row.Find("td.sl_s").Text()))
		peers, _ := strconv.Atoi(strings.TrimSpace(row.Find("td.sl_p").Text()))
		size := strings.TrimSpace(tds.Eq(1).Text())

		items = append(items, kzItem{
			torrentID: torrentID,
			title:     title,
			date:      date,
			seeds:     seeds,
			peers:     peers,
			size:      size,
			cat:       catID,
		})
	})
	return items
}

func (k *KinozalParser) buildDetails(item kzItem, catInfo kzCatInfo) *models.TorrentDetails {
	d := &models.TorrentDetails{
		Title:      item.title,
		Size:       item.size,
		Seed:       item.seeds,
		Peer:       item.peers,
		CreateDate: item.date,
		Tracker:    "kinozal",
		Link:       getKinozalHost()+"/details.php?id=" + item.torrentID,
		Hash:       "kz_" + item.torrentID, // pseudo-hash for deduplication; .torrent not publicly downloadable
		Categories: catInfo.baseCat,
	}
	parseKinozalTitle(d, item.title, catInfo)
	return d
}

// ─── Title parsing ────────────────────────────────────────────────────────────

var (
	// Matches any "(...)" containing "сезон"/"серии" anywhere inside, not just
	// "(N сезон...)" — kinozal also writes plural ranges ("1-2 сезоны: ...")
	// and season-less episode counts ("1-10 серии из 10") that a season-number-
	// first pattern misses, leaving the descriptor stuck to d.Name and
	// breaking TMDB name matching (see dev/kinozal.md).
	reKzSeason    = regexp.MustCompile(`(?i)\([^)]*(?:сезон|серии)[^)]*\)`)
	reKzSeriesHdr = regexp.MustCompile(`(?i)\d+\s*сезон|серии\s*из`)
)

func parseKinozalTitle(d *models.TorrentDetails, title string, catInfo kzCatInfo) {
	ParseTorrentTitle(d, title)
	d.Name = strings.TrimSpace(reKzSeason.ReplaceAllString(d.Name, ""))
	if catInfo.baseCat == "" {
		if reKzSeriesHdr.MatchString(title) || HasEpisodeBrackets(title) {
			d.Categories = catInfo.seriesCat
		} else {
			d.Categories = catInfo.movieCat
		}
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

var reKzID = regexp.MustCompile(`id=(\d+)`)

func extractKzID(href string) string {
	m := reKzID.FindStringSubmatch(href)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func parseKzDate(s string) time.Time {
	s = strings.ToLower(strings.TrimSpace(s))
	now := time.Now()
	switch {
	case strings.HasPrefix(s, "сегодня"):
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	case strings.HasPrefix(s, "вчера"):
		y := now.AddDate(0, 0, -1)
		return time.Date(y.Year(), y.Month(), y.Day(), 0, 0, 0, 0, time.Local)
	default:
		// "16.04.2026 в 06:40"
		parts := strings.SplitN(s, " ", 2)
		t, err := time.ParseInLocation("02.01.2006", parts[0], time.Local)
		if err != nil {
			return time.Time{}
		}
		return t
	}
}

func decodeWin1251(b []byte) string {
	out, err := charmap.Windows1251.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(out)
}

