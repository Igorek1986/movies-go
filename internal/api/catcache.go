package api

import (
	"bytes"
	"context"
	"log"
	"movies-api/db/store"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sync/singleflight"
)

// ─── Response cache ───────────────────────────────────────────────────────────

type cachedResp struct {
	contentType string
	body        []byte
}

var (
	catCacheMu sync.RWMutex
	catCache   = map[string]cachedResp{}
)

// InvalidateCategoryCache drops all cached category responses and the tracker setting.
// Called after each parser run so fresh data is served immediately.
func InvalidateCategoryCache() {
	catCacheMu.Lock()
	catCache = map[string]cachedResp{}
	catCacheMu.Unlock()

	trackerMu.Lock()
	trackerCached = ""
	trackerMu.Unlock()

	requirePosterMu.Lock()
	requirePosterCached = nil
	requirePosterMu.Unlock()

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

	watchedMu.Lock()
	watchedCache = map[string][]string{}
	watchedMu.Unlock()

	log.Println("catcache: invalidated")

	// Data changed — refresh per-category totals for random collections.
	go RecomputeCategoryCounts()
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
)

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
		watchedMu.Unlock()
		return computed, nil
	})
	return v.([]string)
}

// InvalidateWatched drops cached watched-sets for a profile after it saves a timecode,
// so the next category request reflects the new progress.
func InvalidateWatched(deviceID int64, profileID string) {
	prefix := strconv.FormatInt(deviceID, 10) + ":" + profileID + ":"
	watchedMu.Lock()
	for k := range watchedCache {
		if strings.HasPrefix(k, prefix) {
			delete(watchedCache, k)
		}
	}
	watchedMu.Unlock()
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

func getCached(key string) (cachedResp, bool) {
	catCacheMu.RLock()
	v, ok := catCache[key]
	catCacheMu.RUnlock()
	return v, ok
}

func setCached(key string, r cachedResp) {
	catCacheMu.Lock()
	catCache[key] = r
	catCacheMu.Unlock()
}

// withCategoryCache wraps a category handler with in-memory response caching.
// Cache key = full request URI (path + query string).
// On a cache miss the handler response is captured, cached, and forwarded normally.
func withCategoryCache(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.RequestURI()

		if entry, ok := getCached(key); ok {
			w.Header().Set("Content-Type", entry.contentType)
			w.WriteHeader(http.StatusOK)
			w.Write(entry.body) //nolint:errcheck
			return
		}

		cap := &responseCapture{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(cap, r)

		if cap.status == http.StatusOK && cap.buf.Len() > 0 {
			ct := cap.ResponseWriter.Header().Get("Content-Type")
			setCached(key, cachedResp{contentType: ct, body: cap.buf.Bytes()})
		}
	}
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
