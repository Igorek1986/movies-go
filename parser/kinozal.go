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
	"movies-api/releases"
)

func getKinozalHost() string {
	if v, ok := store.GetSetting(context.Background(), "kinozal_host"); ok && v != "" {
		return v
	}
	return "https://kinozal.tv"
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
// multiki/anime rows need title-based series detection.
type kzCatInfo struct {
	baseCat string // empty = detect from title
}

var kinozalCats = map[string]kzCatInfo{
	"8":  {models.CatMovie},
	"9":  {models.CatMovie},
	"10": {models.CatSeries},
	"11": {models.CatSeries},
	"12": {models.CatAnime},
	"14": {""},
	"17": {models.CatMovie},
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

	lastParsed := store.LastParsedAtFor("kinozal")
	fullScan := lastParsed.IsZero()
	var cutoff time.Time
	if !fullScan {
		overlapDays := 2
		cutoff = lastParsed.Add(-time.Duration(overlapDays) * 24 * time.Hour)
		log.Printf("kinozal: incremental scan, cutoff %s", cutoff.Format("2006-01-02"))
	} else {
		log.Println("kinozal: first run — full scan")
	}

	if err := k.login(); err != nil {
		log.Printf("kinozal: no auth: %v", err)
		// continue anyway — listing doesn't require auth; only .torrent download does
	}

	var processed atomic.Int64

	for catID, catInfo := range kinozalCats {
		if stopRequest.Load() {
			log.Println("kinozal: stop requested")
			break
		}
		catID, catInfo := catID, catInfo
		k.parseCategory(catID, catInfo, fullScan, cutoff, &processed)
	}

	if n := processed.Load(); n > 0 {
		store.SetLastParsedAtFor("kinozal")
		log.Printf("kinozal: scan complete, processed %d torrents", n)
	} else {
		log.Println("kinozal: scan complete, no torrents processed — last_parsed_at not updated")
	}
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
	runPageLoop(k.httpClient(), "kinozal", 20, 50,
		func(page int) string {
			return fmt.Sprintf(getKinozalHost()+"/browse.php?c=%s&page=%d", catID, page)
		},
		func(body []byte) ([]enrichJob, bool, int) {
			items := k.parseListing(decodeWin1251(body), catID)
			var jobs []enrichJob
			for _, item := range items {
				if !fullScan && !item.date.IsZero() && item.date.Before(cutoff) {
					log.Printf("kinozal: reached cutoff at %s, stopping cat %s", item.date.Format("2006-01-02"), catID)
					return jobs, true, len(items)
				}
				processed.Add(1)
				d := k.buildDetails(item, catInfo)
				if d.Categories == models.CatTVShow {
					store.CacheTorrent(d.Hash, "", "kinozal")
					continue
				}
				isMovie := d.Categories == models.CatMovie ||
					d.Categories == models.CatDocMovie ||
					d.Categories == models.CatCartoonMovie
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
			releases.Enrich(job.d.Tracker, job.isMovie, job.d)
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
	reKzSeason    = regexp.MustCompile(`(?i)\(\d+\s*сезон[^)]*\)`)
	reKzSeriesHdr = regexp.MustCompile(`(?i)\d+\s*сезон|серии\s*из`)
	reKzYearRange = regexp.MustCompile(`(\d{4})-\d{4}`)
)

func parseKinozalTitle(d *models.TorrentDetails, title string, catInfo kzCatInfo) {
	parts := strings.Split(title, " / ")
	if len(parts) < 2 {
		d.Name = strings.TrimSpace(title)
		return
	}

	ruName := reKzSeason.ReplaceAllString(parts[0], "")
	d.Name = strings.TrimSpace(ruName)

	// Detect whether parts[1] is an English title or the year.
	// "Начало / Inception / 2010 / ..."  → parts[1] = English title
	// "Капитанская дочка / 1958 / РУ / ..." → parts[1] = year (no English title)
	p1 := strings.TrimSpace(parts[1])
	if yr, err := strconv.Atoi(p1); err == nil && yr >= 1900 && yr <= 2100 {
		// parts[1] is the year — no English title in this entry
		d.Year = yr
	} else {
		d.Names = []string{p1}
		if len(parts) >= 3 {
			yearStr := strings.TrimSpace(parts[2])
			if m := reKzYearRange.FindStringSubmatch(yearStr); m != nil {
				yearStr = m[1]
			}
			d.Year, _ = strconv.Atoi(yearStr)
		}
	}

	qualPart := parts[len(parts)-1]
	d.VideoQuality = ParseVQuality(qualPart)
	d.AudioQuality = ParseAQuality(qualPart)

	// Category detection for multiki (detect series from title)
	if catInfo.baseCat == "" {
		if reKzSeriesHdr.MatchString(title) {
			d.Categories = models.CatCartoonSeries
		} else {
			d.Categories = models.CatCartoonMovie
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

