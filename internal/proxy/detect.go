package proxy

import (
	"fmt"
	"strings"
)

// DetectType inspects a pasted proxy link/config and returns the canonical
// value to store in proxy_configs.type. This is the source of truth for what
// protocol a link is — the admin UI never sends a type for anything other
// than "socks5" (which has its own host/port form, not a pasted link); every
// other type is derived here, server-side, from the actual content.
func DetectType(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	low := strings.ToLower(s)
	switch {
	case strings.HasPrefix(low, "socks5://"), strings.HasPrefix(low, "socks5h://"):
		return "socks5", nil
	case strings.HasPrefix(low, "vless://"):
		return "vless", nil
	case strings.HasPrefix(low, "vmess://"):
		return "vmess", nil
	case strings.HasPrefix(low, "trojan://"):
		return "trojan", nil
	case strings.HasPrefix(low, "ss://"):
		return "ss", nil
	case strings.HasPrefix(low, "hysteria2://"), strings.HasPrefix(low, "hy2://"):
		return "hysteria2", nil
	case strings.HasPrefix(low, "tuic://"):
		return "tuic", nil
	case strings.Contains(s, "[Interface]"):
		return "wireguard", nil
	default:
		return "", fmt.Errorf("unrecognized proxy link/config format")
	}
}

// mihomoTypes are the proxy_configs.type values driven by the mihomo sidecar
// (internal/proxy/mihomo) — everything except socks5 (dialed directly) and
// vless (driven by the xray sidecar, internal/proxy/xray).
var mihomoTypes = map[string]bool{
	"vmess": true, "trojan": true, "ss": true,
	"hysteria2": true, "tuic": true, "wireguard": true,
}
