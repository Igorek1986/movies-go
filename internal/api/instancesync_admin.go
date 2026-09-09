package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"movies-api/db/store"
)

// syncItemDef describes one togglable push/pull item shown on /admin/sync —
// see dev/instance-sync.md. Label/description live here (backend), the
// frontend just renders whatever this endpoint returns.
type syncItemDef struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

var syncPushDefs = []struct{ Key, Label, Description string }{
	{"sync_push_enabled", "Play-события",
		"card_id + uid + процент просмотра + реальная длительность от плеера (если есть). Анонимно, без токена. Используется для «Популярного» на принимающей стороне и для самокоррекции runtime там же."},
}

var syncPullDefs = []struct{ Key, Label, Description string }{
	{"sync_pull_runtime", "Runtime / episode_run_time",
		"Если у карточки нет длительности локально — спросить у шлюза, перед остальными внешними источниками (fix_runtime.go). Проверяется по расписанию ежедневной задачи + вручную кнопкой «Обновить runtime»."},
	{"sync_pull_ids", "imdb_id",
		"Если у карточки нет imdb_id локально (у сериалов сейчас не заполняется вообще никак иначе) — подтянуть с шлюза. Разблокирует imdb-based внешние источники (poiskkino.dev и т.п.), которым иначе не с чем сопоставлять."},
	{"sync_pull_quality", "Качество / дата поступления",
		"best_video_quality и latest_torrent_date — не гарантия именно этого инстанса (реальный поиск потока для просмотра — отдельный внешний механизм), а сигнал витрины: «где-то в сети уже находили». Мержится как максимум (GREATEST), не перезаписывает лучшее локальное значение."},
	{"sync_pull_popular", "«Популярное»",
		"Категория «Популярное» берётся с шлюза вместо отдельной настройки popular_source_url (та остаётся как ручной фолбэк, если этот пункт выключен)."},
}

// GET /api/admin/sync
func handleAPIAdminSyncGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	role, _ := store.GetSetting(ctx, "sync_role")
	gatewayURL, _ := store.GetSetting(ctx, "sync_gateway_url")
	interval := store.GetSettingInt(ctx, "sync_interval_minutes")
	if interval < 1 {
		interval = 15
	}

	push := make([]syncItemDef, 0, len(syncPushDefs))
	for _, d := range syncPushDefs {
		v, _ := store.GetSetting(ctx, d.Key)
		push = append(push, syncItemDef{Key: d.Key, Label: d.Label, Description: d.Description, Enabled: v == "1"})
	}
	pull := make([]syncItemDef, 0, len(syncPullDefs))
	for _, d := range syncPullDefs {
		v, _ := store.GetSetting(ctx, d.Key)
		pull = append(pull, syncItemDef{Key: d.Key, Label: d.Label, Description: d.Description, Enabled: v == "1"})
	}

	JSON(w, http.StatusOK, map[string]any{
		"role":             role,
		"gateway_url":      gatewayURL,
		"interval_minutes": interval,
		"push":             push,
		"pull":             pull,
	})
}

var validSyncItemKeys = func() map[string]bool {
	m := map[string]bool{}
	for _, d := range syncPushDefs {
		m[d.Key] = true
	}
	for _, d := range syncPullDefs {
		m[d.Key] = true
	}
	return m
}()

// POST /api/admin/sync
// Body: {"role": "off"|"gateway"|"client", "gateway_url": "...", "interval_minutes": 15,
//
//	"items": {"sync_pull_runtime": true, ...}} — all fields optional, only given ones change.
func handleAPIAdminSyncSave(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Role            *string         `json:"role"`
		GatewayURL      *string         `json:"gateway_url"`
		IntervalMinutes *int            `json:"interval_minutes"`
		Items           map[string]bool `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}

	ctx := context.Background()
	if body.Role != nil {
		switch *body.Role {
		case "off", "gateway", "client":
			store.SetSetting(ctx, "sync_role", *body.Role)
		default:
			Error(w, http.StatusBadRequest, "role must be off/gateway/client")
			return
		}
	}
	if body.GatewayURL != nil {
		store.SetSetting(ctx, "sync_gateway_url", *body.GatewayURL)
	}
	if body.IntervalMinutes != nil && *body.IntervalMinutes > 0 {
		store.SetSetting(ctx, "sync_interval_minutes", strconv.Itoa(*body.IntervalMinutes))
	}
	for key, val := range body.Items {
		if !validSyncItemKeys[key] {
			continue
		}
		v := "0"
		if val {
			v = "1"
		}
		store.SetSetting(ctx, key, v)
	}

	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
