package tmdb

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// buildSocks5Transport creates an http.Transport that routes all connections
// through a SOCKS5 proxy (socks5:// or socks5h://). DNS is resolved by the
// proxy server (socks5h behavior) because http.Transport passes the raw
// hostname to DialContext, which the SOCKS5 dialer forwards as-is.
func buildSocks5Transport(rawURL, user, pass string) (*http.Transport, error) {
	rawURL = strings.Replace(rawURL, "socks5h://", "socks5://", 1)
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	var auth *proxy.Auth
	if user != "" {
		auth = &proxy.Auth{User: user, Password: pass}
	} else if u.User != nil {
		p, _ := u.User.Password()
		auth = &proxy.Auth{User: u.User.Username(), Password: p}
	}
	d, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
	if err != nil {
		return nil, err
	}
	t := &http.Transport{
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if cd, ok := d.(proxy.ContextDialer); ok {
		t.DialContext = cd.DialContext
	} else {
		t.Dial = d.Dial //nolint:staticcheck
	}
	return t, nil
}

func readPageTmdb(path string, params map[string]string, results interface{}) error {
	link := tmdbEndpoint + path
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	link += "?" + q.Encode()

	retryCodes := map[int]bool{429: true, 500: true, 502: true, 503: true, 504: true}
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			time.Sleep(20 * time.Second)
		}
		req, err := http.NewRequest("GET", link, nil)
		if err != nil {
			return err
		}
		req.Header.Set("accept", "application/json")
		req.Header.Set("Authorization", TMDBAuthKey)

		resp, err := tmdbClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if retryCodes[resp.StatusCode] {
			resp.Body.Close()
			lastErr = errors.New(resp.Status)
			continue
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return errors.New(resp.Status)
		}
		err = json.NewDecoder(resp.Body).Decode(results)
		resp.Body.Close()
		return err
	}
	return lastErr
}
