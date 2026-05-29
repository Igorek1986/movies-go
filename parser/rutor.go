package parser

import (
	"bytes"
	"context"
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
	"movies-api/tasker"
	"movies-api/utils"
)

func loadRutorHost() string {
	if v, ok := store.GetSetting(context.Background(), "rutor_host"); ok && v != "" {
		return v
	}
	return "http://rutor.info"
}

var (
	hostsPos = 0
	mhost    sync.Mutex
)

type RutorParser struct {
	mu      sync.Mutex
	isParse bool
}

func NewRutor() *RutorParser { return new(RutorParser) }

func (self *RutorParser) Name() string { return "rutor" }

func (self *RutorParser) httpClient() *http.Client {
	return clientForRoute("parser_rutor")
}

type parseLink struct {
	Host     string
	Link     string
	Cat      string
	FullScan bool
	Cutoff   time.Time
}

func getHost() string {
	mhost.Lock()
	defer mhost.Unlock()
	host := loadRutorHost()
	if host == "" {
		return "http://rutor.info"
	}
	return host
}

func (self *RutorParser) Parse() {
	self.mu.Lock()
	if self.isParse {
		self.mu.Unlock()
		return
	}
	self.isParse = true
	defer func() { self.isParse = false }()
	self.mu.Unlock()

	fullScan, cutoff := scanCutoff("rutor")
	pages := self.readCategories()

	var taskers []*tasker.Tasker
	var processed atomic.Int64

	for cat, pgs := range pages {
		tsk := tasker.New(1, false)
		tsk.DisableLog()
		taskers = append(taskers, tsk)

		for i := 0; i < pgs; i++ {
			host := getHost()
			pl := parseLink{
				Host:     host,
				Link:     host + "/browse/" + strconv.Itoa(i) + "/" + cat + "/0/0",
				Cat:      cat,
				FullScan: fullScan,
				Cutoff:   cutoff,
			}

			tsk.Add(func(pl interface{}) bool {
				p := pl.(parseLink)
				if stopRequest.Load() {
					log.Printf("rutor/%s: stop requested, halting", p.Cat)
					return false
				}
				list := self.parsePage(p)
				if len(list) == 0 {
					return false
				}

				var toEnrich []enrichJob
				hitCutoff := false

				for _, d := range list {
					if !p.FullScan && !d.CreateDate.IsZero() && d.CreateDate.Before(p.Cutoff) {
						log.Printf("rutor/%s: reached cutoff at %s", p.Cat, d.CreateDate.Format("2006-01-02"))
						hitCutoff = true
						break
					}
					processed.Add(1)
					if d.Categories == models.CatTVShow {
						store.CacheTorrent(d.Hash, "", "rutor")
						continue
					}
					cached, cardID := store.TorrentStatus(d.Hash)
					if cached && cardID != "" {
						if d.VideoQuality > 0 {
							store.UpdateQuality(cardID, d.VideoQuality)
						}
						continue
					}
					toEnrich = append(toEnrich, enrichJob{d, isMovieCat(d.Categories)})
				}

				utils.PForLim(toEnrich, 20, func(_ int, job enrichJob) {
					releases.Enrich("rutor/"+p.Cat, job.isMovie, job.d)
				})

				return !hitCutoff
			}, pl)
		}
	}

	for _, t := range taskers {
		go t.Run()
	}
	for _, t := range taskers {
		t.Wait()
	}

	commitScan("rutor", &processed)
}

