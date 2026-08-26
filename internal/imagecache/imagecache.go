// Package imagecache is the disk cache for TMDB poster/backdrop images served
// through /imgproxy (see internal/api/imgproxy.go). On by default
// (images_cache_enabled=1). When active, an image is fetched from TMDB once
// and served from local disk on every subsequent request, instead of
// re-proxying through the (DPI-bypass) proxy client every time.
//
// Keyed by the TMDB path itself (e.g. t/p/w342/abc.jpg) — TMDB assigns a new
// path when an image is genuinely replaced, so entries never need explicit
// invalidation, only size-based eviction (see StartEvictionLoop).
package imagecache

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime"
	"movies-api/db/store"
	"movies-api/internal/proxy"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// var, not const, so tests can point it at a temp dir.
var cacheDir = "cache/images"

var fetchSF singleflight.Group

// Running totals, kept in memory so /api/admin/stats can report cache size
// without scanning the directory on every request. Seeded from disk once at
// startup (LoadStats) and updated incrementally on write/evict.
var (
	cacheBytes atomic.Int64
	cacheFiles atomic.Int64
)

// lastServed tracks the most recent cache-hit time per file path, in memory
// only — deliberately not written back to the file itself (that would bump
// its mtime, which doubles as the HTTP Last-Modified header and would break
// If-Modified-Since/304 for clients that already have it cached). Eviction
// uses this to approximate true LRU (survives while popular) instead of
// FIFO-by-write-time (a hot file evicted just because it's old). Reset on
// restart — degrades gracefully back to write-time ordering until re-warmed.
var (
	lastServedMu sync.Mutex
	lastServed   = map[string]time.Time{}
)

// recencyFor returns the eviction-ordering timestamp for a cache file: the
// later of its on-disk mtime and its last recorded cache-hit time (if any).
func recencyFor(fp string, modTime time.Time) time.Time {
	lastServedMu.Lock()
	defer lastServedMu.Unlock()
	if la, ok := lastServed[fp]; ok && la.After(modTime) {
		return la
	}
	return modTime
}

// Stats returns the current cache size for display in admin stats.
func Stats() (sizeBytes int64, files int64) {
	return cacheBytes.Load(), cacheFiles.Load()
}

// LoadStats scans the cache directory once at startup to seed the in-memory
// totals (cache may already have content from before a restart).
func LoadStats() {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}
	var total, count int64
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		if info, err := e.Info(); err == nil {
			total += info.Size()
			count++
		}
	}
	cacheBytes.Store(total)
	cacheFiles.Store(count)
}

func path(tmdbPath string) string {
	// TMDB paths look like "t/p/w342/abc.jpg" — flatten to a single safe
	// filename rather than mirroring the directory structure.
	safe := strings.ReplaceAll(tmdbPath, "/", "_")
	return filepath.Join(cacheDir, safe)
}

// Exists reports whether tmdbPath is already on disk.
func Exists(tmdbPath string) bool {
	_, err := os.Stat(path(tmdbPath))
	return err == nil
}

// Enabled reports whether the disk cache is turned on (images_cache_enabled).
func Enabled(ctx context.Context) bool {
	return store.GetSettingInt(ctx, "images_cache_enabled") == 1
}

// ServeFromCache serves the image straight from disk if present.
func ServeFromCache(w http.ResponseWriter, r *http.Request, tmdbPath string) bool {
	fp := path(tmdbPath)
	f, err := os.Open(fp)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false
	}
	ct := mime.TypeByExtension(filepath.Ext(fp))
	if ct == "" {
		ct = "image/jpeg"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=604800")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeContent(w, r, fp, info.ModTime(), f)

	lastServedMu.Lock()
	lastServed[fp] = time.Now()
	lastServedMu.Unlock()

	return true
}

type fetchResult struct {
	body        []byte
	contentType string
}

