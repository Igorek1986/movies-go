package parser

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"movies-api/internal/proxy"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"

func clientForRoute(route string) *http.Client {
	return proxy.Default.ClientFor(context.Background(), route)
}

// fetchBytesRetry fetches url using the proxy client, falling back to direct on
// network error. Retries up to maxAttempts with geometric backoff.
func fetchBytesRetry(proxyClient *http.Client, url string, maxAttempts int, baseWait, maxWait time.Duration, ratio float64) ([]byte, error) {
	wait := baseWait
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			w := wait
			if w > maxWait {
				w = maxWait
			}
			log.Printf("parser: retry %d/%d in %s for %s", attempt, maxAttempts-1, w.Round(time.Second), url)
			if !interruptibleSleep(w) {
				return nil, fmt.Errorf("stop requested")
			}
			wait = time.Duration(float64(wait) * ratio)
		}
		body, err := httpGetBytes(proxyClient, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("недоступно после %d попыток: %w", maxAttempts, lastErr)
}

func httpGetBytes(c *http.Client, link string) ([]byte, error) {
	return httpGetBytesRef(c, link, "")
}

func httpGetBytesRef(c *http.Client, link, referer string) ([]byte, error) {
	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func httpPostForm(c *http.Client, link string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequest("POST", link, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	return c.Do(req)
}
