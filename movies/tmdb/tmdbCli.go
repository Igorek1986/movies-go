package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"movies-api/db/store"
)

func tmdbRetryWait() time.Duration {
	if n := store.GetSettingInt(context.Background(), "tmdb_retry_wait_sec"); n > 0 {
		return time.Duration(n) * time.Second
	}
	return 10 * time.Second
}

func tmdbMaxAttempts() int {
	if n := store.GetSettingInt(context.Background(), "tmdb_retry_attempts"); n > 0 {
		return n
	}
	return 5
}

func readPageTmdb(path string, params map[string]string, results interface{}) error {
	link := tmdbEndpoint + path
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	link += "?" + q.Encode()

	maxAttempts := tmdbMaxAttempts()
	retryCodes := map[int]bool{500: true, 502: true, 503: true, 504: true}
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
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
			if attempt+1 < maxAttempts {
				time.Sleep(tmdbRetryWait())
			}
			continue
		}
		if resp.StatusCode == 429 {
			resp.Body.Close()
			w := tmdbRetryWait()
			if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
				if ra := time.Duration(secs) * time.Second; ra > w {
					w = ra
				}
			}
			log.Printf("tmdb: rate limit 429 (attempt %d/%d) %s — ждём %s", attempt+1, maxAttempts, path, w)
			lastErr = errors.New(resp.Status)
			if attempt+1 < maxAttempts {
				time.Sleep(w)
			}
			continue
		}
		if retryCodes[resp.StatusCode] {
			resp.Body.Close()
			log.Printf("tmdb: server error %s (attempt %d/%d) %s", resp.Status, attempt+1, maxAttempts, path)
			lastErr = errors.New(resp.Status)
			if attempt+1 < maxAttempts {
				time.Sleep(tmdbRetryWait())
			}
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
