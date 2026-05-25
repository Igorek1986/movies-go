package models

import "time"

type ProxyConfig struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`   // "socks5"
	Config    string    `json:"config"` // socks5://...
	Enabled   bool      `json:"enabled"`
	Priority  int       `json:"priority"`
	CreatedAt time.Time `json:"created_at"`
}

type ProxyRoute struct {
	Route    string `json:"route"`
	Label    string `json:"label,omitempty"`
	Enabled  bool   `json:"enabled"`
	ProxyIDs []int  `json:"proxy_ids"`
}
