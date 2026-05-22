package parser

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
	"movies-api/config"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"

func newDirectHTTPClient() *http.Client {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	return &http.Client{Jar: jar, Transport: &http.Transport{}, Timeout: 20 * time.Second}
}

func newHTTPClient() *http.Client {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	transport := &http.Transport{}
	cfg := config.Get()
	if cfg.ProxyURL != "" {
		if u, err := url.Parse(cfg.ProxyURL); err == nil {
			if cfg.ProxyUser != "" {
				u.User = url.UserPassword(cfg.ProxyUser, cfg.ProxyPass)
			}
			transport.Proxy = http.ProxyURL(u)
		}
	}
	// 30s for proxy client: SOCKS5 tunnel setup can take 2-5s
	return &http.Client{Jar: jar, Transport: transport, Timeout: 30 * time.Second}
}

// fetchBytesRetry fetches url using the proxy client, falling back to direct on
// network error. Retries up to maxAttempts with geometric backoff.
func fetchBytesRetry(proxy, direct *http.Client, url string, maxAttempts int, baseWait, maxWait time.Duration, ratio float64) ([]byte, error) {
	wait := baseWait
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			w := wait
			if w > maxWait {
				w = maxWait
			}
			log.Printf("parser: retry %d/%d in %s for %s", attempt, maxAttempts-1, w.Round(time.Second), url)
			time.Sleep(w)
			wait = time.Duration(float64(wait) * ratio)
		}
		body, err := httpGetBytes(proxy, url)
		if err != nil {
			// proxy failed — try direct
			if body2, err2 := httpGetBytes(direct, url); err2 == nil {
				return body2, nil
			}
			lastErr = err
			continue
		}
		return body, nil
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
