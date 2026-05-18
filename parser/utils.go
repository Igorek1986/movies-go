package parser

import (
	"movies-api/client"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
	"log"
	"strings"
	"time"
)

func get(link string) (string, error) {
	var body string
	var err error
	link = strings.TrimSpace(link)
	link = strings.ReplaceAll(link, "\t", "%20")
	for i := 0; i < 10; i++ {
		if strings.Contains(link, "\t") {
			link = strings.Replace(link, "\t", "", -1)
		}
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
		log.Println("Error get page,tryes:", i+1, link, err)
		if i < 5 {
			time.Sleep(time.Minute)
		} else {
			time.Sleep(time.Minute * 10)
		}
	}
	return body, err
}

func getBuf(link, referer, cookie string) ([]byte, error) {
	var body []byte
	var err error
	for i := 0; i < 10; i++ {
		_, bodyS, errs := client.GetParam(link, referer, cookie).EndBytes()
		body = bodyS
		if len(errs) > 0 {
			err = errs[0]
		}
		if err == nil {
			break
		}
		log.Println("Error get page,tryes:", i+1, link)
		time.Sleep(time.Second * 2)
	}
	return body, err
}

func node2Text(node *html.Node) string {
	return strings.TrimSpace(strings.Replace((&goquery.Selection{Nodes: []*html.Node{node}}).Text(), " ", " ", -1))
}

func replaceBadName(name string) string {
	name = strings.ReplaceAll(name, "Ванда/Вижн ", "ВандаВижн ")
	name = strings.ReplaceAll(name, "Ё", "Е")
	name = strings.ReplaceAll(name, "ё", "е")
	name = strings.ReplaceAll(name, "щ", "ш")
	return name
}

func getHash(magnet string) string {
	pos := strings.Index(magnet, "btih:")
	if pos == -1 {
		return ""
	}
	magnet = magnet[pos+5:]
	pos = strings.Index(magnet, "&")
	if pos == -1 {
		return strings.ToLower(magnet)
	}
	return strings.ToLower(magnet[:pos])
}
