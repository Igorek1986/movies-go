package parser

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
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
	"movies-api/config"
	"movies-api/db/models"
	"movies-api/db/store"
	"movies-api/releases"
	"movies-api/utils"
)

type KinozalParser struct {
	mu       sync.Mutex
	isParse  bool
	client   *http.Client
	loggedIn bool
}

func NewKinozal() *KinozalParser {
	return &KinozalParser{client: newHTTPClient()}
}

func (k *KinozalParser) Name() string { return "kinozal" }

// kinozal category id → (rutor_category, base models.Cat*)
// multiki/anime rows need title-based series detection.
type kzCatInfo struct {
	rutor   string
	baseCat string // empty = detect from title
}

var kinozalCats = map[string]kzCatInfo{
	"8":  {"kino", models.CatMovie},
	"9":  {"russkie", models.CatMovie},
	"10": {"serial", models.CatSeries},
	"11": {"ruserial", models.CatSeries},
	"12": {"anime", models.CatAnime},
	"14": {"multiki", ""},
	"17": {"4k", models.CatMovie},
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
		log.Printf("kinozal: login failed: %v", err)
		// continue anyway — listing doesn't require auth; only .torrent download does
	}

	var processed atomic.Int64

	for catID, catInfo := range kinozalCats {
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
	cfg := config.Get()
	if cfg.KinozalLogin == "" {
		return errors.New("KINOZAL_LOGIN not set")
	}
	form := url.Values{
		"username": {cfg.KinozalLogin},
		"password": {cfg.KinozalPassword},
		"returnto": {""},
	}
	resp, err := httpPostForm(k.client, "https://kinozal.tv/takelogin.php", form)
	if err != nil {
		return err
	}
	resp.Body.Close()
	k.loggedIn = true
	return nil
}

