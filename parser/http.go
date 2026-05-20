package parser

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
	"movies-api/config"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"

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
	return &http.Client{Jar: jar, Transport: transport, Timeout: 20 * time.Second}
}

func httpGetBytes(c *http.Client, link string) ([]byte, error) {
	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9")
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