// FetchAndCache fetches an image from TMDB and writes it to disk, deduping
// concurrent requests for the same path via singleflight (buffers in memory —
// posters/backdrops are small, ~50-90KB, so this is cheap and lets every
// waiting caller get its own copy of the bytes to write out).
func FetchAndCache(client *http.Client, tmdbPath string) ([]byte, string, error) {
	v, err, _ := fetchSF.Do(tmdbPath, func() (any, error) {
		req, err := http.NewRequest(http.MethodGet, "https://image.tmdb.org/"+tmdbPath, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20)) // 20MB safety cap
		if err != nil {
			return nil, err
		}
		ct := resp.Header.Get("Content-Type")
		if ct == "" {
			ct = "image/jpeg"
		}
		writeFile(tmdbPath, body)
		return fetchResult{body: body, contentType: ct}, nil
	})
	if err != nil {
		return nil, "", err
	}
	res := v.(fetchResult)
	return res.body, res.contentType, nil
}

func writeFile(tmdbPath string, body []byte) {
	fp := path(tmdbPath)
	tmp := fp + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		log.Printf("imagecache: write %s: %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, fp); err != nil {
		log.Printf("imagecache: rename %s -> %s: %v", tmp, fp, err)
		os.Remove(tmp) //nolint:errcheck
		return
	}
	cacheBytes.Add(int64(len(body)))
	cacheFiles.Add(1)
}

// tmdbPathFromURL extracts the "t/p/SIZE/xxx.jpg" tail from a full TMDB image
// URL (as stored in media_cards.poster_path/backdrop_path). Returns "" if url
// doesn't look like a TMDB image URL.
func tmdbPathFromURL(url string) string {
	idx := strings.Index(url, "/t/p/")
	if idx == -1 {
		return ""
	}
	return strings.TrimPrefix(url[idx:], "/")
}

// WarmCard prefetches the poster and backdrop for a newly upserted card into
// the disk cache, if caching is enabled, so the images are already warm the
// first time a client (web or Lampa) requests them. Fire-and-forget — runs in
// background goroutines and never blocks the caller (parser/enrich pipeline).
func WarmCard(posterURL, backdropURL string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if !Enabled(ctx) {
		cancel()
		return
	}
	cancel()

	for _, url := range []string{posterURL, backdropURL} {
		tmdbPath := tmdbPathFromURL(url)
		if tmdbPath == "" || Exists(tmdbPath) {
			continue
		}
		go func(tmdbPath string) {
			client := proxy.Default.ClientFor(context.Background(), proxy.RouteImages)
			if _, _, err := FetchAndCache(client, tmdbPath); err != nil {
				log.Printf("imagecache: warm %s: %v", tmdbPath, err)
			}
		}(tmdbPath)
	}

	if backdropURL != "" && store.GetSettingInt(context.Background(), "images_cache_warm_original") == 1 {
		warmAtSizes(backdropURL, []string{"original"})
	}
}

// basenameOfURL returns the last path segment of a URL (the TMDB image
// filename, stripped of domain/size prefix).
func basenameOfURL(u string) string {
	idx := strings.LastIndex(u, "/")
	if idx == -1 {
		return u
	}
	return u[idx+1:]
}

// warmAtSizes warms url (a full poster_path/backdrop_path URL) at each given
// TMDB size, skipping sizes already on disk.
func warmAtSizes(url string, sizes []string) {
	filename := basenameOfURL(url)
	if filename == "" {
		return
	}
	for _, size := range sizes {
		tmdbPath := "t/p/" + size + "/" + filename
		if Exists(tmdbPath) {
			continue
		}
		go func(p string) {
			client := proxy.Default.ClientFor(context.Background(), proxy.RouteImages)
			if _, _, err := FetchAndCache(client, p); err != nil {
				log.Printf("imagecache: warm sibling %s: %v", p, err)
			}
		}(tmdbPath)
	}
}

