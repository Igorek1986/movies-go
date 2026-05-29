package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"movies-api/db/store"
	"movies-api/parser"
)

// GET /api/admin/parsers
func handleAPIAdminParsersGet(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	trackers := []string{"rutor", "kinozal", "nnmclub"}
	orderVal, _ := store.GetSetting(ctx, "parser_order")
	if orderVal == "" {
		orderVal = "rutor,kinozal,nnmclub"
	}

	defaultEnabled := map[string]bool{
		"rutor":   true,
		"kinozal": false,
		"nnmclub": false,
	}

	type trackerStatus struct {
		Name       string `json:"name"`
		Enabled    bool   `json:"enabled"`
		LastParsed string `json:"last_parsed_at"`
	}

	var statuses []trackerStatus
	for _, name := range trackers {
		enabled := defaultEnabled[name]
		if v, ok := store.GetSetting(ctx, "parser_"+name+"_enabled"); ok {
			enabled = v == "1"
		}
		last := store.LastParsedAtFor(name)
		lastStr := ""
		if !last.IsZero() {
			lastStr = last.Format("2006-01-02T15:04:05Z")
		}
		statuses = append(statuses, trackerStatus{
			Name:       name,
			Enabled:    enabled,
			LastParsed: lastStr,
		})
	}

	retryAttempts := store.GetSettingInt(ctx, "parser_retry_attempts")
	retryBaseWait := store.GetSettingInt(ctx, "parser_retry_base_wait_sec")
	retryMaxWait := store.GetSettingInt(ctx, "parser_retry_max_wait_sec")
	retryRatio := "2.0"
	if v, ok := store.GetSetting(ctx, "parser_retry_ratio"); ok {
		retryRatio = v
	}

	tmdbRetryAttempts := store.GetSettingInt(ctx, "tmdb_retry_attempts")
	tmdbRetryWait := store.GetSettingInt(ctx, "tmdb_retry_wait_sec")

	kinozalLogin, _ := store.GetSetting(ctx, "kinozal_login")
	kinozalPassword, _ := store.GetSetting(ctx, "kinozal_password")
	catalogTrackers, _ := store.GetSetting(ctx, "catalog_trackers")
	if catalogTrackers == "" {
		catalogTrackers = "rutor"
	}

	nextRunAt := ""
	if t := parser.NextRunAt(); !t.IsZero() {
		nextRunAt = t.UTC().Format("2006-01-02T15:04:05Z")
	}

	rutorHost, _ := store.GetSetting(ctx, "rutor_host")
	kinozalHost, _ := store.GetSetting(ctx, "kinozal_host")
	nnmclubHost, _ := store.GetSetting(ctx, "nnmclub_host")

	JSON(w, http.StatusOK, map[string]any{
		"parsers":         statuses,
		"order":           orderVal,
		"running":         parser.IsRunning(),
		"stop_requested":  parser.IsStopRequested(),
		"current_tracker": parser.CurrentTracker(),
		"next_run_at":     nextRunAt,
		"retry_attempts":  retryAttempts,
		"retry_base_wait": retryBaseWait,
		"retry_max_wait":  retryMaxWait,
		"retry_ratio":     retryRatio,
		"tmdb_retry_attempts": tmdbRetryAttempts,
		"tmdb_retry_wait":     tmdbRetryWait,
		"kinozal_login":    kinozalLogin,
		"kinozal_password": kinozalPassword,
		"catalog_trackers": catalogTrackers,
		"tracker_cards":    store.CountCardsByTracker(),
		"rutor_host":    rutorHost,
		"kinozal_host":  kinozalHost,
		"nnmclub_host":  nnmclubHost,
	})
}

