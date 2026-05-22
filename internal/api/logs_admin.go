package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"movies-api/internal/logbuf"
)

var reDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// GET /api/admin/logs — SSE stream: history (today's buffer) + live.
func handleAPIAdminLogsStream(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	for _, line := range logbuf.Default.History() {
		data, _ := json.Marshal(line)
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	f.Flush()

	id, ch := logbuf.Default.Subscribe()
	defer logbuf.Default.Unsubscribe(id)

	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case line := <-ch:
			data, _ := json.Marshal(line)
			fmt.Fprintf(w, "data: %s\n\n", data)
			f.Flush()
		case <-tick.C:
			fmt.Fprintf(w, ": ping\n\n")
			f.Flush()
		}
	}
}

// GET /api/admin/logs/day?date=YYYY-MM-DD — full day from file.
func handleAPIAdminLogsDay(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if !reDate.MatchString(date) {
		Error(w, http.StatusBadRequest, "invalid date, expected YYYY-MM-DD")
		return
	}

	dir := logbuf.Default.LogDir()
	if dir == "" {
		JSON(w, http.StatusOK, map[string]any{"lines": []any{}})
		return
	}

	lines, err := logbuf.ReadDay(dir, date)
	if err != nil {
		Error(w, http.StatusInternalServerError, "read error")
		return
	}
	if lines == nil {
		lines = []logbuf.Line{}
	}
	JSON(w, http.StatusOK, map[string]any{"lines": lines})
}
