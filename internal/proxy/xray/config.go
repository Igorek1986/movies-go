// Package xray parses vless:// links and drives a local xray-core sidecar
// process per proxy config — the rest of the app only ever talks to the plain
// SOCKS5 port that sidecar exposes, so VLESS/Reality itself never has to be
// reimplemented here (that's crypto-sensitive protocol work best left to the
// reference xray-core binary).
package xray

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Link is a parsed vless:// URI.
type Link struct {
	UUID        string
	Address     string
	Port        int
	Encryption  string // always "none" for VLESS, kept for completeness
	Flow        string // e.g. "xtls-rprx-vision", "" if absent
	Security    string // "reality" | "tls" | "none" | "" (treated as none)
	Network     string // "type" param: tcp/ws/grpc/http..., default "tcp"
	HeaderType  string
	Path        string
	Host        string // ws/http Host header
	SNI         string
	Fingerprint string // uTLS fingerprint, e.g. "chrome"
	PublicKey   string // Reality pbk
	ShortID     string // Reality sid
	SpiderX     string // Reality spx (rarely used)
	Remark      string // URI fragment, display-only
}

// ParseLink parses a vless:// URI (as exported by 3x-ui and similar panels)
// into its structured fields.
func ParseLink(raw string) (*Link, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid vless URI: %w", err)
	}
	if u.Scheme != "vless" {
		return nil, fmt.Errorf("not a vless:// URI (scheme=%q)", u.Scheme)
	}
	if u.User == nil || u.User.Username() == "" {
		return nil, fmt.Errorf("vless URI missing UUID (user part)")
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("vless URI missing host")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return nil, fmt.Errorf("vless URI missing/invalid port: %w", err)
	}

	q := u.Query()
	l := &Link{
		UUID:        u.User.Username(),
		Address:     u.Hostname(),
		Port:        port,
		Encryption:  firstNonEmpty(q.Get("encryption"), "none"),
		Flow:        q.Get("flow"),
		Security:    q.Get("security"),
		Network:     firstNonEmpty(q.Get("type"), "tcp"),
		HeaderType:  q.Get("headerType"),
		Path:        q.Get("path"),
		Host:        q.Get("host"),
		SNI:         q.Get("sni"),
		Fingerprint: q.Get("fp"),
		PublicKey:   q.Get("pbk"),
		ShortID:     q.Get("sid"),
		SpiderX:     q.Get("spx"),
		Remark:      u.Fragment,
	}
	return l, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// BuildConfig renders the xray-core JSON config for a single VLESS outbound
// with a plain SOCKS5 inbound on 127.0.0.1:socksPort — the local bridge the
// rest of the app dials into (see internal/proxy/manager.go).
func BuildConfig(l *Link, socksPort int) ([]byte, error) {
	stream := map[string]any{
		"network": l.Network,
	}
	switch l.Security {
	case "reality":
		stream["security"] = "reality"
		stream["realitySettings"] = map[string]any{
			"serverName":  l.SNI,
			"fingerprint": firstNonEmpty(l.Fingerprint, "chrome"),
			"publicKey":   l.PublicKey,
			"shortId":     l.ShortID,
			"spiderX":     l.SpiderX,
		}
	case "tls":
		stream["security"] = "tls"
		stream["tlsSettings"] = map[string]any{
			"serverName":  firstNonEmpty(l.SNI, l.Address),
			"fingerprint": firstNonEmpty(l.Fingerprint, "chrome"),
		}
	default:
		stream["security"] = "none"
	}

	switch l.Network {
	case "ws":
		stream["wsSettings"] = map[string]any{
			"path": l.Path,
			"headers": map[string]any{
				"Host": firstNonEmpty(l.Host, l.SNI),
			},
		}
	case "grpc":
		stream["grpcSettings"] = map[string]any{
			"serviceName": l.Path,
		}
	}

	user := map[string]any{
		"id":         l.UUID,
		"encryption": firstNonEmpty(l.Encryption, "none"),
	}
	if l.Flow != "" {
		user["flow"] = l.Flow
	}

	cfg := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{
			map[string]any{
				"listen":   "127.0.0.1",
				"port":     socksPort,
				"protocol": "socks",
				"settings": map[string]any{"udp": true, "auth": "noauth"},
			},
		},
		"outbounds": []any{
			map[string]any{
				"protocol": "vless",
				"settings": map[string]any{
					"vnext": []any{
						map[string]any{
							"address": l.Address,
							"port":    l.Port,
							"users":   []any{user},
						},
					},
				},
				"streamSettings": stream,
			},
		},
	}
	return json.Marshal(cfg)
}