func (k *KinozalParser) parseCategory(catID string, catInfo kzCatInfo, fullScan bool, cutoff time.Time, processed *atomic.Int64) {
	for page := 0; ; page++ {
		link := fmt.Sprintf("https://kinozal.tv/browse.php?c=%s&page=%d", catID, page)
		body, err := httpGetBytes(k.client, link)
		if err != nil {
			log.Printf("kinozal: get %s: %v", link, err)
			return
		}
		utf8 := decodeWin1251(body)
		items := k.parseListing(utf8, catID)
		if len(items) == 0 {
			return
		}

		type enrichJob struct {
			d       *models.TorrentDetails
			isMovie bool
		}
		var toEnrich []enrichJob
		hitCutoff := false

		for _, item := range items {
			if !fullScan && !item.date.IsZero() && item.date.Before(cutoff) {
				log.Printf("kinozal: reached cutoff at %s, stopping cat %s", item.date.Format("2006-01-02"), catID)
				hitCutoff = true
				break
			}
			processed.Add(1)

			d := k.buildDetails(item, catInfo)
			if d.Categories == models.CatTVShow {
				store.CacheTorrent(d.Hash, "")
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
			// Hash unknown yet — need to download .torrent
			if d.Hash == "" {
				hash, err := k.fetchHash(item.torrentID)
				if err != nil {
					log.Printf("kinozal: hash fetch id=%s: %v", item.torrentID, err)
					continue
				}
				d.Hash = hash
				// Re-check cache with resolved hash
				cached2, cardID2 := store.TorrentStatus(hash)
				if cached2 && cardID2 != "" {
					if d.VideoQuality > 0 {
						store.UpdateQuality(cardID2, d.VideoQuality)
					}
					continue
				}
			}
			toEnrich = append(toEnrich, enrichJob{d, isMovie})
		}

		utils.PForLim(toEnrich, 20, func(_ int, job enrichJob) {
			releases.Enrich(job.d.Tracker, job.isMovie, job.d)
		})

		if hitCutoff || (!fullScan && len(items) < 50) {
			return
		}
		if fullScan && len(items) < 50 {
			return
		}
	}
}

func (k *KinozalParser) parseListing(utf8body, catID string) []kzItem {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(utf8body))
	if err != nil {
		log.Printf("kinozal: parse listing: %v", err)
		return nil
	}

	var items []kzItem
	doc.Find("tr").Each(func(_ int, row *goquery.Selection) {
		a := row.Find("a.bt")
		if a.Length() == 0 {
			return
		}
		title := strings.TrimSpace(a.Text())
		href, _ := a.Attr("href")
		torrentID := extractKzID(href)
		if torrentID == "" || title == "" {
			return
		}

		dateStr := strings.TrimSpace(row.Find("td.s").First().Text())
		date := parseKzDate(dateStr)
		seeds, _ := strconv.Atoi(strings.TrimSpace(row.Find("td.sl_s").Text()))
		peers, _ := strconv.Atoi(strings.TrimSpace(row.Find("td.sl_p").Text()))
		size := strings.TrimSpace(row.Find("td.s").Last().Text())

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
		Tracker:    "Kinozal",
		Link:       "https://kinozal.tv/details.php?id=" + item.torrentID,
		Categories: catInfo.baseCat,
	}
	parseKinozalTitle(d, item.title, catInfo)
	return d
}

func (k *KinozalParser) fetchHash(torrentID string) (string, error) {
	link := "https://kinozal.tv/download.php?id=" + torrentID
	data, err := httpGetBytes(k.client, link)
	if err != nil {
		return "", err
	}
	return infoHash(data)
}

// ─── Title parsing ────────────────────────────────────────────────────────────

var (
	reKzSeason    = regexp.MustCompile(`(?i)\(\d+\s*сезон[^)]*\)`)
	reKzSeriesHdr = regexp.MustCompile(`(?i)\d+\s*сезон|серии\s*из`)
	reKzYearRange = regexp.MustCompile(`(\d{4})-\d{4}`)
)

func parseKinozalTitle(d *models.TorrentDetails, title string, catInfo kzCatInfo) {
	parts := strings.Split(title, " / ")
	if len(parts) < 3 {
		d.Name = strings.TrimSpace(title)
		return
	}

	ruName := reKzSeason.ReplaceAllString(parts[0], "")
	d.Name = strings.TrimSpace(ruName)

	d.Names = []string{strings.TrimSpace(parts[1])}

	yearStr := strings.TrimSpace(parts[2])
	if m := reKzYearRange.FindStringSubmatch(yearStr); m != nil {
		yearStr = m[1]
	}
	d.Year, _ = strconv.Atoi(yearStr)

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
	t, err := time.ParseInLocation("02.01.2006", s, time.Local)
	if err != nil {
		return time.Time{}
	}
	return t
}

func decodeWin1251(b []byte) string {
	out, err := charmap.Windows1251.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(out)
}

// ─── Bencode info hash ────────────────────────────────────────────────────────

// infoHash extracts the BitTorrent info hash (SHA-1 of the bencoded info value)
// from raw .torrent file bytes.
func infoHash(data []byte) (string, error) {
	const marker = "4:info"
	idx := bytes.Index(data, []byte(marker))
	if idx < 0 {
		return "", errors.New("info key not found in torrent")
	}
	start := idx + len(marker)
	end, err := bencodeEnd(data, start)
	if err != nil {
		return "", fmt.Errorf("bencode parse: %w", err)
	}
	sum := sha1.Sum(data[start:end])
	return hex.EncodeToString(sum[:]), nil
}

// bencodeEnd returns the index past the end of the bencode value at pos.
func bencodeEnd(data []byte, pos int) (int, error) {
	if pos >= len(data) {
		return 0, errors.New("unexpected end of bencode data")
	}
	switch {
	case data[pos] == 'd' || data[pos] == 'l':
		pos++
		for pos < len(data) && data[pos] != 'e' {
			next, err := bencodeEnd(data, pos)
			if err != nil {
				return 0, err
			}
			pos = next
		}
		if pos >= len(data) {
			return 0, errors.New("unterminated bencode dict/list")
		}
		return pos + 1, nil
	case data[pos] == 'i':
		end := bytes.IndexByte(data[pos+1:], 'e')
		if end < 0 {
			return 0, errors.New("unterminated bencode integer")
		}
		return pos + 1 + end + 1, nil
	case data[pos] >= '0' && data[pos] <= '9':
		colon := bytes.IndexByte(data[pos:], ':')
		if colon < 0 {
			return 0, errors.New("no colon in bencode string")
		}
		n, err := strconv.Atoi(string(data[pos : pos+colon]))
		if err != nil {
			return 0, fmt.Errorf("invalid bencode string length: %w", err)
		}
		return pos + colon + 1 + n, nil
	}
	return 0, fmt.Errorf("unknown bencode type %q at offset %d", data[pos], pos)
}
