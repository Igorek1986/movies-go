package releases

import (
	"lampa-api/client"
	"lampa-api/config"
	"lampa-api/db/models"
	"log"
	"strings"
	"time"
)

// GetBodyLink fetches the torrent detail page from rutor and returns its HTML body.
func GetBodyLink(torr *models.TorrentDetails) string {
	host := config.Get().Host
	if host == "" {
		host = "http://rutor.info"
	}
	link := host + torr.Link
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
		if strings.Contains(link, "rutor.lib") {
			body, err = client.GetNic(link, "", "")
		} else {
			_, bodyS, errs := client.Get(link).End()
			body = bodyS
			if len(errs) > 0 {
				err = errs[0]
			} else {
				err = nil
			}
		}
		if err == nil || err == client.Err404 {
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
