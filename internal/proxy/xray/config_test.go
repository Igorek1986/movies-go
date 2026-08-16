package xray

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseLinkReality(t *testing.T) {
	// Synthetic values only — never put a real UUID/Reality key in a test that
	// might get committed to git.
	const testUUID = "00000000-0000-4000-8000-000000000000"
	const testKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	uri := "vless://" + testUUID + "@proxy.example.test:8443" +
		"?security=reality&type=tcp&headerType=&path=&host=&sni=example.test" +
		"&fp=chrome&pbk=" + testKey + "&sid=abcd1234#Test"

	link, err := ParseLink(uri)
	if err != nil {
		t.Fatalf("ParseLink: %v", err)
	}
	if link.UUID != testUUID {
		t.Errorf("UUID = %q", link.UUID)
	}
	if link.Address != "proxy.example.test" || link.Port != 8443 {
		t.Errorf("address/port = %q:%d", link.Address, link.Port)
	}
	if link.Security != "reality" || link.SNI != "example.test" || link.PublicKey != testKey || link.ShortID != "abcd1234" {
		t.Errorf("reality fields not parsed correctly: %+v", link)
	}
	if link.Remark != "Test" {
		t.Errorf("Remark = %q", link.Remark)
	}

	cfgJSON, err := BuildConfig(link, 10801)
	if err != nil {
		t.Fatalf("BuildConfig: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(cfgJSON, &cfg); err != nil {
		t.Fatalf("generated config is not valid JSON: %v\n%s", err, cfgJSON)
	}
	if !strings.Contains(string(cfgJSON), `"publicKey":"`+testKey+`"`) {
		t.Errorf("publicKey missing from generated config:\n%s", cfgJSON)
	}
	inbounds, _ := cfg["inbounds"].([]any)
	if len(inbounds) != 1 {
		t.Fatalf("expected 1 inbound, got %d", len(inbounds))
	}
	in0 := inbounds[0].(map[string]any)
	if in0["port"].(float64) != 10801 || in0["protocol"] != "socks" {
		t.Errorf("unexpected inbound: %+v", in0)
	}
}

func TestParseLinkErrors(t *testing.T) {
	cases := []string{
		"socks5://foo@bar:1080", // wrong scheme
		"vless://@host:443",     // missing UUID
		"vless://uuid@:443",     // missing host
		"vless://uuid@host",     // missing port
	}
	for _, c := range cases {
		if _, err := ParseLink(c); err == nil {
			t.Errorf("expected error for %q, got nil", c)
		}
	}
}
