package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"movies-api/db/store"
	"net/http"
	"strings"
)

// adminAPIKeySetting — ключ в app_settings, где хранится админский API-ключ.
const adminAPIKeySetting = "admin_api_key"

// apiKeyFromRequest достаёт API-ключ из заголовка X-API-Key или
// Authorization: Bearer <key>.
func apiKeyFromRequest(r *http.Request) string {
	if k := r.Header.Get("X-API-Key"); k != "" {
		return k
	}
	if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(a, "Bearer "))
	}
	return ""
}

// adminAPIKeyValid возвращает true, если в запросе передан корректный
// админский API-ключ. Сравнение постоянное по времени.
func adminAPIKeyValid(r *http.Request) bool {
	provided := apiKeyFromRequest(r)
	if provided == "" {
		return false
	}
	stored, ok := store.GetSetting(r.Context(), adminAPIKeySetting)
	if !ok || stored == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(stored)) == 1
}

func generateAPIKey() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return "mvg_" + hex.EncodeToString(b)
}

// GET /api/admin/api-key — текущий ключ (или пустая строка, если не задан).
func handleAPIAdminGetAPIKey(w http.ResponseWriter, r *http.Request) {
	key, _ := store.GetSetting(r.Context(), adminAPIKeySetting)
	JSON(w, http.StatusOK, map[string]string{"api_key": key})
}

// POST /api/admin/api-key — сгенерировать новый ключ (старый перестаёт работать).
func handleAPIAdminRotateAPIKey(w http.ResponseWriter, r *http.Request) {
	key := generateAPIKey()
	store.SetSetting(r.Context(), adminAPIKeySetting, key)
	JSON(w, http.StatusOK, map[string]string{"api_key": key})
}

// DELETE /api/admin/api-key — отозвать ключ (очистить).
func handleAPIAdminDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	store.SetSetting(r.Context(), adminAPIKeySetting, "")
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
