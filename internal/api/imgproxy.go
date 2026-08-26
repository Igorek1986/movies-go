package api

import (
	"context"
	"io"
	"movies-api/internal/imagecache"
	"movies-api/internal/proxy"
	"net/http"
	"strings"
)

// handleImgProxy serves TMDB images through the configured proxy, optionally
// through a disk cache (see internal/imagecache — off by default).
// Route: GET /imgproxy/* where * = t/p/w300/abc.jpg
func handleImgProxy(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/imgproxy/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	cacheEnabled := imagecache.Enabled(r.Context())
	if cacheEnabled && imagecache.ServeFromCache(w, r, path) {
		return
	}

	client := proxy.Default.ClientFor(context.Background(), proxy.RouteImages)

	if cacheEnabled {
		body, ct, err := imagecache.FetchAndCache(client, path)
		if err != nil {
			http.Error(w, "proxy error", http.StatusBadGateway)
			return
		}
		go imagecache.WarmSibling(path)
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "public, max-age=604800")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write(body) //nolint:errcheck
		return
	}

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
