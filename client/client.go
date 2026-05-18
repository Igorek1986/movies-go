package client

import (
	"lampa-api/config"
	"net/http"
	"time"

	"github.com/parnurzeal/gorequest"
)

func Get(link string) *gorequest.SuperAgent {
	return GetParam(link, "", "")
}

func GetParam(link, referer, cookie string) *gorequest.SuperAgent {
	agent := gorequest.New()

	if cookie != "" {
		header := http.Header{}
		header.Add("Cookie", cookie)
		request := http.Request{
			Header: header,
		}
		agent.Cookies = request.Cookies()
	}

	if referer != "" {
		agent.AppendHeader("referer", referer)
	}

	if config.ProxyHost != "" {
		agent.Proxy(config.ProxyHost)
	}
	agent.Timeout(30 * time.Second)
	return agent.Get(link)
}
