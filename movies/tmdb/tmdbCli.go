package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"time"
)

func readPageTmdb(path string, params map[string]string, results interface{}) error {
	link := tmdbEndpoint + path
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	link += "?" + q.Encode()

	retryCodes := map[int]bool{429: true, 500: true, 502: true, 503: true, 504: true}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(2 * time.Second)
		}
		req, err := http.NewRequestWithContext(context.Background(), "GET", link, nil)
		if err != nil {
			return err
		}
		req.Header.Set("accept", "application/json")
		req.Header.Set("Authorization", TMDBAuthKey)

		resp, err := HTTPClient().Do(req)
		if err != nil {
			log.Printf("tmdb: network error (attempt %d/3) %s: %v", attempt+1, path, err)
			lastErr = err
			continue
		}
		if resp.StatusCode == 429 {
			resp.Body.Close()
			retryAfter := resp.Header.Get("Retry-After")
			log.Printf("tmdb: rate limit 429 (attempt %d/3) %s — Retry-After: %s", attempt+1, path, retryAfter)
			lastErr = errors.New(resp.Status)
			continue
		}
		if retryCodes[resp.StatusCode] {
			resp.Body.Close()
			log.Printf("tmdb: server error %s (attempt %d/3) %s", resp.Status, attempt+1, path)
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
