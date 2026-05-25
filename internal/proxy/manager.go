package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"movies-api/db/models"
	"movies-api/db/store"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	xproxy "golang.org/x/net/proxy"
)

// Route keys used by callers.
const (
	RouteImages        = "images"
	RouteTMDB          = "tmdb"
	RouteParserRutor   = "parser_rutor"
	RouteParserKinozal = "parser_kinozal"
	RouteParserNNMClub = "parser_nnmclub"
	RouteTelegram      = "telegram"
)

// Default is the package-level proxy manager singleton.
var Default = &Manager{cacheTTL: 30 * time.Second}

type Manager struct {
	mu       sync.RWMutex
	clients  map[string]*http.Client
	cachedAt time.Time
	cacheTTL time.Duration
}

// ClientFor returns an *http.Client for the given route.
// If no proxy is configured for the route, returns a plain http.Client.
func (m *Manager) ClientFor(ctx context.Context, route string) *http.Client {
	m.mu.RLock()
	fresh := m.clients != nil && time.Since(m.cachedAt) < m.cacheTTL
	if fresh {
		if c, ok := m.clients[route]; ok {
			m.mu.RUnlock()
			return c
		}
		m.mu.RUnlock()
		return defaultClient()
	}
	m.mu.RUnlock()

	m.reload(ctx)

	m.mu.RLock()
	defer m.mu.RUnlock()
	if c, ok := m.clients[route]; ok {
		return c
	}
	return defaultClient()
}

// HasProxy returns true if a proxy is configured and enabled for the given route.
func (m *Manager) HasProxy(ctx context.Context, route string) bool {
	m.mu.RLock()
	fresh := m.clients != nil && time.Since(m.cachedAt) < m.cacheTTL
	if fresh {
		_, ok := m.clients[route]
		m.mu.RUnlock()
		return ok
	}
	m.mu.RUnlock()
	m.reload(ctx)
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.clients[route]
	return ok
}

// Invalidate clears the cache so the next ClientFor call reloads from DB.
func (m *Manager) Invalidate() {
	m.mu.Lock()
	m.clients = nil
	m.mu.Unlock()
}

func (m *Manager) reload(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.clients != nil && time.Since(m.cachedAt) < m.cacheTTL {
		return
	}

	configs, err := store.ListProxyConfigs(ctx)
	if err != nil {
		log.Printf("proxy: reload configs: %v", err)
		m.clients = make(map[string]*http.Client)
		m.cachedAt = time.Now()
		return
	}

	routes, err := store.GetProxyRouting(ctx)
	if err != nil {
		log.Printf("proxy: reload routing: %v", err)
		m.clients = make(map[string]*http.Client)
		m.cachedAt = time.Now()
		return
	}

	clients := make(map[string]*http.Client)
	for _, route := range routes {
		if !route.Enabled {
			continue
		}
		proxies := selectProxies(configs, route)
		if len(proxies) == 0 {
			continue
		}
		clients[route.Route] = buildClient(proxies)
	}

	m.clients = clients
	m.cachedAt = time.Now()
}

func selectProxies(configs []models.ProxyConfig, route models.ProxyRoute) []models.ProxyConfig {
	if len(route.ProxyIDs) > 0 {
		var out []models.ProxyConfig
		for _, id := range route.ProxyIDs {
			for _, c := range configs {
				if c.ID == id && c.Enabled {
					out = append(out, c)
				}
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
		return out
	}
	var out []models.ProxyConfig
	for _, c := range configs {
		if c.Enabled {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	return out
}

// BuildClient creates an http.Client that routes through the given proxies in order,
// falling back to direct connection if all fail.
func BuildClient(proxies []models.ProxyConfig) *http.Client { return buildClient(proxies) }

func buildClient(proxies []models.ProxyConfig) *http.Client {
	dialers := make([]func(ctx context.Context, network, addr string) (net.Conn, error), 0, len(proxies))
	for _, p := range proxies {
		if p.Type == "socks5" {
			d := socks5Dialer(p.Config)
			if d != nil {
				dialers = append(dialers, d)
			}
		}
	}
	if len(dialers) == 0 {
		return defaultClient()
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var lastErr error
			for _, d := range dialers {
				conn, err := d(ctx, network, addr)
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			// Fallback to direct
			conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, addr)
			if err != nil {
				return nil, fmt.Errorf("all proxies failed (%w), direct also failed: %v", lastErr, err)
			}
			return conn, nil
		},
		MaxIdleConns:        20,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig:     &tls.Config{},
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}
}

func socks5Dialer(configURL string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	raw := strings.Replace(configURL, "socks5h://", "socks5://", 1)
	u, err := url.Parse(raw)
	if err != nil {
		log.Printf("proxy: invalid socks5 URL %q: %v", configURL, err)
		return nil
	}
	host := u.Host
	var auth *xproxy.Auth
	if u.User != nil {
		pass, _ := u.User.Password()
		auth = &xproxy.Auth{User: u.User.Username(), Password: pass}
	}
	d, err := xproxy.SOCKS5("tcp", host, auth, xproxy.Direct)
	if err != nil {
		log.Printf("proxy: socks5 dialer for %s: %v", host, err)
		return nil
	}
	dc, ok := d.(xproxy.ContextDialer)
	if !ok {
		return func(ctx context.Context, network, addr string) (net.Conn, error) {
			return d.Dial(network, addr)
		}
	}
	return dc.DialContext
}

func defaultClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}
