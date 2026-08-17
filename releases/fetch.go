package releases

import (
	"context"
	"io"
	"log"
	"movies-api/db/models"
	"movies-api/db/store"
	"movies-api/internal/proxy"
	"net/http"
	"strings"
	"time"
)

// GetBodyLink fetches the torrent detail page and returns its HTML body.
// For rutor, torr.Link is a relative path that is resolved against the rutor host.
// For other trackers, torr.Link is an absolute URL used as-is.
func GetBodyLink(torr *models.TorrentDetails) string {
	if torr.Link == "" {
		return ""
	}
	link := torr.Link
	if !strings.HasPrefix(link, "http://") && !strings.HasPrefix(link, "https://") {
		host, _ := store.GetSetting(context.Background(), "rutor_host")
		if host == "" {
			host = "http://rutor.info"
		}
		link = host + link
	}
	body, err := fetchPage(link)
	if err != nil {
		log.Println("Error get torrent page:", err, link)
		return ""
	}
	return body
}

func fetchPage(link string) (string, error) {
	link = strings.TrimSpace(link)
	link = strings.ReplaceAll(link, "\t", "%20")
	c := proxy.Default.ClientFor(context.Background(), proxy.RouteParserRutor)
	var (
		body string
		err  error
	)
	for i := range 10 {
		body, err = httpGetString(c, link)
		if err == nil {
			break
		}
		log.Println("Error fetching page, attempt:", i+1, link, err)
		if i < 5 {
			time.Sleep(time.Minute)
		} else {
			time.Sleep(time.Minute * 10)
		}
	}
	return body, err
}

func httpGetString(c *http.Client, link string) (string, error) {
	req, err := http.NewRequest("GET", link, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
