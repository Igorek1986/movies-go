package api

import (
	"bytes"
	"container/list"
	"context"
	"log"
	"movies-api/db/postgres"
	"movies-api/db/store"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// ─── Response cache ───────────────────────────────────────────────────────────

type cachedResp struct {
	ContentType string
	Body        []byte
	Generation  int64
}

// maxCatCacheEntries caps catCache size. The key is the full request URI including
// the device token (see withCategoryCache), so this is effectively per-device, not
// per-category — measured ~43KB per (device, category page) entry under synthetic
// load, so a 10000-entry cap bounds worst case around ~400MB, well above any
// realistic working set for this project's scale.
const maxCatCacheEntries = 10000

var (
	catCacheMu    sync.RWMutex
	catCache      = map[string]cachedResp{}
	catCacheOrder = list.New()                  // front = most recently written; back = eviction candidate
	catCacheElems = map[string]*list.Element{}   // key -> its node in catCacheOrder
	catGeneration int64
	catRefreshSF  singleflight.Group // dedupes concurrent background refreshes of the same stale key
)

// InvalidateCategoryCache marks every cached category response stale and refreshes
// the tracker setting. Called after each parser run.
//
// Does NOT clear catCache: a request for a now-stale key still gets served
// immediately from the old value (withCategoryCache checks the generation and
// kicks off a background refresh) instead of blocking on a synchronous recompute —
// the "first request after a parser run pays full price" problem this used to cause.
// catCache no longer self-cleans via a periodic wipe, so setCached enforces
// maxCatCacheEntries itself (LRU by last-write, see there) instead.
func InvalidateCategoryCache() {
	catCacheMu.Lock()
	catGeneration++
	catCacheMu.Unlock()

	trackerMu.Lock()
	trackerCached = ""
	trackerMu.Unlock()

	requirePosterMu.Lock()
	requirePosterCached = nil
	requirePosterMu.Unlock()

	showNoTorrentMu.Lock()
	showNoTorrentCached = nil
	showNoTorrentMu.Unlock()

	childKeywordsMu.Lock()
	childKeywordsCached = nil
	childKeywordsLoaded = false
	childKeywordsMu.Unlock()

	childTextKwMu.Lock()
	childTextKwCached = nil
	childTextKwLoaded = false
	childTextKwMu.Unlock()

	childTextAgesMu.Lock()
	childTextAgesCached = nil
	childTextAgesLoaded = false
	childTextAgesMu.Unlock()

	popularMu.Lock()
	popularActors = popularPool{}
	popularActorsRu = popularPool{}
	popularDirectors = popularPool{}
	popularMu.Unlock()

	// watchedCache/unwatchedCache are NOT wiped here: a parser run never changes
	// episode air dates or completion status (that data comes from MyShows sync,
	// see InvalidateWatchedForCard) — only the catalog (new cards/torrents), which
	// is what catCache/catCount above cover.

	log.Println("catcache: invalidated")

	// Data changed — refresh per-category totals for random collections.
	go RecomputeCategoryCounts()

	// Eagerly recompute the admin-stats counts too, so the first /api/admin/stats
	// visit after a parser run doesn't pay the ~500ms recompute cost itself.
	go refreshStatsCounts(context.Background())
}

// ─── Watched-set cache (per device+profile) ──────────────────────────────────
//
// hide_watched excludes fully-watched cards. Computing that set scans timecodes +
// episodes (~hundreds of ms) and was previously run inside every category request —
// painful for the uncached genre_* collections. The set depends only on the profile's
// progress, so it is cached here and refreshed when the profile saves a timecode.

var (
	watchedMu    sync.RWMutex
	watchedCache = map[string][]string{}
	watchedSF    singleflight.Group // dedupe concurrent computations of the same key

	// watchedCardIndex maps card_id -> set of watchedCache keys whose cached set
	// contained that card at some point. Used by InvalidateWatchedForCard to drop
	// just the affected entries instead of wiping every profile. Entries are only
	// ever added, never pruned when a key's set changes — a stale association just
	// means an occasional harmless extra invalidation, not a correctness issue.
	watchedCardIndex = map[string]map[string]struct{}{}
)

func indexWatchedCard(cardID, key string) {
	set, ok := watchedCardIndex[cardID]
	if !ok {
		set = map[string]struct{}{}
		watchedCardIndex[cardID] = set
	}
	set[key] = struct{}{}
}

func watchedKey(deviceID int64, profileID string, percent int) string {
	return strconv.FormatInt(deviceID, 10) + ":" + profileID + ":" + strconv.Itoa(percent)
}

// cachedWatchedCardIDs returns the profile's fully-watched card_ids, computing and
// caching them on a miss.
func cachedWatchedCardIDs(deviceID int64, profileID string, percent int) []string {
	k := watchedKey(deviceID, profileID, percent)
	watchedMu.RLock()
	ids, ok := watchedCache[k]
	watchedMu.RUnlock()
	if ok {
		return ids
	}
	// Concurrent misses (e.g. the 16 genre lines on the home screen) share one query.
	v, _, _ := watchedSF.Do(k, func() (any, error) {
		watchedMu.RLock()
		cached, hit := watchedCache[k]
		watchedMu.RUnlock()
		if hit {
			return cached, nil
		}
		computed := store.WatchedCardIDs(deviceID, profileID, percent)
		watchedMu.Lock()
		watchedCache[k] = computed
		for _, id := range computed {
			indexWatchedCard(id, k)
		}
		watchedMu.Unlock()
		return computed, nil
	})
	return v.([]string)
}

// InvalidateWatched drops cached watched-sets for one profile after its progress changes,
// so the next category request reflects it.
func InvalidateWatched(deviceID int64, profileID string) {
	invalidateWatchedPrefix(strconv.FormatInt(deviceID, 10) + ":" + profileID + ":")
}

// InvalidateWatchedDevice drops cached watched-sets for all profiles of a device
// (used when a mutation clears/affects the whole device).
func InvalidateWatchedDevice(deviceID int64) {
	invalidateWatchedPrefix(strconv.FormatInt(deviceID, 10) + ":")
}

func invalidateWatchedPrefix(prefix string) {
	watchedMu.Lock()
	for k := range watchedCache {
		if strings.HasPrefix(k, prefix) {
			delete(watchedCache, k)
		}
	}
	watchedMu.Unlock()
}

// ─── Unwatched-shows cache (per device+profile) ───────────────────────────────
//
// Same shape as the watched-set cache above, but for the "Непросмотренные" list.
// Unlike genre_*/movies_new/etc. (cached by full URL, invalidated only per parser
// run), this is profile-progress-dependent and must go stale the moment a timecode
// changes — so it gets its own cache keyed by device+profile+percent and is dropped
// via the same OnWatchedChanged hook as watchedCache, not the generic category cache.

var (
	unwatchedMu    sync.RWMutex
	unwatchedCache = map[string][]store.UnwatchedTVShow{}
	unwatchedSF    singleflight.Group

	// unwatchedCardIndex mirrors watchedCardIndex above, for unwatchedCache entries.
	unwatchedCardIndex = map[string]map[string]struct{}{}
)

func indexUnwatchedCard(cardID, key string) {
	set, ok := unwatchedCardIndex[cardID]
	if !ok {
		set = map[string]struct{}{}
		unwatchedCardIndex[cardID] = set
	}
	set[key] = struct{}{}
}

func unwatchedKey(deviceID int64, profileID string, percent int, sortOrder string) string {
	return strconv.FormatInt(deviceID, 10) + ":" + profileID + ":" + strconv.Itoa(percent) + ":" + sortOrder
}

// cachedUnwatchedShows returns the profile's "Непросмотренные" shows, computing and
// caching them on a miss. sortOrder is part of the cache key — different sort orders
// are cached separately.
func cachedUnwatchedShows(deviceID int64, profileID string, percent int, sortOrder string) []store.UnwatchedTVShow {
	k := unwatchedKey(deviceID, profileID, percent, sortOrder)
	unwatchedMu.RLock()
	shows, ok := unwatchedCache[k]
	unwatchedMu.RUnlock()
	if ok {
		return shows
	}
	v, _, _ := unwatchedSF.Do(k, func() (any, error) {
		unwatchedMu.RLock()
		cached, hit := unwatchedCache[k]
		unwatchedMu.RUnlock()
		if hit {
			return cached, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		computed := store.UnwatchedTVShows(ctx, deviceID, profileID, percent, sortOrder)
		unwatchedMu.Lock()
		unwatchedCache[k] = computed
		for _, show := range computed {
			indexUnwatchedCard(show.CardID, k)
		}
		unwatchedMu.Unlock()
		return computed, nil
	})
	return v.([]store.UnwatchedTVShow)
}

// InvalidateUnwatched drops the cached "Непросмотренные" list for one profile after its
// progress changes.
func InvalidateUnwatched(deviceID int64, profileID string) {
	invalidateUnwatchedPrefix(strconv.FormatInt(deviceID, 10) + ":" + profileID + ":")
}

// InvalidateUnwatchedDevice drops cached "Непросмотренные" lists for all profiles of a
// device.
func InvalidateUnwatchedDevice(deviceID int64) {
	invalidateUnwatchedPrefix(strconv.FormatInt(deviceID, 10) + ":")
}

func invalidateUnwatchedPrefix(prefix string) {
	unwatchedMu.Lock()
	for k := range unwatchedCache {
		if strings.HasPrefix(k, prefix) {
			delete(unwatchedCache, k)
		}
	}
	unwatchedMu.Unlock()
}

// InvalidateWatchedForCard drops watchedCache/unwatchedCache entries for every
// profile whose cached set referenced cardID, via the reverse indexes built in
// cachedWatchedCardIDs/cachedUnwatchedShows. Wired to myshows.OnEpisodesUpdated —
// the show's aired-episode count just changed, so its watched/unwatched status may
// have too. Cheaper than a full wipe: only profiles that actually track this show
// pay for a recompute, and only on their next request.
//
// This only reaches cache entries that ALREADY reference cardID — it cannot help
// a profile whose cached "Непросмотренные" list doesn't include this card yet
// (nothing indexed it there to find). That's fine for most callers (a timecode
// write, MyShows sync, etc. all touch a card the profile is already presumed to
// be tracking) but wrong for the one case where a card is about to become newly
// relevant to profiles that don't have it cached at all yet — the aired-cutoff
// crossing (see StartUnwatchedCutoffInvalidation/replayMissedCutoffs), which is
// exactly when a show with zero unwatched episodes yesterday can suddenly qualify
// today. Those callers use InvalidateAllUnwatched (below) instead/in addition.
func InvalidateWatchedForCard(cardID string) {
	watchedMu.Lock()
	for k := range watchedCardIndex[cardID] {
		delete(watchedCache, k)
	}
	delete(watchedCardIndex, cardID)
	watchedMu.Unlock()

	unwatchedMu.Lock()
	for k := range unwatchedCardIndex[cardID] {
		delete(unwatchedCache, k)
	}
	delete(unwatchedCardIndex, cardID)
	unwatchedMu.Unlock()
}

// InvalidateAllUnwatched drops every cached "Непросмотренные" list, regardless of
// which cards they reference — the reverse-index-based InvalidateWatchedForCard
// can't be used for this (see its own comment): a show crossing the aired cutoff
// can make it newly eligible for profiles whose cached list was computed before
// that, and no cache entry currently references the card to find via the index.
// A full wipe is the simplest correct fix — this only runs once per aired-cutoff
// crossing (at most a handful of times a day, see StartUnwatchedCutoffInvalidation),
// not per-request, so the lost warmth (every profile recomputes its list once, on
// its next request) is a non-issue next to serving a permanently-wrong list.
func InvalidateAllUnwatched() {
	unwatchedMu.Lock()
	unwatchedCache = map[string][]store.UnwatchedTVShow{}
	unwatchedCardIndex = map[string]map[string]struct{}{}
	unwatchedMu.Unlock()
}

// ─── Continues cache (per device+profile) ─────────────────────────────────────
//
// Same shape and reasoning as the unwatched-shows cache above: "Продолжить
// просмотр" depends on per-user timecodes/subjective_statuses, not the catalog, so
// it must not sit behind the generic full-URL category cache (that one only goes
// stale on parser runs — a card just marked "Брошено" would keep showing here for
// hours). Only the timecode/status scan (store.ContinuesAggregate) is cached;
// pagination and the media_cards fetch (store.GetContinues) stay uncached, same
// split as cachedUnwatchedShows/ListCategory.

var (
	continuesMu    sync.RWMutex
	continuesCache = map[string][]store.ContinuesAgg{}
	continuesSF    singleflight.Group
)

func continuesKey(deviceID int64, profileID, mediaFilter string, minPct int) string {
	return strconv.FormatInt(deviceID, 10) + ":" + profileID + ":" + mediaFilter + ":" + strconv.Itoa(minPct)
}

// cachedContinuesAgg returns the profile's in-progress card aggregate, computing
// and caching it on a miss.
func cachedContinuesAgg(ctx context.Context, deviceID int64, profileID, mediaFilter string, minPct int) []store.ContinuesAgg {
	k := continuesKey(deviceID, profileID, mediaFilter, minPct)
	continuesMu.RLock()
	agg, ok := continuesCache[k]
	continuesMu.RUnlock()
	if ok {
		return agg
	}
	v, _, _ := continuesSF.Do(k, func() (any, error) {
		continuesMu.RLock()
		cached, hit := continuesCache[k]
		continuesMu.RUnlock()
		if hit {
			return cached, nil
		}
		computed := store.ContinuesAggregate(ctx, deviceID, profileID, mediaFilter, minPct)
		continuesMu.Lock()
		continuesCache[k] = computed
		continuesMu.Unlock()
		return computed, nil
	})
	return v.([]store.ContinuesAgg)
}

// InvalidateContinues drops the cached "Продолжить просмотр" aggregate for one
// profile after its progress or status changes.
func InvalidateContinues(deviceID int64, profileID string) {
	invalidateContinuesPrefix(strconv.FormatInt(deviceID, 10) + ":" + profileID + ":")
}

// InvalidateContinuesDevice drops cached "Продолжить просмотр" aggregates for all
// profiles of a device.
func InvalidateContinuesDevice(deviceID int64) {
	invalidateContinuesPrefix(strconv.FormatInt(deviceID, 10) + ":")
}

func invalidateContinuesPrefix(prefix string) {
	continuesMu.Lock()
	for k := range continuesCache {
		if strings.HasPrefix(k, prefix) {
			delete(continuesCache, k)
		}
	}
	continuesMu.Unlock()
}

// ─── Per-category totals (random collections) ────────────────────────────────
//
// genre_* / genre_random are served via an indexed rand_key seek (no COUNT in the
// request path). Their total card count changes only when the catalog changes, so it
// is cached here and refreshed once per parser run via RecomputeCategoryCounts.

var (
	catCountMu sync.RWMutex
	catCount   = map[string]int{}
)

// cachedCategoryCount returns the cached total for a random category, computing the
// base count on a cold miss.
func cachedCategoryCount(category string) int {
	catCountMu.RLock()
	v, ok := catCount[category]
	catCountMu.RUnlock()
	if ok {
		return v
	}
	return recomputeCategoryCount(category)
}

func recomputeCategoryCount(category string) int {
	preset, ok := categoryRoutes[category]
	if !ok {
		return 0
	}
	f := preset
	applyCatalogTrackers(&f)
	c := store.CountCategory(f)
	catCountMu.Lock()
	catCount[category] = c
	catCountMu.Unlock()
	return c
}

// RecomputeCategoryCounts refreshes cached totals for all random (genre_*) categories.
// Called after each parser run and once at startup.
func RecomputeCategoryCounts() {
	for cat := range categoryRoutes {
		if strings.HasPrefix(cat, "genre_") {
			recomputeCategoryCount(cat)
		}
	}
}

// getCached returns the cached entry (if any) and whether it's stale — computed
// in a generation older than the current one, i.e. a parser run happened since it
// was cached.
func getCached(key string) (entry cachedResp, ok bool, stale bool) {
	catCacheMu.RLock()
	entry, ok = catCache[key]
	gen := catGeneration
	catCacheMu.RUnlock()
	if !ok {
		return entry, false, false
	}
	return entry, true, entry.Generation != gen
}

// setCached stores an entry and touches its LRU position (moved to front on
// every write — including background refreshes of a stale entry, so anything
// still being requested stays warm). Evicts the least-recently-written entry
// once catCache exceeds maxCatCacheEntries. Recency is only tracked on writes,
// not on plain reads (getCached), to keep the hot read path lock-free of list
// bookkeeping — an approximation of true LRU, not exact, but cheap.
func setCached(key string, r cachedResp) {
	catCacheMu.Lock()
	r.Generation = catGeneration
	catCache[key] = r
	if elem, ok := catCacheElems[key]; ok {
		catCacheOrder.MoveToFront(elem)
	} else {
		catCacheElems[key] = catCacheOrder.PushFront(key)
	}
	for catCacheOrder.Len() > maxCatCacheEntries {
		oldest := catCacheOrder.Back()
		if oldest == nil {
			break
		}
		oldKey := oldest.Value.(string)
		catCacheOrder.Remove(oldest)
		delete(catCacheElems, oldKey)
		delete(catCache, oldKey)
	}
	catCacheMu.Unlock()
}

// withCategoryCache wraps a category handler with in-memory response caching.
// Cache key = full request URI (path + query string).
// On a cache miss the handler response is captured, cached, and forwarded normally.
// On a stale hit (see InvalidateCategoryCache), the old value is still served
// immediately and a background refresh recomputes it via refreshCatCache.
func withCategoryCache(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.RequestURI()

		if entry, ok, stale := getCached(key); ok {
			w.Header().Set("Content-Type", entry.ContentType)
			w.WriteHeader(http.StatusOK)
			w.Write(entry.Body) //nolint:errcheck
			if stale {
				go refreshCatCache(key, h, r.Clone(context.Background()))
			}
			return
		}

		cap := &responseCapture{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(cap, r)

		if cap.status == http.StatusOK && cap.buf.Len() > 0 {
			ct := cap.ResponseWriter.Header().Get("Content-Type")
			setCached(key, cachedResp{ContentType: ct, Body: cap.buf.Bytes()})
		}
	}
}

// refreshCatCache recomputes a stale category response in the background and
// swaps it into catCache, deduping concurrent refreshes of the same key (several
// requests can all observe the same stale entry before the first refresh lands).
// Runs against a detached context — the request that triggered it may finish (and
// its context get canceled) well before this completes.
func refreshCatCache(key string, h http.HandlerFunc, req *http.Request) {
	_, _, _ = catRefreshSF.Do(key, func() (any, error) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK && rec.Body.Len() > 0 {
			ct := rec.Header().Get("Content-Type")
			setCached(key, cachedResp{ContentType: ct, Body: rec.Body.Bytes()})
		}
		return nil, nil
	})
}

// responseCapture records what the handler writes while forwarding to w normally.
type responseCapture struct {
	http.ResponseWriter
	buf    bytes.Buffer
	status int
}

func (c *responseCapture) WriteHeader(s int) {
	c.status = s
	c.ResponseWriter.WriteHeader(s)
}

func (c *responseCapture) Write(b []byte) (int, error) {
	c.buf.Write(b)
	return c.ResponseWriter.Write(b)
}

// ─── Cached catalog_trackers setting ─────────────────────────────────────────

var (
	trackerMu     sync.RWMutex
	trackerCached string

	requirePosterMu     sync.RWMutex
	requirePosterCached *bool

	showNoTorrentMu     sync.RWMutex
	showNoTorrentCached *bool

	childKeywordsMu     sync.RWMutex
	childKeywordsCached []int
	childKeywordsLoaded bool

	childTextKwMu     sync.RWMutex
	childTextKwCached []string
	childTextKwLoaded bool

	childTextAgesMu     sync.RWMutex
	childTextAgesCached []int
	childTextAgesLoaded bool
)

// cachedRequirePoster returns true if cards without a poster should be excluded.
// Default: true. Reset by InvalidateCategoryCache.
func cachedRequirePoster() bool {
	requirePosterMu.RLock()
	v := requirePosterCached
	requirePosterMu.RUnlock()
	if v != nil {
		return *v
	}

	requirePosterMu.Lock()
	defer requirePosterMu.Unlock()
	if requirePosterCached != nil {
		return *requirePosterCached
	}
	val := true // default: enabled
	if s, ok := store.GetSetting(context.Background(), "catalog_require_poster"); ok {
		val = s != "0"
	}
	requirePosterCached = &val
	return val
}

// cachedShowNoTorrent returns true if cards without any torrent (added manually
// from TMDB search, or auto-created from a timecode on unparsed content — see
// handleWebAddFromTMDB / refreshCardFromTMDB) should be shown in the general
// catalog/genre listings alongside regular cards. Default: true. "Моё"/История
// show such cards regardless of this setting — it only gates ListCategory's
// generic browse paths. Reset by InvalidateCategoryCache.
func cachedShowNoTorrent() bool {
	showNoTorrentMu.RLock()
	v := showNoTorrentCached
	showNoTorrentMu.RUnlock()
	if v != nil {
		return *v
	}

	showNoTorrentMu.Lock()
	defer showNoTorrentMu.Unlock()
	if showNoTorrentCached != nil {
		return *showNoTorrentCached
	}
	val := true // default: enabled
	if s, ok := store.GetSetting(context.Background(), "catalog_show_no_torrent"); ok {
		val = s != "0"
	}
	showNoTorrentCached = &val
	return val
}

// cachedTrackers returns the catalog_trackers setting, reading from DB only once
// per parser cycle. Reset by InvalidateCategoryCache.
func cachedTrackers() string {
	trackerMu.RLock()
	v := trackerCached
	trackerMu.RUnlock()
	if v != "" {
		return v
	}

	trackerMu.Lock()
	defer trackerMu.Unlock()
	if trackerCached != "" {
		return trackerCached
	}
	if s, ok := store.GetSetting(context.Background(), "catalog_trackers"); ok && s != "" {
		trackerCached = s
	} else {
		trackerCached = "rutor"
	}
	return trackerCached
}

// DefaultChildBlockedKeywords are TMDB keyword IDs blocked for child profiles by default.
var DefaultChildBlockedKeywords = []int{
	281741, // nudity
	354470, // sex scene
	329280, // sexual content
	570,    // rape
	312898, // violence
	10292,  // gore
	13006,  // torture
	11494,  // drug use
	158718, // lgbt
}

// cachedChildKeywords returns the list of TMDB keyword IDs to block for child profiles.
// Loaded once from app_settings, reset by InvalidateCategoryCache.
func cachedChildKeywords() []int {
	childKeywordsMu.RLock()
	if childKeywordsLoaded {
		v := childKeywordsCached
		childKeywordsMu.RUnlock()
		return v
	}
	childKeywordsMu.RUnlock()

	childKeywordsMu.Lock()
	defer childKeywordsMu.Unlock()
	if childKeywordsLoaded {
		return childKeywordsCached
	}
	childKeywordsLoaded = true

	val, ok := store.GetSetting(context.Background(), "child_blocked_keywords")
	if !ok || strings.TrimSpace(val) == "" {
		childKeywordsCached = DefaultChildBlockedKeywords
		return childKeywordsCached
	}
	var ids []int
	for _, s := range strings.Split(val, "\n") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if id, err := strconv.Atoi(s); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	childKeywordsCached = ids
	return childKeywordsCached
}

// cachedChildTextKeywords returns text words to block in title/overview for child profiles.
func cachedChildTextKeywords() []string {
	childTextKwMu.RLock()
	if childTextKwLoaded {
		v := childTextKwCached
		childTextKwMu.RUnlock()
		return v
	}
	childTextKwMu.RUnlock()

	childTextKwMu.Lock()
	defer childTextKwMu.Unlock()
	if childTextKwLoaded {
		return childTextKwCached
	}
	childTextKwLoaded = true
	val, _ := store.GetSetting(context.Background(), "child_text_keywords")
	var words []string
	for _, s := range strings.Split(val, "\n") {
		s = strings.TrimSpace(s)
		if s != "" {
			words = append(words, s)
		}
	}
	childTextKwCached = words
	return childTextKwCached
}

// cachedChildTextAges returns ChildAge levels for which text keyword filtering is active.
// Default: [0] (ages 0-5 only).
func cachedChildTextAges() []int {
	childTextAgesMu.RLock()
	if childTextAgesLoaded {
		v := childTextAgesCached
		childTextAgesMu.RUnlock()
		return v
	}
	childTextAgesMu.RUnlock()

	childTextAgesMu.Lock()
	defer childTextAgesMu.Unlock()
	if childTextAgesLoaded {
		return childTextAgesCached
	}
	childTextAgesLoaded = true
	val, ok := store.GetSetting(context.Background(), "child_text_keyword_ages")
	if !ok || strings.TrimSpace(val) == "" {
		childTextAgesCached = []int{0}
		return childTextAgesCached
	}
	var ages []int
	for _, s := range strings.Split(val, ",") {
		s = strings.TrimSpace(s)
		if age, err := strconv.Atoi(s); err == nil {
			ages = append(ages, age)
		}
	}
	childTextAgesCached = ages
	return childTextAgesCached
}

// ─── Popular actors/directors pool cache ──────────────────────────────────────
//
// handleAPICategories (GET /api/categories) fetches a wide candidate pool via
// store.GetPopularActors/GetPopularDirectors and picks a random subset per
// request for menu variety (store.PickRandomActors) — but the pool itself is a
// full aggregation over media_card_cast/media_card_crew (measured ~300ms+ each,
// called up to 3x per request: actors, actors_ru, directors — GROUP BY + disk
// sort over ~1M rows) that only changes when the catalog does. Caching just the
// pool (not the picked subset) keeps the per-request randomness while cutting
// the DB cost. Reset by InvalidateCategoryCache.

type popularPool struct {
	limit int
	pool  []store.PopularActor
}

var (
	popularMu        sync.RWMutex
	popularActors    popularPool
	popularActorsRu  popularPool
	popularDirectors popularPool
)

func cachedPopularActors(ctx context.Context, limit int, ruOnly bool) []store.PopularActor {
	slot := &popularActors
	if ruOnly {
		slot = &popularActorsRu
	}
	popularMu.RLock()
	if slot.pool != nil && slot.limit == limit {
		p := slot.pool
		popularMu.RUnlock()
		return p
	}
	popularMu.RUnlock()

	pool := store.GetPopularActors(ctx, limit, ruOnly)
	popularMu.Lock()
	*slot = popularPool{limit: limit, pool: pool}
	popularMu.Unlock()
	return pool
}

func cachedPopularDirectors(ctx context.Context, limit int) []store.PopularActor {
	popularMu.RLock()
	if popularDirectors.pool != nil && popularDirectors.limit == limit {
		p := popularDirectors.pool
		popularMu.RUnlock()
		return p
	}
	popularMu.RUnlock()

	pool := store.GetPopularDirectors(ctx, limit)
	popularMu.Lock()
	popularDirectors = popularPool{limit: limit, pool: pool}
	popularMu.Unlock()
	return pool
}

// ─── Admin-stats counts cache ────────────────────────────────────────────────
//
// actor_count/director_count (/api/admin/stats) are each a COUNT(DISTINCT ...)
// over ~1M rows (media_card_cast/media_card_crew) — ~250-300ms apiece even with
// an index on person_id, since a distinct count still has to walk every row.
// popular_source_count is a live HTTP round-trip to the external source. None
// of the three needs to be request-fresh: they only change when the catalog
// changes (parser run / backfill) or the source is reconfigured. Cached and
// warmed eagerly in the background right after InvalidateCategoryCache runs
// (see refreshStatsCounts call there), so the admin never pays the recompute
// cost on the next visit — unlike the lazy popularPool cache above, which
// still makes the first post-parse request pay full price.

type statsCounts struct {
	actors        int
	directors     int
	popularSource int // -1 = unknown/unreachable
	loaded        bool
}

var (
	statsCountsMu    sync.RWMutex
	statsCountsCache statsCounts
)

func cachedStatsCounts(ctx context.Context) (actors, directors, popularSource int) {
	statsCountsMu.RLock()
	if statsCountsCache.loaded {
		c := statsCountsCache
		statsCountsMu.RUnlock()
		return c.actors, c.directors, c.popularSource
	}
	statsCountsMu.RUnlock()

	refreshStatsCounts(ctx)

	statsCountsMu.RLock()
	defer statsCountsMu.RUnlock()
	return statsCountsCache.actors, statsCountsCache.directors, statsCountsCache.popularSource
}

func refreshStatsCounts(ctx context.Context) {
	var actors, directors int
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(DISTINCT person_id) FROM media_card_cast`).Scan(&actors)                                     //nolint:errcheck
	postgres.Pool.QueryRow(ctx, `SELECT COUNT(DISTINCT person_id) FROM media_card_crew WHERE job='Director'`).Scan(&directors) //nolint:errcheck

	popularSource := -1
	if url := getPopularSourceURL(ctx); url != "" {
		sctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		if resp, err := fetchPopularSource(sctx, 1, 1, ""); err == nil {
			popularSource = resp.TotalResults
		}
		cancel()
	}

	statsCountsMu.Lock()
	statsCountsCache = statsCounts{actors: actors, directors: directors, popularSource: popularSource, loaded: true}
	statsCountsMu.Unlock()
}
