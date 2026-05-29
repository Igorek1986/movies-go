package parser

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

func node2Text(node *html.Node) string {
	return strings.TrimSpace(strings.Replace((&goquery.Selection{Nodes: []*html.Node{node}}).Text(), " ", " ", -1))
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
