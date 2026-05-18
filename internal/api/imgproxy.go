package api

import (
	"context"
	"io"
	"movies-api/config"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

var (
	imgProxyOnce   sync.Once
	imgProxyClient *http.Client
)

// handleImgProxy serves TMDB images through the configured SOCKS5 proxy.
// Route: GET /imgproxy/* where * = t/p/w300/abc.jpg
func handleImgProxy(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	if cfg.ProxyURL == "" {
		http.NotFound(w, r)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/imgproxy/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	client := getImgProxyClient(cfg)
	tmdbURL := "https://image.tmdb.org/" + path

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, tmdbURL, nil)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "proxy error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "upstream error", resp.StatusCode)
		return
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/jpeg"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=604800")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	io.Copy(w, resp.Body) //nolint:errcheck
}

func getImgProxyClient(cfg *config.ConfigParser) *http.Client {
	imgProxyOnce.Do(func() {
		u, err := url.Parse(strings.Replace(cfg.ProxyURL, "socks5h://", "socks5://", 1))
		if err != nil {
			imgProxyClient = &http.Client{Timeout: 20 * time.Second}
			return
		}

		var auth *proxy.Auth
		if cfg.ProxyUser != "" {
			auth = &proxy.Auth{User: cfg.ProxyUser, Password: cfg.ProxyPass}
		} else if u.User != nil {
			pass, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: pass}
		}

		dialer, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
		if err != nil {
			imgProxyClient = &http.Client{Timeout: 20 * time.Second}
			return
		}

		imgProxyClient = &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return dialer.Dial(network, addr)
				},
				MaxIdleConns:        20,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		}
	})
	return imgProxyClient
}
