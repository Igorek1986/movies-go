package releases

import (
	"movies-api/client"
	"movies-api/config"
	"movies-api/db/models"
	"log"
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
		host := config.Get().Host
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
	var (
		body string
		err  error
	)
	for i := 0; i < 10; i++ {
		_, bodyS, errs := client.Get(link).End()
		body = bodyS
		if len(errs) > 0 {
			err = errs[0]
		} else {
			err = nil
		}
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
