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

	log.Println("catcache: invalidated")
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
// Source: SURS plugin without_keywords list.
var DefaultChildBlockedKeywords = []int{
	346488, 158718, 41278, 13141, 345822, 315535, 290667, 323477, 290609,
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
