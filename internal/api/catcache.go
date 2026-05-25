package api

import (
	"bytes"
	"context"
	"log"
	"movies-api/db/store"
	"net/http"
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
)

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
