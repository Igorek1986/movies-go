package parser

import (
	"fmt"
	"log"
	"strconv"
	"sync"
	"sync/atomic"

	"movies-api/db/store"
	"movies-api/internal/cfbypass"
)

// One-off maintenance: torrents linked to a card can end up with created_at
// (the tracker's own post date) NULL — either legacy rows inserted before a
// tracker started persisting it, or a tracker that's simply had very few
// fresh TMDB matches to exercise the write path. Crawls every category's
// full history for all three trackers — ignoring the incremental cutoff
// entirely, since these old pages are exactly what's missing — and fills in
// created_at wherever it's still NULL.
//
// Never touches card_id or re-runs TMDB matching: zero risk to existing
// cards (including tracker-exclusive ones), unlike wiping and re-parsing
// would be. Runs its own page loops rather than reusing runPageLoop/
// parser.go's stopRequest — that flag is the shared "stop all parsers"
// switch, and this backfill has its own independent lifecycle (started/
// stopped separately, possibly while a normal parse cycle is also running).
//
// One button covers all three trackers — rutor is ~5x the volume of nnmclub
// and kinozal now goes through the (slower) Cloudflare bypass, so a full
// pass can take a long time; the three run concurrently so the small ones
// don't wait on rutor.

type backfillState struct {
	running atomic.Bool
	stop    atomic.Bool
	pages   atomic.Int64
	fixed   atomic.Int64
}

var backfillStates = map[string]*backfillState{
	"nnmclub": {},
	"kinozal": {},
	"rutor":   {},
}

// TrackerBackfillStatus is a snapshot of one tracker's backfill progress.
type TrackerBackfillStatus struct {
	Running bool  `json:"running"`
	Pages   int64 `json:"pages"`
	Fixed   int64 `json:"fixed"`
}

// DateBackfillStatus is a snapshot of the whole (all-trackers) backfill pass.
type DateBackfillStatus struct {
	Running  bool                             `json:"running"`
	Trackers map[string]TrackerBackfillStatus `json:"trackers"`
}

// GetDateBackfillStatus returns the current state of the date backfill,
// across all trackers.
func GetDateBackfillStatus() DateBackfillStatus {
	out := DateBackfillStatus{Trackers: map[string]TrackerBackfillStatus{}}
	for name, st := range backfillStates {
		running := st.running.Load()
		if running {
			out.Running = true
		}
		out.Trackers[name] = TrackerBackfillStatus{
			Running: running,
			Pages:   st.pages.Load(),
			Fixed:   st.fixed.Load(),
		}
	}
	return out
}

// StopDateBackfill requests all running backfill passes to stop after their
// current page.
func StopDateBackfill() {
	for _, st := range backfillStates {
		st.stop.Store(true)
	}
}

// RunDateBackfillAll starts a backfill pass for all three trackers
// concurrently. Safe to call while it's already running (each tracker's own
// CompareAndSwap guard just no-ops) and safe to re-run afterwards (idempotent
// — only ever fills a NULL).
func RunDateBackfillAll() {
	var wg sync.WaitGroup
	for _, fn := range []func(){backfillNNMClubDates, backfillKinozalDates, backfillRutorDates} {
		wg.Add(1)
		go func(f func()) {
			defer wg.Done()
			f()
		}(fn)
	}
	wg.Wait()
	log.Println("date-backfill: all trackers done")
}

