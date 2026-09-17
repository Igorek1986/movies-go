package parser

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"movies-api/db/models"
	"movies-api/db/store"
	"movies-api/internal/cfbypass"
	"movies-api/releases"
)

// rutracker.org sits behind a heavier Cloudflare challenge than kinozal —
// FlareSolverr needs ~1-3GB RAM to solve it without the browser tab crashing
// (see docker-compose.cfbypass.yml comment) and up to a few minutes on a cold
// session, vs kinozal's usual 10-45s. Same internal/cfbypass mechanism
// otherwise (FlareSolverr session + cffetch fast path once solved).
//
// magnet/real info-hash is deliberately NOT fetched — nothing in this
// project persists or uses it (models.TorrentDetails.Magnet is write-only,
// no `magnet` column anywhere), so unlike a from-scratch tracker we don't
// need rutracker's second per-torrent request (topic page) at all: a
// pseudo-hash from the topic ID (same trick as kinozal/nnmclub) is enough
// for dedup, and the listing page alone has everything else (title, date,
// seeds/peers, size).

func getRutrackerHost() string {
	if v, ok := store.GetSetting(context.Background(), "rutracker_host"); ok && v != "" {
		return v
	}
	return "https://rutracker.org"
}

type RutrackerParser struct {
	mu      sync.Mutex
	isParse bool
}

func NewRutracker() *RutrackerParser { return &RutrackerParser{} }

func (r *RutrackerParser) Name() string { return "rutracker" }

func (r *RutrackerParser) httpClient() *http.Client {
	return clientForRoute("parser_rutracker")
}

// rutracker forum id → base category. A small, hand-picked starting set
// (rutracker has ~250 forums total, jacred's reference map) rather than the
// full tree — easy to extend once this proves worth keeping.
type rtCatInfo struct {
	baseCat string
}

var rutrackerCats = map[string]rtCatInfo{
	"7":    {models.CatMovie}, // Зарубежное кино
	"1666": {models.CatMovie},
	"941":  {models.CatMovie},
	"2090": {models.CatMovie},
	"84":   {models.CatCartoonMovie},  // Мультфильмы
	"921":  {models.CatCartoonSeries}, // Мультсериалы
	"842":  {models.CatSeries},        // Сериалы
	"235":  {models.CatSeries},
	"1105": {models.CatAnime},    // Аниме
	"709":  {models.CatDocMovie}, // Документальные фильмы
}

type rtItem struct {
	topicID string
	title   string
	date    time.Time
	seeds   int
	peers   int
	size    string
}

func (r *RutrackerParser) Parse() {
	r.mu.Lock()
	if r.isParse {
		r.mu.Unlock()
		return
	}
	r.isParse = true
	defer func() { r.isParse = false }()
	r.mu.Unlock()

	fullScan, cutoff := scanCutoff("rutracker")

	if !cfbypass.Enabled() {
		log.Printf("rutracker: Cloudflare bypass not configured — rutracker.org will just fail every request")
	}

	var processed atomic.Int64

	var wg sync.WaitGroup
	for catID, catInfo := range rutrackerCats {
		catID, catInfo := catID, catInfo
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.parseCategory(catID, catInfo, fullScan, cutoff, &processed)
		}()
	}
	wg.Wait()

	commitScan("rutracker", &processed)
}

func (r *RutrackerParser) parseCategory(catID string, catInfo rtCatInfo, fullScan bool, cutoff time.Time, processed *atomic.Int64) {
	label := "rutracker/" + catID
	fetch := clientFetch(r.httpClient())
	if cfbypass.Enabled() {
		fetch = cfbypass.Get
	}
	runPageLoop(fetch, label, 20, 50,
		func(page int) string {
			return fmt.Sprintf(getRutrackerHost()+"/forum/viewforum.php?f=%s&start=%d", catID, page*50)
		},
		func(body []byte) ([]enrichJob, bool, int) {
			items := r.parseListing(string(body))
			var jobs []enrichJob
			for _, item := range items {
				if !fullScan && !item.date.IsZero() && item.date.Before(cutoff) {
					log.Printf("%s: reached cutoff at %s", label, item.date.Format("2006-01-02"))
					return jobs, true, len(items)
				}
				processed.Add(1)
				d := r.buildDetails(item, catInfo)
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

// ─── Listing parsing ────────────────────────────────────────────────────────

// Real torrent rows are wrapped in `<div class="torTopic">` (exact, no other
// classes) — sticky/announcement rows use `torTopic bold tt-text` on the
// inner <a> instead and lack seed/size fields entirely, so they fall out
// naturally when those fields fail to match below (same filter jacred's
// parser uses — see dev/jacred's RutrackerParser.cs TryParseRowFields).
const rtRowMarker = `class="torTopic"`

var (
	reRtDate  = regexp.MustCompile(`<p>(\d{4}-\d{2}-\d{2} \d{2}:\d{2})</p>`)
	reRtTopic = regexp.MustCompile(`<a id="tt-(\d+)"[^>]*>([^\n\r]+)</a>`)
	reRtTag   = regexp.MustCompile(`<[^>]+>`)
	reRtSeed  = regexp.MustCompile(`class="seedmed"[^>]*><b>(\d+)</b>`)
	reRtPeer  = regexp.MustCompile(`class="leechmed"[^>]*><b>(\d+)</b>`)
	reRtSize  = regexp.MustCompile(`dl-stub">([^<]+)</a>`)
)

func (r *RutrackerParser) parseListing(body string) []rtItem {
	var items []rtItem
	rows := strings.Split(body, rtRowMarker)
	if len(rows) < 2 {
		return items
	}
	for _, row := range rows[1:] {
		dm := reRtDate.FindStringSubmatch(row)
		if dm == nil {
			continue
		}
		date, err := time.ParseInLocation("2006-01-02 15:04", dm[1], time.Local)
		if err != nil {
			continue
		}

		tm := reRtTopic.FindStringSubmatch(row)
		if tm == nil {
			continue
		}
		topicID := tm[1]
		title := strings.TrimSpace(reRtTag.ReplaceAllString(tm[2], ""))
		if topicID == "" || title == "" {
			continue
		}

		sm := reRtSeed.FindStringSubmatch(row)
		pm := reRtPeer.FindStringSubmatch(row)
		zm := reRtSize.FindStringSubmatch(row)
		if sm == nil || pm == nil || zm == nil {
			// No seed/peer/size — a sticky/announcement/closed thread, not a
			// real torrent.
			continue
		}
		seeds, _ := strconv.Atoi(sm[1])
		peers, _ := strconv.Atoi(pm[1])
		size := strings.ReplaceAll(zm[1], " ", " ")

		items = append(items, rtItem{
			topicID: topicID,
			title:   title,
			date:    date,
			seeds:   seeds,
			peers:   peers,
			size:    size,
		})
	}
	return items
}

func (r *RutrackerParser) buildDetails(item rtItem, catInfo rtCatInfo) *models.TorrentDetails {
	d := &models.TorrentDetails{
		Title:      item.title,
		Size:       item.size,
		Seed:       item.seeds,
		Peer:       item.peers,
		CreateDate: item.date,
		Tracker:    "rutracker",
		Link:       getRutrackerHost() + "/forum/viewtopic.php?t=" + item.topicID,
		Hash:       "rt_" + item.topicID, // pseudo-hash; real info-hash needs a topic-page fetch we don't do (see file header)
		Categories: catInfo.baseCat,
	}
	ParseTorrentTitle(d, item.title)
	return d
}
