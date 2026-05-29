package parser

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PuerkitoBio/goquery"
	"movies-api/db/models"
	"movies-api/db/store"
	"movies-api/releases"
)

func getNNMClubHost() string {
	if v, ok := store.GetSetting(context.Background(), "nnmclub_host"); ok && v != "" {
		return v
	}
	return "https://nnmclub.to"
}


type NNMClubParser struct {
	mu      sync.Mutex
	isParse bool
}

func NewNNMClub() *NNMClubParser { return &NNMClubParser{} }

func (n *NNMClubParser) httpClient() *http.Client {
	return clientForRoute("parser_nnmclub")
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

	fullScan, cutoff := scanCutoff("nnmclub")

	var processed atomic.Int64

	var wg sync.WaitGroup
	for catID, catInfo := range nnmClubCats {
		catID, catInfo := catID, catInfo
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.parseCategory(catID, catInfo, fullScan, cutoff, &processed)
		}()
	}
	wg.Wait()

	commitScan("nnmclub", &processed)
}

func (n *NNMClubParser) parseCategory(catID string, catInfo nnmCatInfo, fullScan bool, cutoff time.Time, processed *atomic.Int64) {
	label := "nnmclub/" + catID
	// NNMClub paginates with start= offset (multiples of 50), not page index.
	// Pseudo-hash "nnm_"+topicID avoids per-torrent topic page fetches.
	runPageLoop(n.httpClient(), label, 20, 50,
		func(page int) string {
			return fmt.Sprintf(getNNMClubHost()+"/forum/viewforum.php?f=%s&start=%d", catID, page*50)
		},
		func(body []byte) ([]enrichJob, bool, int) {
			items := n.parseListing(decodeWin1251(body), catID)
			var jobs []enrichJob
			for _, item := range items {
				if !fullScan && !item.date.IsZero() && item.date.Before(cutoff) {
					log.Printf("%s: reached cutoff at %s", label, item.date.Format("2006-01-02"))
					return jobs, true, len(items)
				}
				processed.Add(1)
				d := n.buildDetails(item, catInfo)
				if d.Categories == models.CatTVShow {
					continue
				}
				isMovie := isMovieCat(d.Categories)
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

		dateStr := strings.TrimSpace(row.Find(`td.row2[valign="middle"]`).First().Text())
		date := parseRuDate(dateStr)

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
		Link:       getNNMClubHost() + "/forum/viewtopic.php?t=" + item.topicID,
		Hash:       "nnm_" + item.topicID,
		Categories: catInfo.baseCat,
	}
	parseNNMTitle(d, item.title, catInfo)
	return d
}


// ─── Title parsing ────────────────────────────────────────────────────────────

var reNNMSeriesHdr = regexp.MustCompile(`(?i)\d+\s*сезон|серии\s*из|Season\s+\d`)

func parseNNMTitle(d *models.TorrentDetails, title string, catInfo nnmCatInfo) {
	ParseTorrentTitle(d, title)
	if catInfo.baseCat == "" {
		if reNNMSeriesHdr.MatchString(title) || HasEpisodeBrackets(title) {
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