func backfillNNMClubDates() {
	st := backfillStates["nnmclub"]
	if !st.running.CompareAndSwap(false, true) {
		log.Println("date-backfill/nnmclub: already running")
		return
	}
	defer st.running.Store(false)
	st.pages.Store(0)
	st.fixed.Store(0)
	st.stop.Store(false)

	n := &NNMClubParser{}
	fetch := clientFetch(n.httpClient())
	attempts, baseWait, maxWait, ratio := retryOpts()

	for catID := range nnmClubCats {
		label := "date-backfill/nnmclub/" + catID
		for page := 0; ; page++ {
			if st.stop.Load() {
				log.Printf("%s: stop requested, halting", label)
				return
			}
			url := fmt.Sprintf(getNNMClubHost()+"/forum/viewforum.php?f=%s&start=%d", catID, page*50)
			body, err := fetchBytesRetry(fetch, url, attempts, baseWait, maxWait, ratio)
			if err != nil {
				log.Printf("%s: get %s: %v", label, url, err)
				break
			}
			st.pages.Add(1)
			items := n.parseListing(decodeWin1251(body), catID)
			if len(items) == 0 {
				break
			}
			for _, item := range items {
				if item.date.IsZero() {
					continue
				}
				if store.BackfillTorrentCreatedAt("nnm_"+item.topicID, "nnmclub", item.date) {
					st.fixed.Add(1)
				}
			}
		}
	}
	log.Printf("date-backfill/nnmclub: done, %d pages, %d fixed", st.pages.Load(), st.fixed.Load())
}

func backfillKinozalDates() {
	st := backfillStates["kinozal"]
	if !st.running.CompareAndSwap(false, true) {
		log.Println("date-backfill/kinozal: already running")
		return
	}
	defer st.running.Store(false)
	st.pages.Store(0)
	st.fixed.Store(0)
	st.stop.Store(false)

	k := &KinozalParser{}
	fetch := clientFetch(k.httpClient())
	decode := decodeWin1251
	if cfbypass.Enabled() {
		fetch = cfbypass.Get
		decode = func(b []byte) string { return string(b) }
	}
	attempts, baseWait, maxWait, ratio := retryOpts()

	for catID := range kinozalCats {
		label := "date-backfill/kinozal/" + catID
		for page := 0; ; page++ {
			if st.stop.Load() {
				log.Printf("%s: stop requested, halting", label)
				return
			}
			url := fmt.Sprintf(getKinozalHost()+"/browse.php?c=%s&page=%d", catID, page)
			body, err := fetchBytesRetry(fetch, url, attempts, baseWait, maxWait, ratio)
			if err != nil {
				log.Printf("%s: get %s: %v", label, url, err)
				break
			}
			st.pages.Add(1)
			items := k.parseListing(decode(body), catID)
			if len(items) == 0 {
				break
			}
			for _, item := range items {
				if item.date.IsZero() {
					continue
				}
				if store.BackfillTorrentCreatedAt("kz_"+item.torrentID, "kinozal", item.date) {
					st.fixed.Add(1)
				}
			}
		}
	}
	log.Printf("date-backfill/kinozal: done, %d pages, %d fixed", st.pages.Load(), st.fixed.Load())
}

func backfillRutorDates() {
	st := backfillStates["rutor"]
	if !st.running.CompareAndSwap(false, true) {
		log.Println("date-backfill/rutor: already running")
		return
	}
	defer st.running.Store(false)
	st.pages.Store(0)
	st.fixed.Store(0)
	st.stop.Store(false)

	rp := &RutorParser{}
	pagesByCat := rp.readCategories()

	for cat, pgs := range pagesByCat {
		label := "date-backfill/rutor/" + cat
		for page := 0; page < pgs; page++ {
			if st.stop.Load() {
				log.Printf("%s: stop requested, halting", label)
				return
			}
			pl := parseLink{
				Host: getHost(),
				Link: getHost() + "/browse/" + strconv.Itoa(page) + "/" + cat + "/0/0",
				Cat:  cat,
			}
			list := rp.parsePage(pl)
			st.pages.Add(1)
			for _, d := range list {
				if d.CreateDate.IsZero() || d.Hash == "" {
					continue
				}
				if store.BackfillTorrentCreatedAt(d.Hash, "rutor", d.CreateDate) {
					st.fixed.Add(1)
				}
			}
		}
	}
	log.Printf("date-backfill/rutor: done, %d pages, %d fixed", st.pages.Load(), st.fixed.Load())
}
