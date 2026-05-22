package parser

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PuerkitoBio/goquery"
	"movies-api/db/models"
	"movies-api/db/store"
	"movies-api/releases"
)

type NNMClubParser struct {
	mu           sync.Mutex
	isParse      bool
	client       *http.Client // proxy
	directClient *http.Client // fallback without proxy
}

func NewNNMClub() *NNMClubParser {
	return &NNMClubParser{
		client:       newHTTPClient(),
		directClient: newDirectHTTPClient(),
	}
}

func (n *NNMClubParser) Name() string { return "nnmclub" }

type nnmCatInfo struct {
	baseCat string // empty = detect from title
}

var nnmClubCats = map[string]nnmCatInfo{
	"219":  {models.CatMovie},
	"954":  {models.CatMovie},
	"882":  {models.CatMovie},
	"227":  {models.CatMovie},
	"1296": {models.CatMovie},
	"768":  {models.CatSeries},
	"769":  {models.CatSeries},
	"621":  {models.CatAnime},
	"625":  {models.CatAnime},
	"1338": {""},
	"1332": {""},
}

type nnmItem struct {
	topicID string
	title   string
	date    time.Time
	cat     string
}

func (n *NNMClubParser) Parse() {
	n.mu.Lock()
	if n.isParse {
		n.mu.Unlock()
		return
	}
	n.isParse = true
	defer func() { n.isParse = false }()
	n.mu.Unlock()

	lastParsed := store.LastParsedAtFor("nnmclub")
	fullScan := lastParsed.IsZero()
	var cutoff time.Time
	if !fullScan {
		overlapDays := 2
		cutoff = lastParsed.Add(-time.Duration(overlapDays) * 24 * time.Hour)
		log.Printf("nnmclub: incremental scan, cutoff %s", cutoff.Format("2006-01-02"))
	} else {
		log.Println("nnmclub: first run — full scan")
	}

	var processed atomic.Int64

	for catID, catInfo := range nnmClubCats {
		if stopRequest.Load() {
			log.Println("nnmclub: stop requested")
			break
		}
		catID, catInfo := catID, catInfo
		n.parseCategory(catID, catInfo, fullScan, cutoff, &processed)
	}

	if total := processed.Load(); total > 0 {
		store.SetLastParsedAtFor("nnmclub")
		log.Printf("nnmclub: scan complete, processed %d torrents", total)
	} else {
		log.Println("nnmclub: scan complete, no torrents processed — last_parsed_at not updated")
	}
}

func (n *NNMClubParser) parseCategory(catID string, catInfo nnmCatInfo, fullScan bool, cutoff time.Time, processed *atomic.Int64) {
	// NNMClub paginates with start= offset (multiples of 50), not page index.
	// We abuse the page argument as offset/50 and multiply inside buildURL.
	// Concurrency=2: NNMClub rate-limits aggressive parallel requests.
	runPageLoop(n.client, n.directClient, "nnmclub", 2, 50,
		func(page int) string {
			return fmt.Sprintf("https://nnmclub.to/forum/viewforum.php?f=%s&start=%d", catID, page*50)
		},
		func(body []byte) ([]enrichJob, bool, int) {
			items := n.parseListing(decodeWin1251(body), catID)
			var jobs []enrichJob
			for _, item := range items {
				if !fullScan && !item.date.IsZero() && item.date.Before(cutoff) {
					log.Printf("nnmclub: reached cutoff at %s, stopping cat %s", item.date.Format("2006-01-02"), catID)
					return jobs, true, len(items)
				}
				processed.Add(1)
				d := n.buildDetails(item, catInfo)
				if d.Categories == models.CatTVShow {
					continue
				}
				isMovie := d.Categories == models.CatMovie ||
					d.Categories == models.CatDocMovie ||
					d.Categories == models.CatCartoonMovie
				// Hash unknown until topic page is fetched — always enqueue.
				jobs = append(jobs, enrichJob{d, isMovie})
			}
			return jobs, false, len(items)
		},
		func(job enrichJob) {
			d := job.d
			hash, magnet, err := n.fetchHashFromTopic(d.Link)
			if err != nil {
				log.Printf("nnmclub: topic fetch %s: %v", d.Link, err)
				return
			}
			d.Hash = hash
			d.Magnet = magnet
			cached, cardID := store.TorrentStatus(hash)
			if cached && cardID != "" {
				if d.VideoQuality > 0 {
					store.UpdateQuality(cardID, d.VideoQuality)
				}
				return
			}
			releases.Enrich(d.Tracker, job.isMovie, d)
		},
	)
}

func (n *NNMClubParser) parseListing(utf8body, catID string) []nnmItem {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(utf8body))
	if err != nil {
		log.Printf("nnmclub: parse listing: %v", err)
		return nil
	}

	var items []nnmItem
	doc.Find("tr").Each(func(_ int, row *goquery.Selection) {
		a := row.Find("a.topictitle").First()
		if a.Length() == 0 {
			return
		}
		title := strings.TrimSpace(a.Text())
		href, _ := a.Attr("href")
		topicID := extractNNMTopicID(href)
		if topicID == "" || title == "" {
			return
		}

		dateStr := strings.TrimSpace(row.Find("td.vf").First().Text())
		date := parseNNMDate(dateStr)

		items = append(items, nnmItem{
			topicID: topicID,
			title:   title,
			date:    date,
			cat:     catID,
		})
	})
	return items
}

func (n *NNMClubParser) buildDetails(item nnmItem, catInfo nnmCatInfo) *models.TorrentDetails {
	d := &models.TorrentDetails{
		Title:      item.title,
		CreateDate: item.date,
		Tracker:    "nnmclub",
		Link:       "https://nnmclub.to/forum/viewtopic.php?t=" + item.topicID,
		Categories: catInfo.baseCat,
	}
	parseNNMTitle(d, item.title, catInfo)
	return d
}