// WarmSibling looks up which card owns the TMDB image just requested through
// /imgproxy (identified by tmdbPath, e.g. "t/p/w500/abc.jpg") and, if that
// image was the poster, warms the backdrop (or vice versa) — so a client that
// only asked for one of the pair already finds the other one cached when it
// needs it (e.g. opening the full card right after browsing its poster).
// Only worth calling on a cache miss — a hit means the sibling was very
// likely already warmed alongside it the first time.
func WarmSibling(tmdbPath string) {
	filename := basenameOfURL(tmdbPath)
	if filename == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	posterURL, backdropURL, ok := store.FindCardImagesByFilename(ctx, filename)
	cancel()
	if !ok {
		return
	}

	if basenameOfURL(posterURL) == filename && backdropURL != "" {
		sizes := []string{"w780", "w1280"}
		if store.GetSettingInt(context.Background(), "images_cache_warm_original") == 1 {
			sizes = append(sizes, "original")
		}
		warmAtSizes(backdropURL, sizes)
	} else if basenameOfURL(backdropURL) == filename && posterURL != "" {
		warmAtSizes(posterURL, []string{"w500"})
	}
}

// staleTmpAge is how long a .tmp is given to turn into a real file (write +
// rename normally takes milliseconds) before sweepStaleTmp treats it as
// litter from a failed write (e.g. disk full) and removes it. Swept
// unconditionally on every tick, independent of evictIfOverLimit — a write
// failure never bumps cacheBytes, so a size-gated sweep would never run
// during the exact situation (disk full) that leaves .tmp litter behind.
const staleTmpAge = time.Hour

func sweepStaleTmp() {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-staleTmpAge)
	removed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if os.Remove(filepath.Join(cacheDir, e.Name())) == nil {
			removed++
		}
	}
	if removed > 0 {
		log.Printf("imagecache: swept %d stale .tmp file(s)", removed)
	}
}

// StartEvictionLoop periodically trims the cache down to images_cache_limit_mb,
// oldest-written file first, whenever it's enabled and over the limit, and
// sweeps any stray .tmp files left behind by failed writes. Runs once
// immediately, then on the given interval.
func StartEvictionLoop(ctx context.Context, interval time.Duration) {
	evictIfOverLimit(ctx)
	sweepStaleTmp()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				evictIfOverLimit(ctx)
				sweepStaleTmp()
			}
		}
	}()
}

type cacheEntry struct {
	path    string
	size    int64
	recency time.Time // max(write time, last cache-hit time) — see lastServed
}

func evictIfOverLimit(ctx context.Context) {
	limitMB := store.GetSettingInt(ctx, "images_cache_limit_mb")
	if limitMB <= 0 {
		return
	}
	limitBytes := int64(limitMB) * 1024 * 1024
	if cacheBytes.Load() <= limitBytes {
		return
	}
	// Trim down to 90% of the limit, not exactly to it — otherwise a handful
	// of writes right after this pass would push it back over and trigger
	// eviction again on the very next tick.
	targetBytes := limitBytes * 9 / 10

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}
	var files []cacheEntry
	var total int64
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fp := filepath.Join(cacheDir, e.Name())
		files = append(files, cacheEntry{
			path:    fp,
			size:    info.Size(),
			recency: recencyFor(fp, info.ModTime()),
		})
		total += info.Size()
	}
	if total <= targetBytes {
		cacheBytes.Store(total)
		cacheFiles.Store(int64(len(files)))
		return
	}

	sort.Slice(files, func(i, j int) bool { return files[i].recency.Before(files[j].recency) })
	removed := 0
	for _, f := range files {
		if total <= targetBytes {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
			removed++
			lastServedMu.Lock()
			delete(lastServed, f.path)
			lastServedMu.Unlock()
		}
	}
	cacheBytes.Store(total)
	cacheFiles.Add(int64(-removed))
	log.Printf("imagecache: evicted %d files, now %.1f MB (limit %d MB)", removed, float64(total)/1024/1024, limitMB)
}
