package api

import (
	"encoding/json"
	"fmt"
	"movies-api/db/store"
	"net/http"
	"time"
)

// GET /api/export
func handleExport(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	data, err := store.ExportUserData(r.Context(), u.ID)
	if err != nil {
		Error(w, http.StatusInternalServerError, "export error")
		return
	}

	filename := fmt.Sprintf("lampa-backup-%s.json", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	json.NewEncoder(w).Encode(data) //nolint:errcheck
}

// POST /api/import
func handleImport(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var data store.ExportData
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if data.Version != 1 {
		Error(w, http.StatusBadRequest, "unsupported version")
		return
	}

	if err := store.ImportUserData(r.Context(), u.ID, &data); err != nil {
		Error(w, http.StatusInternalServerError, "import error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