// POST /api/admin/parsers/settings
func handleAPIAdminParsersSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Order           string  `json:"order"`
		KinozalEnabled  *bool   `json:"kinozal_enabled"`
		NNMClubEnabled  *bool   `json:"nnmclub_enabled"`
		RutorEnabled    *bool   `json:"rutor_enabled"`
		OverlapDays     *int    `json:"overlap_days"`
		RetryAttempts   *int    `json:"retry_attempts"`
		RetryBaseWait   *int    `json:"retry_base_wait"`
		RetryMaxWait    *int    `json:"retry_max_wait"`
		RetryRatio      *string `json:"retry_ratio"`
		TMDBRetryAttempts *int `json:"tmdb_retry_attempts"`
		TMDBRetryWait     *int `json:"tmdb_retry_wait"`
		KinozalLogin    *string `json:"kinozal_login"`
		KinozalPassword *string `json:"kinozal_password"`
		CatalogTrackers *string `json:"catalog_trackers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}

	ctx := context.Background()

	if body.Order != "" {
		store.SetSetting(ctx, "parser_order", body.Order)
	}
	boolSetting := func(key string, v *bool) {
		if v == nil {
			return
		}
		val := "1"
		if !*v {
			val = "0"
		}
		store.SetSetting(ctx, key, val)
	}
	intSetting := func(key string, v *int) {
		if v != nil {
			store.SetSetting(ctx, key, strconv.Itoa(*v))
		}
	}
	boolSetting("parser_kinozal_enabled", body.KinozalEnabled)
	boolSetting("parser_nnmclub_enabled", body.NNMClubEnabled)
	boolSetting("parser_rutor_enabled", body.RutorEnabled)
	intSetting("parser_overlap_days", body.OverlapDays)
	intSetting("parser_retry_attempts", body.RetryAttempts)
	intSetting("parser_retry_base_wait_sec", body.RetryBaseWait)
	intSetting("parser_retry_max_wait_sec", body.RetryMaxWait)
	intSetting("tmdb_retry_attempts", body.TMDBRetryAttempts)
	intSetting("tmdb_retry_wait_sec", body.TMDBRetryWait)
	if body.RetryRatio != nil {
		store.SetSetting(ctx, "parser_retry_ratio", *body.RetryRatio)
	}
	if body.KinozalLogin != nil {
		store.SetSetting(ctx, "kinozal_login", *body.KinozalLogin)
	}
	if body.KinozalPassword != nil {
		store.SetSetting(ctx, "kinozal_password", *body.KinozalPassword)
	}
	if body.CatalogTrackers != nil {
		store.SetSetting(ctx, "catalog_trackers", *body.CatalogTrackers)
		InvalidateCategoryCache()
	}

	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /api/admin/parsers/run
func handleAPIAdminParsersRun(w http.ResponseWriter, r *http.Request) {
	if parser.IsRunning() {
		JSON(w, http.StatusOK, map[string]string{"status": "already_running"})
		return
	}
	go parser.RunAll()
	JSON(w, http.StatusOK, map[string]string{"status": "started"})
}

// POST /api/admin/parsers/stop
func handleAPIAdminParsersStop(w http.ResponseWriter, r *http.Request) {
	if !parser.IsRunning() {
		JSON(w, http.StatusOK, map[string]string{"status": "not_running"})
		return
	}
	parser.RequestStop()
	JSON(w, http.StatusOK, map[string]string{"status": "stop_requested"})
}

// POST /api/admin/parsers/{name}/run
func handleAPIAdminParserTrackerRun(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	switch name {
	case "kinozal", "nnmclub", "rutor":
	default:
		Error(w, http.StatusBadRequest, "unknown tracker")
		return
	}
	if !parser.StartOne(name) {
		JSON(w, http.StatusOK, map[string]string{"status": "already_running"})
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "started"})
}

// POST /api/admin/parsers/{name}/reset
// Body (optional): {"date":"2024-01-15"} — set to date; omit to reset (full scan).
func handleAPIAdminParserTrackerReset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	switch name {
	case "kinozal", "nnmclub", "rutor":
	default:
		Error(w, http.StatusBadRequest, "unknown tracker")
		return
	}
	var body struct {
		Date string `json:"date"`
	}
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
	if body.Date != "" {
		t, err := time.Parse("2006-01-02", body.Date)
		if err != nil || t.IsZero() {
			Error(w, http.StatusBadRequest, "неверная дата")
			return
		}
		store.SetLastParsedAtTimeFor(name, t)
		JSON(w, http.StatusOK, map[string]any{"status": "ok", "tracker": name, "date": t.Format("2006-01-02")})
		return
	}
	store.ResetLastParsedAtFor(name)
	JSON(w, http.StatusOK, map[string]string{"status": "ok", "tracker": name})
}
