package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"movies-api/db/postgres"
	"movies-api/db/store"
	"movies-api/movies/tmdb"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

var cardIDRe = regexp.MustCompile(`^(\d+)_(movie|tv)$`)

// ─── POST /timecode ───────────────────────────────────────────────────────────

func handleSaveTimecode(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}

	var body struct {
		CardID string `json:"card_id"`
		Item   string `json:"item"`
		Data   string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.CardID == "" || body.Item == "" {
		Error(w, http.StatusBadRequest, "card_id, item, data required")
		return
	}
	if !json.Valid([]byte(body.Data)) {
		Error(w, http.StatusBadRequest, "data must be valid JSON")
		return
	}

	profileID := r.URL.Query().Get("profile_id")
	profileName := r.URL.Query().Get("profile_name")

	// Extract player-reported duration before storing.
	var playerDur struct{ Duration float64 `json:"duration"` }
	json.Unmarshal([]byte(body.Data), &playerDur) //nolint:errcheck

	store.UpsertTimecodes(r.Context(), d.ID, profileID, []store.TimecodeRow{
		{CardID: body.CardID, Item: body.Item, Data: body.Data},
	})
	store.TrimToLimit(r.Context(), d.ID, profileID, deviceUserRole(r, d))
	store.UpsertProfileName(r.Context(), d.ID, profileID, profileName)

	// Update runtime/episode_run_time from player-reported duration when reliable.
	if m := cardIDRe.FindStringSubmatch(body.CardID); m != nil && playerDur.Duration > 60 {
		go store.MaybeUpdateRuntimeFromPlayer(body.CardID, m[2], playerDur.Duration)
	}

	// Фоновое обновление метаданных карточки из TMDB, не чаще раза в сутки.
	if m := cardIDRe.FindStringSubmatch(body.CardID); m != nil {
		go refreshCardFromTMDB(body.CardID)
	}

	// Broadcast to other devices of the same user.
	go broadcastTimecode(d.UserID, d.ID, profileID, body.CardID, body.Item, body.Data)

	JSON(w, http.StatusOK, map[string]bool{"success": true})
}

// refreshCardFromTMDB обновляет media_card из TMDB API если прошли сутки с последнего обновления.
func refreshCardFromTMDB(cardID string) {
	m := cardIDRe.FindStringSubmatch(cardID)
	if m == nil {
		return
	}
	tmdbID, _ := strconv.ParseInt(m[1], 10, 64)
	mediaType := m[2]

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Проверяем cooldown: обновляем не чаще раза в сутки.
	var updatedAt *time.Time
	postgres.Pool.QueryRow(ctx, //nolint:errcheck
		`SELECT tmdb_updated_at FROM media_cards WHERE card_id = $1`, cardID,
	).Scan(&updatedAt)
	if updatedAt != nil && time.Since(*updatedAt) < 24*time.Hour {
		return
	}

	isMovie := mediaType == "movie"
	ent := tmdb.GetVideoDetails(isMovie, tmdbID)
	if ent == nil {
		return
	}
	// Сохраняем только TMDB-поля (без перезаписи torrent-специфичных данных).
	store.RefreshCardTMDB(ctx, cardID, ent)
}

// ─── POST /timecode/batch ─────────────────────────────────────────────────────

func handleBatchTimecodes(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}

	var rows []store.TimecodeRow
	if err := json.NewDecoder(r.Body).Decode(&rows); err != nil {
		Error(w, http.StatusBadRequest, "expected [{card_id,item,data}]")
		return
	}

	profileID := r.URL.Query().Get("profile_id")
	saved := store.UpsertTimecodes(r.Context(), d.ID, profileID, rows)
	store.TrimToLimit(r.Context(), d.ID, profileID, deviceUserRole(r, d))

	JSON(w, http.StatusOK, map[string]any{"success": true, "saved": saved})
}

// ─── GET /timecode/export ─────────────────────────────────────────────────────

func handleExportTimecodes(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}
	result := store.ExportTimecodes(r.Context(), d.ID, r.URL.Query().Get("profile_id"))
	if result == nil {
		result = map[string]map[string]string{}
	}
	JSON(w, http.StatusOK, result)
}

// ─── POST /timecode/import/lampac ─────────────────────────────────────────────

func handleImportLampac(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}

	var data map[string]map[string]string
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		Error(w, http.StatusBadRequest, "expected {card_id:{item:data}}")
		return
	}

	profileID := r.URL.Query().Get("profile_id")
	var rows []store.TimecodeRow
	for cardID, items := range data {
		for item, dataStr := range items {
			if json.Valid([]byte(dataStr)) {
				rows = append(rows, store.TimecodeRow{CardID: cardID, Item: item, Data: dataStr})
			}
		}
	}
	saved := store.UpsertTimecodes(r.Context(), d.ID, profileID, rows)
	JSON(w, http.StatusOK, map[string]any{"success": true, "imported": saved})
}

// ─── DELETE /timecode ─────────────────────────────────────────────────────────

func handleDeleteTimecode(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}
	cardID := r.URL.Query().Get("card_id")
	item := r.URL.Query().Get("item")
	if cardID == "" || item == "" {
		Error(w, http.StatusBadRequest, "card_id and item required")
		return
	}
	store.DeleteTimecode(r.Context(), d.ID, r.URL.Query().Get("profile_id"), cardID, item)
	JSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ─── GET /timecode/history ────────────────────────────────────────────────────

