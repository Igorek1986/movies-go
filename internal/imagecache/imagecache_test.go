package imagecache

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withTempCacheDir points cacheDir at a fresh temp dir and clears the
// in-memory hit-tracking map/counters, so tests don't see state left by a
// previous test or a real running server.
func withTempCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := cacheDir
	cacheDir = dir
	t.Cleanup(func() { cacheDir = old })

	lastServedMu.Lock()
	lastServed = map[string]time.Time{}
	lastServedMu.Unlock()

	return dir
}

func TestRecencyForPrefersLastServedOverModTime(t *testing.T) {
	withTempCacheDir(t)

	modTime := time.Now().Add(-24 * time.Hour)  // written a day ago
	hitTime := time.Now().Add(-1 * time.Minute) // but served a minute ago

	fp := filepath.Join(cacheDir, "hot.jpg")
	lastServedMu.Lock()
	lastServed[fp] = hitTime
	lastServedMu.Unlock()

	got := recencyFor(fp, modTime)
	if !got.Equal(hitTime) {
		t.Fatalf("recencyFor: got %v, want lastServed hit time %v (should win over older mtime)", got, hitTime)
	}
}

func TestRecencyForFallsBackToModTimeWhenNeverServed(t *testing.T) {
	withTempCacheDir(t)

	modTime := time.Now().Add(-24 * time.Hour)
	fp := filepath.Join(cacheDir, "cold.jpg")

	got := recencyFor(fp, modTime)
	if !got.Equal(modTime) {
		t.Fatalf("recencyFor: got %v, want modTime %v (no hit recorded, should fall back)", got, modTime)
	}
}

func TestServeFromCacheRecordsHit(t *testing.T) {
	dir := withTempCacheDir(t)

	tmdbPath := "t/p/w500/hot.jpg"
	fp := filepath.Join(dir, "t_p_w500_hot.jpg")
	if err := os.WriteFile(fp, []byte("fake-jpeg-bytes"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// Backdate the file so a fresh recencyFor call (no hit yet) reports the
	// old write time, not "now" — makes the before/after hit comparison mean
	// something.
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(fp, old, old); err != nil {
		t.Fatalf("setup chtimes: %v", err)
	}

	before := recencyFor(fp, old)
	if !before.Equal(old) {
		t.Fatalf("before hit: recencyFor = %v, want %v", before, old)
	}

	req := httptest.NewRequest("GET", "/imgproxy/"+tmdbPath, nil)
	w := httptest.NewRecorder()
	if !ServeFromCache(w, req, tmdbPath) {
		t.Fatalf("ServeFromCache: expected a hit, got a miss")
	}
	if w.Code != 200 {
		t.Fatalf("ServeFromCache: status = %d, want 200", w.Code)
	}

	after := recencyFor(fp, old)
	if !after.After(before) {
		t.Fatalf("after hit: recencyFor = %v, want something after %v (ServeFromCache should have recorded the hit)", after, before)
	}
}

// TestEvictionOrderFavorsRecentlyServedFile is the actual regression this
// change is about: two files with the same age (mtime), one of them "hot"
// (recently served) — eviction (given a size-sorted list, oldest-recency
// first) must pick the cold one first, not whichever happened to be written
// first.
func TestEvictionOrderFavorsRecentlyServedFile(t *testing.T) {
	dir := withTempCacheDir(t)

	writeTime := time.Now().Add(-24 * time.Hour)
	coldFP := filepath.Join(dir, "cold.jpg")
	hotFP := filepath.Join(dir, "hot.jpg")
	for _, fp := range []string{coldFP, hotFP} {
		if err := os.WriteFile(fp, []byte("x"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.Chtimes(fp, writeTime, writeTime); err != nil {
			t.Fatalf("setup chtimes: %v", err)
		}
	}

	// hot.jpg gets a cache hit just now; cold.jpg never does.
	lastServedMu.Lock()
	lastServed[hotFP] = time.Now()
	lastServedMu.Unlock()

	entries := []cacheEntry{
		{path: coldFP, size: 1, recency: recencyFor(coldFP, writeTime)},
		{path: hotFP, size: 1, recency: recencyFor(hotFP, writeTime)},
	}

	// Same ordering evictIfOverLimit uses: oldest recency evicted first.
	oldest := entries[0]
	for _, e := range entries[1:] {
		if e.recency.Before(oldest.recency) {
			oldest = e
		}
	}

	if oldest.path != coldFP {
		t.Fatalf("eviction would pick %q first, want %q (the never-served one)", oldest.path, coldFP)
	}
}

func TestSweepStaleTmpRemovesOldButKeepsFresh(t *testing.T) {
	dir := withTempCacheDir(t)

	stale := filepath.Join(dir, "t_p_w500_stale.jpg.tmp")
	fresh := filepath.Join(dir, "t_p_w500_fresh.jpg.tmp")
	for _, fp := range []string{stale, fresh} {
		if err := os.WriteFile(fp, []byte("x"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	// stale.tmp looks like it's been sitting there since well before a
	// write+rename (milliseconds) would ever normally take.
	old := time.Now().Add(-2 * staleTmpAge)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("setup chtimes: %v", err)
	}

	sweepStaleTmp()

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale.tmp should have been swept, stat err = %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh.tmp should NOT have been swept (too young to be litter): %v", err)
	}
}
