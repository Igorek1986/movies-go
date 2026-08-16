package mihomo

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseVMess(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"v": "2", "ps": "Test", "add": "proxy.example.test", "port": "443",
		"id": "00000000-0000-4000-8000-000000000000", "aid": "0",
		"net": "ws", "type": "none", "host": "proxy.example.test", "path": "/ray",
		"tls": "tls", "sni": "proxy.example.test",
	})
	uri := "vmess://" + base64.StdEncoding.EncodeToString(payload)

	name, node, err := ParseLink(uri)
	if err != nil {
		t.Fatalf("ParseLink: %v", err)
	}
	if name != "Test" {
		t.Errorf("name = %q", name)
	}
	if node["type"] != "vmess" || node["server"] != "proxy.example.test" || node["port"] != 443 {
		t.Errorf("unexpected node: %+v", node)
	}
	if _, err := BuildConfig(name, node, 10900); err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
}

func TestParseTrojan(t *testing.T) {
	uri := "trojan://secret@proxy.example.test:443?sni=proxy.example.test&allowInsecure=1#Test"
	name, node, err := ParseLink(uri)
	if err != nil {
		t.Fatalf("ParseLink: %v", err)
	}
	if name != "Test" || node["password"] != "secret" || node["port"] != 443 {
		t.Errorf("unexpected: name=%q node=%+v", name, node)
	}
}

func TestParseShadowsocks(t *testing.T) {
	userinfo := base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:secret"))
	uri := "ss://" + userinfo + "@proxy.example.test:8388#Test"
	name, node, err := ParseLink(uri)
	if err != nil {
		t.Fatalf("ParseLink: %v", err)
	}
	if name != "Test" || node["cipher"] != "aes-256-gcm" || node["password"] != "secret" || node["port"] != 8388 {
		t.Errorf("unexpected: name=%q node=%+v", name, node)
	}
}

func TestParseShadowsocksLegacy(t *testing.T) {
	whole := base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:secret@proxy.example.test:8388"))
	uri := "ss://" + whole
	_, node, err := ParseLink(uri)
	if err != nil {
		t.Fatalf("ParseLink: %v", err)
	}
	if node["cipher"] != "aes-256-gcm" || node["password"] != "secret" {
		t.Errorf("unexpected: %+v", node)
	}
}

func TestParseHysteria2(t *testing.T) {
	for _, scheme := range []string{"hysteria2", "hy2"} {
		uri := scheme + "://secret@proxy.example.test:443?sni=proxy.example.test&insecure=1#Test"
		name, node, err := ParseLink(uri)
		if err != nil {
			t.Fatalf("ParseLink(%s): %v", scheme, err)
		}
		if name != "Test" || node["type"] != "hysteria2" || node["password"] != "secret" {
			t.Errorf("%s: unexpected: name=%q node=%+v", scheme, name, node)
		}
	}
}

func TestParseTUIC(t *testing.T) {
	uri := "tuic://00000000-0000-4000-8000-000000000000:secret@proxy.example.test:443?sni=proxy.example.test#Test"
	name, node, err := ParseLink(uri)
	if err != nil {
		t.Fatalf("ParseLink: %v", err)
	}
	if name != "Test" || node["uuid"] != "00000000-0000-4000-8000-000000000000" || node["password"] != "secret" {
		t.Errorf("unexpected: name=%q node=%+v", name, node)
	}
}

func TestParseWireGuard(t *testing.T) {
	conf := `[Interface]
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Address = 10.0.0.2/32
DNS = 1.1.1.1

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
Endpoint = proxy.example.test:51820
AllowedIPs = 0.0.0.0/0
`
	name, node, err := ParseLink(conf)
	if err != nil {
		t.Fatalf("ParseLink: %v", err)
	}
	if !strings.HasPrefix(name, "wg-") {
		t.Errorf("name = %q", name)
	}
	if node["server"] != "proxy.example.test" || node["port"] != 51820 || node["ip"] != "10.0.0.2" {
		t.Errorf("unexpected node: %+v", node)
	}
	if _, err := BuildConfig(name, node, 10901); err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
}

func TestParseLinkUnrecognized(t *testing.T) {
	if _, _, err := ParseLink("not-a-link"); err == nil {
		t.Error("expected error for unrecognized input")
	}
}