func handleHistory(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	entries, _, total := store.GetHistoryFiltered(r.Context(), store.HistoryFilter{
		DeviceID:  d.ID,
		ProfileID: r.URL.Query().Get("profile_id"),
		Page:      page,
		PerPage:   limit,
	})
	if entries == nil {
		entries = []store.HistoryEntry{}
	}
	totalPages := (total + limit - 1) / limit
	if totalPages < 1 {
		totalPages = 1
	}
	JSON(w, http.StatusOK, map[string]any{"results": entries, "total_pages": totalPages})
}

// ─── GET /timecode/profiles ───────────────────────────────────────────────────

func handleListProfiles(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}
	profiles := store.ListProfiles(r.Context(), d.ID)
	if profiles == nil {
		profiles = []store.ProfileInfo{}
	}
	lim := store.LimitsFor(deviceUserRole(r, d)).MaxProfiles
	JSON(w, http.StatusOK, map[string]any{"profiles": profiles, "limit": lim})
}

// ─── POST /timecode/profiles ──────────────────────────────────────────────────

func handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}

	var req struct {
		Name      string `json:"name"`
		ProfileID string `json:"profile_id"`
		Icon      string `json:"icon"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		Error(w, http.StatusBadRequest, "name required")
		return
	}

	maxProfiles := store.LimitsFor(deviceUserRole(r, d)).MaxProfiles
	if maxProfiles > 0 && store.CountProfiles(r.Context(), d.ID) >= maxProfiles {
		Error(w, http.StatusForbidden, "profile limit reached")
		return
	}

	profileID := strings.TrimSpace(req.ProfileID)
	if profileID == "" {
		profileID = randHex(4)
	}

	lp, err := store.CreateProfile(r.Context(), d.ID, profileID, req.Name, req.Icon)
	if err != nil {
		if strings.Contains(err.Error(), "uq_profile") {
			Error(w, http.StatusConflict, "profile id already exists")
		} else {
			Error(w, http.StatusInternalServerError, "db error")
		}
		return
	}
	JSON(w, http.StatusOK, map[string]any{
		"ok": true, "profile_id": lp.ProfileID, "name": lp.Name, "icon": lp.Icon,
	})
}

// ─── PATCH /timecode/profiles/{profile_id} ────────────────────────────────────

func handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}
	profileID := chi.URLParam(r, "profile_id")

	var req struct {
		Name   *string        `json:"name"`
		Icon   *string        `json:"icon"`
		Child  *bool          `json:"child"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := store.UpdateProfile(r.Context(), d.ID, profileID, req.Name, req.Icon, req.Child, nil, req.Params); err != nil {
		Error(w, http.StatusInternalServerError, "update error")
		return
	}
	go broadcastProfileUpdated(d.UserID, d.ID, profileID, req.Name, req.Icon)
	JSON(w, http.StatusOK, map[string]any{"ok": true, "profile_id": profileID})
}

// ─── DELETE /timecode/profiles/{profile_id} ───────────────────────────────────

func handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}
	profileID := chi.URLParam(r, "profile_id")
	if err := store.DeleteProfile(r.Context(), d.ID, profileID); err != nil {
		Error(w, http.StatusInternalServerError, "delete error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ─── GET /timecode/favorite ───────────────────────────────────────────────────

func handleGetFavorite(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}
	fav := store.GetFavorite(r.Context(), d.ID, r.URL.Query().Get("profile_id"))
	JSON(w, http.StatusOK, map[string]any{"favorite": fav})
}

// ─── PUT /timecode/favorite ───────────────────────────────────────────────────

func handlePutFavorite(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		Error(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}

	var body struct {
		Favorite any `json:"favorite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	profileID := r.URL.Query().Get("profile_id")
	if err := store.SaveFavorite(r.Context(), d.ID, profileID, body.Favorite); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	go broadcastFavorite(d.UserID, d.ID, profileID, body.Favorite)
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ─── WS broadcast helpers ─────────────────────────────────────────────────────

func broadcastTimecode(userID, deviceID int64, profileID, cardID, item, data string) {
	msg, _ := json.Marshal(map[string]any{
		"type":       "timecode",
		"profile_id": profileID,
		"card_id":    cardID,
		"item":       item,
		"data":       json.RawMessage(data),
	})
	TimecodeHub.Broadcast(userID, deviceID, msg)
}

func broadcastFavorite(userID, deviceID int64, profileID string, favorite any) {
	msg, _ := json.Marshal(map[string]any{
		"type":       "favorite",
		"profile_id": profileID,
		"favorite":   favorite,
	})
	TimecodeHub.Broadcast(userID, deviceID, msg)
}

func broadcastProfileUpdated(userID, deviceID int64, profileID string, name *string, icon *string) {
	payload := map[string]any{
		"type":       "profile_updated",
		"profile_id": profileID,
	}
	if name != nil {
		payload["name"] = *name
	}
	if icon != nil {
		payload["icon"] = *icon
	}
	msg, _ := json.Marshal(payload)
	TimecodeHub.Broadcast(userID, deviceID, msg)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func deviceUserRole(r *http.Request, d *deviceCtx) string {
	u := store.GetUserByID(r.Context(), d.UserID)
	if u != nil {
		return u.Role
	}
	return "simple"
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}
