package api

import (
	"movies-api/db/store"
	"net/http"
	"sync"
	"time"
)

type ipEntry struct {
	count     int
	windowEnd time.Time
}

type ipRateLimiter struct {
	mu      sync.Mutex
	entries map[string]*ipEntry
}

func newIPRateLimiter() *ipRateLimiter {
	rl := &ipRateLimiter{entries: make(map[string]*ipEntry)}
	go rl.cleanup()
	return rl
}

func (rl *ipRateLimiter) allow(ip string, max int, windowSec int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	e := rl.entries[ip]
	if e == nil || now.After(e.windowEnd) {
		rl.entries[ip] = &ipEntry{count: 1, windowEnd: now.Add(time.Duration(windowSec) * time.Second)}
		return true
	}
	e.count++
	return e.count <= max
}

func (rl *ipRateLimiter) cleanup() {
	t := time.NewTicker(5 * time.Minute)
	for range t.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, e := range rl.entries {
			if now.After(e.windowEnd) {
				delete(rl.entries, ip)
			}
		}
		rl.mu.Unlock()
	}
}

var (
	loginRL    = newIPRateLimiter()
	registerRL = newIPRateLimiter()
	forgotRL   = newIPRateLimiter()
	twoFARL    = newIPRateLimiter()
)

func rateLimitMiddleware(rl *ipRateLimiter, maxKey, windowKey string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		max := store.GetSettingInt(r.Context(), maxKey)
		window := store.GetSettingInt(r.Context(), windowKey)
		if max <= 0 {
			max = 10
		}
		if window <= 0 {
			window = 900
		}
		ip := realIP(r)
		if !rl.allow(ip, max, window) {
			Error(w, http.StatusTooManyRequests, "too many attempts, try later")
			return
		}
		next(w, r)
	}
}
