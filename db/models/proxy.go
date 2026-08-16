package models

import "time"

type ProxyConfig struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`   // "socks5" | "vless"
	Config    string    `json:"config"` // socks5://... or vless://... (see internal/proxy/xray for vless)
	Enabled   bool      `json:"enabled"`
	Priority  int       `json:"priority"`
	CreatedAt time.Time `json:"created_at"`

	// Last result from the background healthcheck (internal/tasks/proxy_healthcheck.go),
	// so the admin UI can show a status dot without waiting for a fresh test.
	LastOK        *bool      `json:"last_ok,omitempty"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
	LastError     *string    `json:"last_error,omitempty"`
}

type ProxyRoute struct {
	Route    string `json:"route"`
	Label    string `json:"label,omitempty"`
	Enabled  bool   `json:"enabled"`
	ProxyIDs []int  `json:"proxy_ids"`
}
