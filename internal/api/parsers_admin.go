package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"movies-api/db/store"
	"movies-api/parser"
)

// GET /api/admin/parsers
func handleAPIAdminParsersGet(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()

	trackers := []string{"kinozal", "nnmclub", "rutor"}
	orderVal, _ := store.GetSetting(ctx, "parser_order")
	if orderVal == "" {
		orderVal = "kinozal,nnmclub,rutor"
	}

	type trackerStatus struct {
		Name        string `json:"name"`
		Enabled     bool   `json:"enabled"`
		LastParsed  string `json:"last_parsed_at"`
	}

	var statuses []trackerStatus
	for _, name := range trackers {
		enabled := true
		if v, ok := store.GetSetting(ctx, "parser_"+name+"_enabled"); ok {
			enabled = v != "0"
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

	JSON(w, http.StatusOK, map[string]any{
		"parsers": statuses,
		"order":   orderVal,
		"running": parser.IsRunning(),
	})
}

// POST /api/admin/parsers/settings
func handleAPIAdminParsersSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Order          string `json:"order"`
		KinozalEnabled *bool  `json:"kinozal_enabled"`
		NNMClubEnabled *bool  `json:"nnmclub_enabled"`
		RutorEnabled   *bool  `json:"rutor_enabled"`
		OverlapDays    *int   `json:"overlap_days"`
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
	boolSetting("parser_kinozal_enabled", body.KinozalEnabled)
	boolSetting("parser_nnmclub_enabled", body.NNMClubEnabled)
	boolSetting("parser_rutor_enabled", body.RutorEnabled)
	if body.OverlapDays != nil {
		store.SetSetting(ctx, "parser_overlap_days", strconv.Itoa(*body.OverlapDays))
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

// POST /api/admin/parsers/{name}/reset
func handleAPIAdminParserTrackerReset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	switch name {
	case "kinozal", "nnmclub", "rutor":
	default:
		Error(w, http.StatusBadRequest, "unknown tracker")
		return
	}
	store.ResetLastParsedAtFor(name)
	JSON(w, http.StatusOK, map[string]string{"status": "ok", "tracker": name})
}
