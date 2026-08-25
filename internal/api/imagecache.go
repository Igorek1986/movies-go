package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime"
	"movies-api/db/store"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// ─── Disk cache for /imgproxy ──────────────────────────────────────────────
//
// On by default (images_cache_enabled=1). Enabling it also makes
// handleAppConfig route the web UI's images through /imgproxy in the first
// place (see auth.go) — caching only matters once images actually pass
// through our server, so the setting drives its own routing rather than
// depending on a separate toggle. When active, a poster/backdrop is fetched
// from TMDB once and served from local disk on every subsequent request,
// instead of re-proxying through the (DPI-bypass) proxy client every time.
// Keyed by the TMDB path itself (e.g. t/p/w342/abc.jpg) — TMDB assigns a new
// path when an image is genuinely replaced, so entries never need explicit
// invalidation, only size-based eviction (see StartImageCacheEvictionLoop).

const imageCacheDir = "cache/images"

var imageFetchSF singleflight.Group

// Running totals, kept in memory so /api/admin/stats can report cache size
// without scanning the directory on every request. Seeded from disk once at
// startup (LoadImageCacheStats) and updated incrementally on write/evict.
var (
	imageCacheBytes atomic.Int64
	imageCacheFiles atomic.Int64
)

// ImageCacheStats returns the current cache size for display in admin stats.
func ImageCacheStats() (sizeBytes int64, files int64) {
	return imageCacheBytes.Load(), imageCacheFiles.Load()
}

// LoadImageCacheStats scans the cache directory once at startup to seed the
// in-memory totals (cache may already have content from before a restart).
func LoadImageCacheStats() {
	entries, err := os.ReadDir(imageCacheDir)
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
	imageCacheBytes.Store(total)
	imageCacheFiles.Store(count)
}

func imageCachePath(tmdbPath string) string {
	// TMDB paths look like "t/p/w342/abc.jpg" — flatten to a single safe
	// filename rather than mirroring the directory structure.
	safe := strings.ReplaceAll(tmdbPath, "/", "_")
	return filepath.Join(imageCacheDir, safe)
}

// serveFromImageCache serves the image straight from disk if present.
func serveFromImageCache(w http.ResponseWriter, r *http.Request, tmdbPath string) bool {
	fp := imageCachePath(tmdbPath)
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
	return true
}

type imageFetchResult struct {
	body        []byte
	contentType string
}

// fetchAndCacheImage fetches an image from TMDB and writes it to disk,
// deduping concurrent requests for the same path via singleflight (buffers
// in memory — posters/backdrops are small, ~50-90KB, so this is cheap and
// lets every waiting caller get its own copy of the bytes to write out).
func fetchAndCacheImage(client *http.Client, tmdbPath string) ([]byte, string, error) {
	v, err, _ := imageFetchSF.Do(tmdbPath, func() (any, error) {
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
		writeImageCacheFile(tmdbPath, body)
		return imageFetchResult{body: body, contentType: ct}, nil
	})
	if err != nil {
		return nil, "", err
	}
	res := v.(imageFetchResult)
	return res.body, res.contentType, nil
}

func writeImageCacheFile(tmdbPath string, body []byte) {
	fp := imageCachePath(tmdbPath)
	tmp := fp + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, fp); err != nil {
		os.Remove(tmp) //nolint:errcheck
		return
	}
	imageCacheBytes.Add(int64(len(body)))
	imageCacheFiles.Add(1)
}

// StartImageCacheEvictionLoop periodically trims the cache down to
// images_cache_limit_mb, oldest-written file first, whenever it's enabled
// and over the limit. Runs once immediately, then on the given interval.
func StartImageCacheEvictionLoop(ctx context.Context, interval time.Duration) {
	evictImageCacheIfOverLimit(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				evictImageCacheIfOverLimit(ctx)
			}
		}
	}()
}

type imageCacheEntry struct {
	path    string
	size    int64
	modTime time.Time
}

func evictImageCacheIfOverLimit(ctx context.Context) {
	limitMB := store.GetSettingInt(ctx, "images_cache_limit_mb")
	if limitMB <= 0 {
		return
	}
	limitBytes := int64(limitMB) * 1024 * 1024
	if imageCacheBytes.Load() <= limitBytes {
		return
	}

	entries, err := os.ReadDir(imageCacheDir)
	if err != nil {
		return
	}
	var files []imageCacheEntry
	var total int64
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, imageCacheEntry{
			path:    filepath.Join(imageCacheDir, e.Name()),
			size:    info.Size(),
			modTime: info.ModTime(),
		})
		total += info.Size()
	}
	if total <= limitBytes {
		imageCacheBytes.Store(total)
		imageCacheFiles.Store(int64(len(files)))
		return
	}

	sort.Slice(files, func(i, j int) bool { return files[i].modTime.Before(files[j].modTime) })
	removed := 0
	for _, f := range files {
		if total <= limitBytes {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
			removed++
		}
	}
	imageCacheBytes.Store(total)
	imageCacheFiles.Add(int64(-removed))
	log.Printf("imagecache: evicted %d files, now %.1f MB (limit %d MB)", removed, float64(total)/1024/1024, limitMB)
}
