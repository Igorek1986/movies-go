package models

import "time"

type ProxyConfig struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	// "socks5" | "vless" | "vmess" | "trojan" | "ss" | "hysteria2" | "tuic" | "wireguard" | "warp".
	// Always server-derived from Config via proxy.DetectType — never trust a
	// client-supplied value here except for the socks5 host/port form.
	Type string `json:"type"`
	// socks5://..., a protocol link (vless/vmess/trojan/ss/hysteria2/tuic), a raw
	// WireGuard .conf, or (type=warp) mihomo.EncodeWarpConfig's "warp://" blob.
	Config    string    `json:"config"`
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