var nnmRateLimitMsg = []byte("\xd1\xeb\xe8\xf8\xea\xee\xec \xec\xed\xee\xe3\xee") // "Слишком много" in cp1251

var (
	reMagnet = regexp.MustCompile(`(?i)(magnet:\?xt=urn:btih:[a-z0-9]{32,40}[^"' ]*)`)
	reHash   = regexp.MustCompile(`(?i)xt=urn:btih:([a-z0-9]{32,40})`)
)

func (n *NNMClubParser) fetchHashFromTopic(topicURL string) (hash, magnet string, err error) {
	const (
		maxAttempts = 10
		ratio       = 1.5
		maxWait     = 5 * time.Minute
	)
	rlWait  := 60 * time.Second // rate-limit backoff
	netWait := 5 * time.Second  // network error backoff

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Try proxy, fall back to direct on network error
		body, fetchErr := httpGetBytes(n.client, topicURL)
		if fetchErr != nil {
			body2, directErr := httpGetBytes(n.directClient, topicURL)
			if directErr == nil {
				body, fetchErr = body2, nil
			}
		}

		if fetchErr != nil {
			err = fetchErr
			if attempt+1 >= maxAttempts {
				break
			}
			w := netWait
			if w > maxWait {
				w = maxWait
			}
			log.Printf("nnmclub: сеть недоступна, повтор %d/%d через %s", attempt+1, maxAttempts, w.Round(time.Second))
			time.Sleep(w)
			netWait = time.Duration(float64(netWait) * ratio)
			continue
		}

		if bytes.Contains(body, nnmRateLimitMsg) {
			if attempt+1 >= maxAttempts {
				break
			}
			w := rlWait
			if w > maxWait {
				w = maxWait
			}
			log.Printf("nnmclub: rate limit, повтор %d/%d через %s", attempt+1, maxAttempts, w.Round(time.Second))
			time.Sleep(w)
			rlWait = time.Duration(float64(rlWait) * ratio)
			continue
		}

		utf8 := decodeWin1251(body)
		doc, parseErr := goquery.NewDocumentFromReader(strings.NewReader(utf8))
		if parseErr == nil {
			doc.Find("a").Each(func(_ int, s *goquery.Selection) {
				if magnet != "" {
					return
				}
				if href, _ := s.Attr("href"); strings.HasPrefix(href, "magnet:") {
					magnet = href
				}
			})
		}
		if magnet == "" {
			if m := reMagnet.FindStringSubmatch(utf8); len(m) > 1 {
				magnet = m[1]
			}
		}
		if magnet != "" {
			break
		}
		return "", "", fmt.Errorf("magnet not found on topic page")
	}

	if magnet == "" {
		if err != nil {
			return "", "", fmt.Errorf("сеть недоступна после %d попыток: %w", maxAttempts, err)
		}
		return "", "", fmt.Errorf("rate limit после %d попыток", maxAttempts)
	}

	m := reHash.FindStringSubmatch(magnet)
	if len(m) < 2 {
		return "", "", fmt.Errorf("hash not found in magnet %q", magnet)
	}
	return strings.ToLower(m[1]), magnet, nil
}

// ─── Title parsing ────────────────────────────────────────────────────────────

var (
	reNNMSeriesHdr = regexp.MustCompile(`(?i)\d+\s*сезон|серии\s*из|Season\s+\d`)
	reNNMYearRange = regexp.MustCompile(`(\d{4})-\d{4}`)
)

func parseNNMTitle(d *models.TorrentDetails, title string, catInfo nnmCatInfo) {
	parts := strings.Split(title, " / ")
	if len(parts) < 2 {
		d.Name = strings.TrimSpace(title)
		return
	}

	d.Name = strings.TrimSpace(parts[0])
	d.Names = []string{strings.TrimSpace(parts[1])}

	if len(parts) >= 3 {
		yearStr := strings.TrimSpace(parts[2])
		if m := reNNMYearRange.FindStringSubmatch(yearStr); m != nil {
			yearStr = m[1]
		}
		// yearStr may also contain extra info after the year
		yearStr = strings.Fields(yearStr)[0]
		d.Year, _ = strconv.Atoi(yearStr)
	}

	qualPart := parts[len(parts)-1]
	d.VideoQuality = ParseVQuality(qualPart)
	d.AudioQuality = ParseAQuality(qualPart)

	if catInfo.baseCat == "" {
		if reNNMSeriesHdr.MatchString(title) {
			d.Categories = models.CatCartoonSeries
		} else {
			d.Categories = models.CatCartoonMovie
		}
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

var reNNMTopicID = regexp.MustCompile(`[?&]t=(\d+)`)

func extractNNMTopicID(href string) string {
	m := reNNMTopicID.FindStringSubmatch(href)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

var nnmMonths = map[string]int{
	"Янв": 1, "Фев": 2, "Мар": 3, "Апр": 4, "Май": 5, "Июн": 6,
	"Июл": 7, "Авг": 8, "Сен": 9, "Окт": 10, "Ноя": 11, "Дек": 12,
}

func parseNNMDate(s string) time.Time {
	parts := strings.Fields(s)
	if len(parts) < 3 {
		return time.Time{}
	}
	day, _ := strconv.Atoi(parts[0])
	monthNum := nnmMonths[parts[1]]
	year, _ := strconv.Atoi(parts[2])
	if day == 0 || monthNum == 0 || year == 0 {
		return time.Time{}
	}
	return time.Date(year, time.Month(monthNum), day, 0, 0, 0, 0, time.Local)
}
