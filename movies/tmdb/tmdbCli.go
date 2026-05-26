package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"time"

	"movies-api/db/store"
)

func tmdbRetryOpts() (attempts int, baseWait, maxWait time.Duration) {
	ctx := context.Background()
	attempts = store.GetSettingInt(ctx, "tmdb_retry_attempts")
	if attempts <= 0 {
		attempts = 5
	}
	baseSec := store.GetSettingInt(ctx, "tmdb_retry_base_wait_sec")
	if baseSec <= 0 {
		baseSec = 2
	}
	maxSec := store.GetSettingInt(ctx, "tmdb_retry_max_wait_sec")
	if maxSec <= 0 {
		maxSec = 8
	}
	return attempts, time.Duration(baseSec) * time.Second, time.Duration(maxSec) * time.Second
}

func readPageTmdb(path string, params map[string]string, results interface{}) error {
	link := tmdbEndpoint + path
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	link += "?" + q.Encode()

	maxAttempts, baseWait, maxWait := tmdbRetryOpts()
	retryCodes := map[int]bool{429: true, 500: true, 502: true, 503: true, 504: true}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			w := baseWait * time.Duration(1<<(attempt-1))
			if w > maxWait {
				w = maxWait
			}
			time.Sleep(w)
		}
		req, err := http.NewRequestWithContext(context.Background(), "GET", link, nil)
		if err != nil {
			return err
		}
		req.Header.Set("accept", "application/json")
		req.Header.Set("Authorization", TMDBAuthKey)

		resp, err := HTTPClient().Do(req)
		if err != nil {
			log.Printf("tmdb: network error (attempt %d/%d) %s: %v", attempt+1, maxAttempts, path, err)
			lastErr = err
			continue
		}
		if resp.StatusCode == 429 {
			resp.Body.Close()
			retryAfter := resp.Header.Get("Retry-After")
			log.Printf("tmdb: rate limit 429 (attempt %d/%d) %s — Retry-After: %s", attempt+1, maxAttempts, path, retryAfter)
			lastErr = errors.New(resp.Status)
			continue
		}
		if retryCodes[resp.StatusCode] {
			resp.Body.Close()
			log.Printf("tmdb: server error %s (attempt %d/%d) %s", resp.Status, attempt+1, maxAttempts, path)
			lastErr = errors.New(resp.Status)
			continue
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			log.Printf("tmdb: unexpected status %s %s", resp.Status, path)
			return errors.New(resp.Status)
		}
		err = json.NewDecoder(resp.Body).Decode(results)
		resp.Body.Close()
		return err
	}
	return lastErr
}