func (self *RutorParser) readCategories() map[string]int {
	// 1  - Зарубежные фильмы   | Фильмы
	// 5  - Наши фильмы         | Фильмы
	// 4  - Зарубежные сериалы  | Сериалы
	// 16 - Наши сериалы        | Сериалы
	// 12 - Науч-поп фильмы     | Dok
	// 6  - Телевизор           | ТВ Шоу
	// 7  - Мультипликация      | Мультфильмы
	// 10 - Аниме               | Аниме
	// 17 - Иностранные релизы  | UA озвучка
	// 13 - Спорт и Здоровье    | ТВ Шоу
	// 15 - Юмор                | ТВ Шоу
	log.Println("Read Rutor categories")

	categories := []string{"1", "5", "4", "16", "12", "6", "7", "10", "17", "13", "15"}
	pages := map[string]int{}
	var mm sync.Mutex
	attempts, baseWait, maxWait, ratio := retryOpts()

	utils.PFor(categories, func(_ int, cat string) {
		link := getHost() + "/browse/0/" + cat + "/0/0"
		bodyBytes, err := fetchBytesRetry(self.httpClient(), link, attempts, baseWait, maxWait, ratio)
		if err != nil {
			return
		}
		re := regexp.MustCompile(`<a href="/browse/([0-9]+)/[0-9]+/[0-9]+/[0-9]+"><b>[0-9]+&nbsp;-&nbsp;[0-9]+</b></a></p>`)
		matches := re.FindStringSubmatch(string(bodyBytes))
		if len(matches) > 1 {
			if pgs, err := strconv.Atoi(matches[1]); err == nil {
				mm.Lock()
				pages[cat] = pgs
				log.Println("Category readed", link, pgs)
				mm.Unlock()
			}
		}
	})

	return pages
}

func (self *RutorParser) parsePage(pl parseLink) []*models.TorrentDetails {
	attempts, baseWait, maxWait, ratio := retryOpts()
	bodyBytes, err := fetchBytesRetry(self.httpClient(), pl.Link, attempts, baseWait, maxWait, ratio)
	if err != nil {
		log.Println("Error get page:", err, pl.Link)
		return nil
	}
	body := string(bodyBytes)
	if !strings.Contains(body, "<title>rutor.info") {
		log.Println("Not rutor page:", pl.Link)
		return nil
	}
	log.Println("Readed:", pl.Link)
	doc, err := goquery.NewDocumentFromReader(bytes.NewBufferString(body))
	if err != nil {
		log.Println("Error parse page:", err, pl.Link)
		return nil
	}

	var list []*models.TorrentDetails
	doc.Find("div#index").Find("tr").Each(func(_ int, sel *goquery.Selection) {
		if sel.HasClass("backgr") {
			return
		}
		selTd := sel.Find("td")

		itm := new(models.TorrentDetails)
		itm.CreateDate = parseRuDate(node2Text(selTd.Get(0)))
		itm.Title = node2Text(selTd.Get(1))
		self.parseTitle(itm, pl.Cat)
		itm.Magnet = selTd.Get(1).FirstChild.NextSibling.Attr[0].Val
		hash := getHash(itm.Magnet)
		if hash == "" {
			return
		}
		itm.Hash = hash
		itm.Link = strings.TrimSpace(selTd.Get(1).LastChild.Attr[0].Val)
		switch len(selTd.Nodes) {
		case 4:
			itm.Size = node2Text(selTd.Get(2))
			prarr := strings.Split(node2Text(selTd.Get(3)), "  ")
			if len(prarr) > 1 {
				itm.Seed, _ = strconv.Atoi(prarr[0])
				itm.Peer, _ = strconv.Atoi(prarr[1])
			}
		case 5:
			itm.Size = node2Text(selTd.Get(3))
			prarr := strings.Split(node2Text(selTd.Get(4)), "  ")
			if len(prarr) > 1 {
				itm.Seed, _ = strconv.Atoi(prarr[0])
				itm.Peer, _ = strconv.Atoi(prarr[1])
			}
		}
		itm.Tracker = "rutor"
		list = append(list, itm)
	})
	return list
}

func (self *RutorParser) parseTitle(td *models.TorrentDetails, cat string) {
	ParseTorrentTitle(td, td.Title)
	hasBrackets := HasEpisodeBrackets(td.Title)
	switch {
	case cat == "1", cat == "5", cat == "17":
		td.Categories = models.CatMovie
	case cat == "4", cat == "16":
		td.Categories = models.CatSeries
	case cat == "12":
		if hasBrackets {
			td.Categories = models.CatDocSeries
		} else {
			td.Categories = models.CatDocMovie
		}
	case cat == "6", cat == "13", cat == "15":
		td.Categories = models.CatTVShow
	case cat == "7":
		if hasBrackets {
			td.Categories = models.CatCartoonSeries
		} else {
			td.Categories = models.CatCartoonMovie
		}
	case cat == "10":
		td.Categories = models.CatAnime
	}
}
