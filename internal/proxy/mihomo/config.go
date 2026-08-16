// Package mihomo parses links/configs for protocols xray-core doesn't cover
// (VMess, Trojan, Shadowsocks, Hysteria2, TUIC, WireGuard) and drives a local
// mihomo (Clash Meta fork) sidecar process — mirroring internal/proxy/xray's
// approach for VLESS: the rest of the app only ever dials the plain SOCKS5
// port the sidecar exposes.
package mihomo

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseLink parses a proxy link (vmess/trojan/ss/hysteria2/tuic URI) or a raw
// WireGuard .conf into a mihomo proxy node. Returns the node's name (for the
// proxy-group reference) and its YAML-mappable fields.
func ParseLink(raw string) (name string, node map[string]any, err error) {
	s := strings.TrimSpace(raw)
	low := strings.ToLower(s)
	switch {
	case strings.HasPrefix(low, "vmess://"):
		return parseVMess(s)
	case strings.HasPrefix(low, "trojan://"):
		return parseTrojan(s)
	case strings.HasPrefix(low, "ss://"):
		return parseShadowsocks(s)
	case strings.HasPrefix(low, "hysteria2://"), strings.HasPrefix(low, "hy2://"):
		return parseHysteria2(s)
	case strings.HasPrefix(low, "tuic://"):
		return parseTUIC(s)
	case strings.Contains(s, "[Interface]"):
		return parseWireGuard(s)
	default:
		return "", nil, fmt.Errorf("unrecognized mihomo link/config format")
	}
}

func decodeB64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawURLEncoding.DecodeString(s)
}

// ─── VMess (v2rayN base64-JSON format) ─────────────────────────────────────

