package parser

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
	"movies-api/internal/proxy"
)

func get(link string) (string, error) {
	link = strings.TrimSpace(link)
	link = strings.ReplaceAll(link, "\t", "%20")
	client := proxy.Default.ClientFor(context.Background(), proxy.RouteParserRutor)
	var (
		body string
		err  error
	)
	for i := 0; i < 10; i++ {
		b, e := httpGetBytes(client, link)
		if e == nil {
			return string(b), nil
		}
		err = e
		log.Println("Error get page, tries:", i+1, link, err)
		if i < 5 {
			time.Sleep(time.Minute)
		} else {
			time.Sleep(time.Minute * 10)
		}
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
