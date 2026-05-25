package api

import (
	"context"
	"encoding/json"
	"movies-api/db/models"
	"movies-api/db/store"
	"movies-api/internal/proxy"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// ─── List routes (for UI labels) ─────────────────────────────────────────────

// proxyRouteOrder defines display order; labels are looked up from this slice.
var proxyRouteOrder = []struct{ key, label string }{
	{proxy.RouteTelegram,      "Telegram бот"},
	{proxy.RouteTMDB,          "TMDB API"},
	{proxy.RouteImages,        "Картинки TMDB"},
	{proxy.RouteParserKinozal, "Парсер Kinozal"},
	{proxy.RouteParserNNMClub, "Парсер NNMClub"},
	{proxy.RouteParserRutor,   "Парсер Rutor"},
}

// proxyRouteLabels is used for quick key → label lookup and save validation.
var proxyRouteLabels = func() map[string]string {
	m := make(map[string]string, len(proxyRouteOrder))
	for _, r := range proxyRouteOrder {
		m[r.key] = r.label
	}
	return m
}()

// ─── Proxy configs CRUD ───────────────────────────────────────────────────────

func handleAPIProxiesList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	configs, err := store.ListProxyConfigs(ctx)
	if err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	routes, err := store.GetProxyRouting(ctx)
	if err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}

	type routeOut struct {
		models.ProxyRoute
		Label string `json:"label"`
	}
	routeMap := make(map[string]models.ProxyRoute)
	for _, rt := range routes {
		routeMap[rt.Route] = rt
	}
	var routesOut []routeOut
	for _, r := range proxyRouteOrder {
		rt := routeMap[r.key]
		rt.Route = r.key
		if rt.ProxyIDs == nil {
			rt.ProxyIDs = []int{}
		}
		routesOut = append(routesOut, routeOut{ProxyRoute: rt, Label: r.label})
	}

	JSON(w, http.StatusOK, map[string]any{
		"configs": configs,
		"routes":  routesOut,
	})
}

func handleAPIProxiesCreate(w http.ResponseWriter, r *http.Request) {
	var body models.ProxyConfig
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Type != "socks5" {
		Error(w, http.StatusBadRequest, "type must be socks5")
		return
	}
	if body.Name == "" || body.Config == "" {
		Error(w, http.StatusBadRequest, "name and config are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	created, err := store.CreateProxyConfig(ctx, body)
	if err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	proxy.Default.Invalidate()
	JSON(w, http.StatusCreated, created)
}

func handleAPIProxiesUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body models.ProxyConfig
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Type != "socks5" {
		Error(w, http.StatusBadRequest, "type must be socks5")
		return
	}
	body.ID = id

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := store.UpdateProxyConfig(ctx, body); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	proxy.Default.Invalidate()
	w.WriteHeader(http.StatusNoContent)
}

func handleAPIProxiesDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := store.DeleteProxyConfig(ctx, id); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	proxy.Default.Invalidate()
	w.WriteHeader(http.StatusNoContent)
}

// ─── Routing ──────────────────────────────────────────────────────────────────

func handleAPIProxyRoutingSave(w http.ResponseWriter, r *http.Request) {
	type routeInput struct {
		Route    string `json:"route"`
		Enabled  bool   `json:"enabled"`
		ProxyIDs []int  `json:"proxy_ids"`
	}
	var body []routeInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	for _, rt := range body {
		if _, ok := proxyRouteLabels[rt.Route]; !ok {
			continue
		}
		ids := rt.ProxyIDs
		if ids == nil {
			ids = []int{}
		}
		if err := store.SetProxyRoute(ctx, rt.Route, rt.Enabled, ids); err != nil {
			Error(w, http.StatusInternalServerError, "db error")
			return
		}
	}
	proxy.Default.Invalidate()
	w.WriteHeader(http.StatusNoContent)
}

// ─── Test ─────────────────────────────────────────────────────────────────────

func handleAPIProxiesTest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	cfg, err := store.GetProxyConfig(ctx, id)
	if err != nil {
		Error(w, http.StatusNotFound, "not found")
		return
	}

	client := buildTestClient(cfg)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.themoviedb.org/3/configuration", nil)
	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		JSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "ms": elapsed.Milliseconds()})
		return
	}
	resp.Body.Close()
	JSON(w, http.StatusOK, map[string]any{"ok": resp.StatusCode < 500, "status": resp.StatusCode, "ms": elapsed.Milliseconds()})
}

func buildTestClient(cfg *models.ProxyConfig) *http.Client {
	from := []models.ProxyConfig{*cfg}
	// reuse manager's buildClient via the exported package
	return proxy.BuildClient(from)
}