func parseVMess(raw string) (string, map[string]any, error) {
	payload := strings.TrimPrefix(raw, "vmess://")
	if i := strings.IndexAny(payload, "#"); i >= 0 {
		payload = payload[:i]
	}
	b, err := decodeB64(payload)
	if err != nil {
		return "", nil, fmt.Errorf("vmess: base64 decode: %w", err)
	}
	var v struct {
		V    string `json:"v"`
		PS   string `json:"ps"`
		Add  string `json:"add"`
		Port any    `json:"port"`
		ID   string `json:"id"`
		Aid  any    `json:"aid"`
		Net  string `json:"net"`
		Type string `json:"type"`
		Host string `json:"host"`
		Path string `json:"path"`
		TLS  string `json:"tls"`
		SNI  string `json:"sni"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", nil, fmt.Errorf("vmess: invalid json: %w", err)
	}
	if v.Add == "" || v.ID == "" {
		return "", nil, fmt.Errorf("vmess: missing address or id")
	}
	port, _ := strconv.Atoi(fmt.Sprint(v.Port))
	name := firstNonEmpty(v.PS, "vmess-"+v.Add)

	node := map[string]any{
		"name":       name,
		"type":       "vmess",
		"server":     v.Add,
		"port":       port,
		"uuid":       v.ID,
		"alterId":    atoiAny(v.Aid),
		"cipher":     "auto",
		"udp":        true,
		"tls":        v.TLS == "tls" || v.TLS == "reality",
		"network":    firstNonEmpty(v.Net, "tcp"),
		"servername": firstNonEmpty(v.SNI, v.Host),
	}
	switch v.Net {
	case "ws":
		node["ws-opts"] = map[string]any{
			"path": v.Path,
			"headers": map[string]any{
				"Host": firstNonEmpty(v.Host, v.SNI),
			},
		}
	case "grpc":
		node["grpc-opts"] = map[string]any{"grpc-service-name": v.Path}
	}
	return name, node, nil
}

func atoiAny(v any) int {
	n, _ := strconv.Atoi(fmt.Sprint(v))
	return n
}

// ─── Trojan ─────────────────────────────────────────────────────────────────

func parseTrojan(raw string) (string, map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", nil, fmt.Errorf("trojan: %w", err)
	}
	if u.User == nil || u.User.Username() == "" {
		return "", nil, fmt.Errorf("trojan: missing password")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return "", nil, fmt.Errorf("trojan: missing/invalid port: %w", err)
	}
	q := u.Query()
	name := firstNonEmpty(unescapeFragment(u.Fragment), "trojan-"+u.Hostname())
	node := map[string]any{
		"name":             name,
		"type":             "trojan",
		"server":           u.Hostname(),
		"port":             port,
		"password":         u.User.Username(),
		"sni":              firstNonEmpty(q.Get("sni"), q.Get("peer")),
		"udp":              true,
		"skip-cert-verify": q.Get("allowInsecure") == "1" || q.Get("insecure") == "1",
	}
	if net := q.Get("type"); net == "ws" {
		node["network"] = "ws"
		node["ws-opts"] = map[string]any{
			"path": q.Get("path"),
			"headers": map[string]any{
				"Host": firstNonEmpty(q.Get("host"), u.Hostname()),
			},
		}
	}
	return name, node, nil
}

// ─── Shadowsocks ────────────────────────────────────────────────────────────

func parseShadowsocks(raw string) (string, map[string]any, error) {
	trimmed := strings.TrimPrefix(raw, "ss://")
	var remark string
	if i := strings.Index(trimmed, "#"); i >= 0 {
		if r, err := url.QueryUnescape(trimmed[i+1:]); err == nil {
			remark = r
		}
		trimmed = trimmed[:i]
	}
	if i := strings.Index(trimmed, "?"); i >= 0 {
		trimmed = trimmed[:i]
	}

	var method, password, hostport string
	if at := strings.LastIndex(trimmed, "@"); at >= 0 {
		userinfo, hp := trimmed[:at], trimmed[at+1:]
		hostport = hp
		if dec, err := decodeB64(userinfo); err == nil && strings.Contains(string(dec), ":") {
			method, password, _ = strings.Cut(string(dec), ":")
		} else if strings.Contains(userinfo, ":") {
			method, password, _ = strings.Cut(userinfo, ":")
		} else {
			return "", nil, fmt.Errorf("ss: unrecognized userinfo encoding")
		}
	} else {
		dec, err := decodeB64(trimmed)
		if err != nil {
			return "", nil, fmt.Errorf("ss: base64 decode: %w", err)
		}
		methodPass, hp, ok := strings.Cut(string(dec), "@")
		if !ok {
			return "", nil, fmt.Errorf("ss: malformed legacy link")
		}
		method, password, _ = strings.Cut(methodPass, ":")
		hostport = hp
	}

	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return "", nil, fmt.Errorf("ss: host:port: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", nil, fmt.Errorf("ss: invalid port: %w", err)
	}

	name := firstNonEmpty(remark, "ss-"+host)
	node := map[string]any{
		"name":     name,
		"type":     "ss",
		"server":   host,
		"port":     port,
		"cipher":   method,
		"password": password,
		"udp":      true,
	}
	return name, node, nil
}

// ─── Hysteria2 ──────────────────────────────────────────────────────────────

func parseHysteria2(raw string) (string, map[string]any, error) {
	// Normalize the hy2:// alias to a scheme net/url recognizes the same way.
	normalized := raw
	if strings.HasPrefix(strings.ToLower(raw), "hy2://") {
		normalized = "hysteria2://" + raw[len("hy2://"):]
	}
	u, err := url.Parse(normalized)
	if err != nil {
		return "", nil, fmt.Errorf("hysteria2: %w", err)
	}
	if u.User == nil || u.User.Username() == "" {
		return "", nil, fmt.Errorf("hysteria2: missing password")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return "", nil, fmt.Errorf("hysteria2: missing/invalid port: %w", err)
	}
	q := u.Query()
	name := firstNonEmpty(unescapeFragment(u.Fragment), "hysteria2-"+u.Hostname())
	node := map[string]any{
		"name":             name,
		"type":             "hysteria2",
		"server":           u.Hostname(),
		"port":             port,
		"password":         u.User.Username(),
		"sni":              firstNonEmpty(q.Get("sni"), q.Get("peer")),
		"skip-cert-verify": q.Get("insecure") == "1",
	}
	if obfs := q.Get("obfs"); obfs != "" {
		node["obfs"] = obfs
		node["obfs-password"] = q.Get("obfs-password")
	}
	return name, node, nil
}

// ─── TUIC ───────────────────────────────────────────────────────────────────

func parseTUIC(raw string) (string, map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", nil, fmt.Errorf("tuic: %w", err)
	}
	if u.User == nil || u.User.Username() == "" {
		return "", nil, fmt.Errorf("tuic: missing uuid")
	}
	password, _ := u.User.Password()
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return "", nil, fmt.Errorf("tuic: missing/invalid port: %w", err)
	}
	q := u.Query()
	name := firstNonEmpty(unescapeFragment(u.Fragment), "tuic-"+u.Hostname())
	node := map[string]any{
		"name":                  name,
		"type":                  "tuic",
		"server":                u.Hostname(),
		"port":                  port,
		"uuid":                  u.User.Username(),
		"password":              password,
		"sni":                   firstNonEmpty(q.Get("sni"), u.Hostname()),
		"alpn":                  []string{firstNonEmpty(q.Get("alpn"), "h3")},
		"congestion-controller": firstNonEmpty(q.Get("congestion_control"), "bbr"),
		"udp-relay-mode":        firstNonEmpty(q.Get("udp_relay_mode"), "native"),
		"skip-cert-verify":      q.Get("allow_insecure") == "1",
	}
	return name, node, nil
}

// ─── WireGuard (raw .conf, [Interface]/[Peer] INI format) ──────────────────

func parseWireGuard(raw string) (string, map[string]any, error) {
	var section string
	kv := map[string]map[string]string{"Interface": {}, "Peer": {}}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok || kv[section] == nil {
			continue
		}
		kv[section][strings.TrimSpace(key)] = strings.TrimSpace(val)
	}

	iface, peer := kv["Interface"], kv["Peer"]
	if iface["PrivateKey"] == "" || peer["PublicKey"] == "" || peer["Endpoint"] == "" {
		return "", nil, fmt.Errorf("wireguard: missing PrivateKey/PublicKey/Endpoint")
	}
	host, portStr, err := net.SplitHostPort(peer["Endpoint"])
	if err != nil {
		return "", nil, fmt.Errorf("wireguard: invalid Endpoint: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", nil, fmt.Errorf("wireguard: invalid Endpoint port: %w", err)
	}

	ip := ""
	if addr := iface["Address"]; addr != "" {
		first, _, _ := strings.Cut(addr, ",")
		ip, _, _ = strings.Cut(strings.TrimSpace(first), "/")
	}

	name := "wg-" + host
	node := map[string]any{
		"name":        name,
		"type":        "wireguard",
		"server":      host,
		"port":        port,
		"ip":          ip,
		"private-key": iface["PrivateKey"],
		"public-key":  peer["PublicKey"],
		"udp":         true,
	}
	if psk := peer["PresharedKey"]; psk != "" {
		node["pre-shared-key"] = psk
	}
	if mtu := iface["MTU"]; mtu != "" {
		if n, err := strconv.Atoi(mtu); err == nil {
			node["mtu"] = n
		}
	}
	if dns := iface["DNS"]; dns != "" {
		var list []string
		for _, d := range strings.Split(dns, ",") {
			if d = strings.TrimSpace(d); d != "" {
				list = append(list, d)
			}
		}
		if len(list) > 0 {
			node["dns"] = list
		}
	}
	return name, node, nil
}

// ─── Shared helpers ─────────────────────────────────────────────────────────

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func unescapeFragment(s string) string {
	if r, err := url.QueryUnescape(s); err == nil {
		return r
	}
	return s
}

// BuildConfig renders the mihomo YAML config running a single proxy node
// behind a plain SOCKS5 listener on 127.0.0.1:socksPort.
func BuildConfig(name string, node map[string]any, socksPort int) ([]byte, error) {
	cfg := map[string]any{
		"mixed-port":          0,
		"socks-port":          socksPort,
		"bind-address":        "127.0.0.1",
		"allow-lan":           false,
		"mode":                "rule",
		"log-level":           "warning",
		"ipv6":                false,
		"external-controller": "",
		"proxies":             []any{node},
		"proxy-groups": []any{
			map[string]any{"name": "PROXY", "type": "select", "proxies": []string{name}},
		},
		"rules": []string{"MATCH,PROXY"},
	}
	return yaml.Marshal(cfg)
}
