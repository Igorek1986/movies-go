package api

import (
	"context"
	"encoding/json"
	"net/http"

	"movies-api/db/store"
)

var validExternalSourceKeys = func() map[string]bool {
	m := make(map[string]bool, len(store.ExternalSourceKeys))
	for _, k := range store.ExternalSourceKeys {
		m[k] = true
	}
	return m
}()

// GET /api/admin/external-sources
func handleAPIAdminExternalSourcesGet(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, map[string]any{
		"sources": store.ListExternalSources(context.Background()),
	})
}

// POST /api/admin/external-sources/{key}
// Body: {"enabled": bool, "token": string} — оба поля опциональны, задавай
// только то, что меняешь. Пустая строка в token стирает сохранённый ключ.
func handleAPIAdminExternalSourcesSet(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !validExternalSourceKeys[key] {
		Error(w, http.StatusBadRequest, "неизвестный источник")
		return
	}

	var body struct {
		Enabled *bool   `json:"enabled"`
		Token   *string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "bad request")
		return
	}

	ctx := context.Background()
	if body.Enabled != nil {
		if err := store.SetExternalSourceEnabled(ctx, key, *body.Enabled); err != nil {
			Error(w, http.StatusInternalServerError, "ошибка сохранения")
			return
		}
	}
	if body.Token != nil {
		if err := store.SetExternalSourceToken(ctx, key, *body.Token); err != nil {
			Error(w, http.StatusInternalServerError, "ошибка сохранения")
			return
		}
	}

	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
