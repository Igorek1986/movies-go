// Package warp registers a new Cloudflare WARP device (the same undocumented
// endpoint the official WARP client/wgcf use) and produces the WireGuard
// parameters needed to tunnel through it — including the account-specific
// "reserved" bytes WARP's peer requires on top of stock WireGuard.
package warp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/curve25519"
)

// v0a2158 is the API version wgcf (github.com/ViRb3/wgcf) and the official
// WARP client currently use — Cloudflare has bumped this string before with
// no announcement, so a 404 here after this has worked for a while likely
// means it moved again.
const registerURL = "https://api.cloudflareclient.com/v0a2158/reg"

// Account is everything needed to build a mihomo WireGuard node for this
// WARP device — see internal/proxy/mihomo's warp:// link handling.
type Account struct {
	PrivateKey string // ours, base64
	Address4   string // assigned tunnel IPv4, no /mask
	Endpoint   string // "host:port"
	PeerPubKey string // Cloudflare's WireGuard public key
	Reserved   [3]byte
}

// Register creates a new (free-tier) WARP device and returns its WireGuard
// parameters. Each call creates a distinct device on the Cloudflare account
// side — there's no "log back into an existing one" here, matching what
// wgcf/the official client do on first run.
func Register(ctx context.Context) (*Account, error) {
	priv, pub, err := generateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("generate keypair: %w", err)
	}

	body, _ := json.Marshal(map[string]any{
		"key":        pub,
		"install_id": "",
		"fcm_token":  "",
		"tos":        time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		"type":       "Android",
		"locale":     "en_US",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registerURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "okhttp/3.12.1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("register request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("register: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var reg registration
	if err := json.Unmarshal(respBody, &reg); err != nil {
		return nil, fmt.Errorf("register: invalid response: %w", err)
	}
	if len(reg.Config.Peers) == 0 {
		return nil, fmt.Errorf("register: no peers in response")
	}

	// A freshly registered device starts with warp_enabled=false — that's a
	// distinct "1.1.1.1 DNS only" mode with no working WireGuard tunnel.
	// Flipping it on is what wgcf/the official client do right after register.
	if !reg.WarpEnabled {
		if err := enableWarp(ctx, reg.ID, reg.Token); err != nil {
			return nil, fmt.Errorf("register: enable warp: %w", err)
		}
	}

	reserved, err := clientIDReserved(reg.Config.ClientID)
	if err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}

	peer := reg.Config.Peers[0]
	return &Account{
		PrivateKey: priv,
		Address4:   reg.Config.Interface.Addresses.V4,
		Endpoint:   peer.Endpoint.Host,
		PeerPubKey: peer.PublicKey,
		Reserved:   reserved,
	}, nil
}

type registration struct {
	ID     string `json:"id"`
	Token  string `json:"token"`
	Config struct {
		ClientID string `json:"client_id"`
		Peers    []struct {
			PublicKey string `json:"public_key"`
			Endpoint  struct {
				Host string `json:"host"`
			} `json:"endpoint"`
		} `json:"peers"`
		Interface struct {
			Addresses struct {
				V4 string `json:"v4"`
			} `json:"addresses"`
		} `json:"interface"`
	} `json:"config"`
	WarpEnabled bool `json:"warp_enabled"`
}

func enableWarp(ctx context.Context, deviceID, token string) error {
	body, _ := json.Marshal(map[string]any{"warp_enabled": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, registerURL+"/"+deviceID, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "okhttp/3.12.1")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// clientIDReserved decodes WARP's client_id (base64, 3 raw bytes) into the
// WireGuard "reserved" field mihomo's wireguard proxy type accepts — WARP's
// peer silently drops handshakes whose first 3 header bytes don't match this
// per-account value, which is why a stock WireGuard client can't talk to it.
func clientIDReserved(clientID string) ([3]byte, error) {
	var out [3]byte
	b, err := base64.StdEncoding.DecodeString(clientID)
	if err != nil {
		return out, fmt.Errorf("invalid client_id: %w", err)
	}
	if len(b) != 3 {
		return out, fmt.Errorf("unexpected client_id length %d (want 3)", len(b))
	}
	copy(out[:], b)
	return out, nil
}

func generateKeyPair() (privB64, pubB64 string, err error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return "", "", err
	}
	// WireGuard/X25519 clamping.
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64

	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(priv[:]), base64.StdEncoding.EncodeToString(pub), nil
}
