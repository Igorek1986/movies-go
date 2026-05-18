package api

import (
	"encoding/json"
	"movies-api/db/store"
	"net/http"
	"strconv"
	"strings"
)

// ─── /api/lampa-profile/* (used by profiles.js plugin) ───────────────────────

// PATCH /api/lampa-profile
func handleProfilePatch(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		DeviceID  int64          `json:"device_id"`
		ProfileID string         `json:"profile_id"`
		Name      *string        `json:"name"`
		Child     *bool          `json:"child"`
		Params    map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !userOwnsDevice(r, u.ID, req.DeviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := store.UpdateProfile(r.Context(), req.DeviceID, req.ProfileID, req.Name, nil, req.Child, req.Params); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// DELETE /api/lampa-profile?device_id=&profile_id=
func handleProfileDelete(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	q := r.URL.Query()
	deviceID, _ := strconv.ParseInt(q.Get("device_id"), 10, 64)
	profileID := q.Get("profile_id")
	if deviceID == 0 || profileID == "" {
		Error(w, http.StatusBadRequest, "device_id and profile_id required")
		return
	}
	if !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := store.DeleteProfile(r.Context(), deviceID, profileID); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// POST /api/lampa-profile/clear?device_id=&profile_id=
func handleProfileClear(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	q := r.URL.Query()
	deviceID, _ := strconv.ParseInt(q.Get("device_id"), 10, 64)
	profileID := q.Get("profile_id")
	if deviceID == 0 {
		Error(w, http.StatusBadRequest, "device_id required")
		return
	}
	if !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := store.ClearProfileTimecodes(r.Context(), deviceID, profileID); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// GET /api/lampa-profile/quota?device_id=
func handleProfileQuota(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	deviceID, _ := strconv.ParseInt(r.URL.Query().Get("device_id"), 10, 64)
	if deviceID == 0 || !userOwnsDevice(r, u.ID, deviceID) {
		JSON(w, http.StatusOK, map[string]any{"count": 0, "limit": 0})
		return
	}
	count := store.CountProfiles(r.Context(), deviceID)
	lim := store.LimitsFor(u.Role).MaxProfiles
	var limitVal any = lim
	if lim == 0 {
		limitVal = nil
	}
	JSON(w, http.StatusOK, map[string]any{"count": count, "limit": limitVal})
}

// POST /api/lampa-profile/create
func handleProfileCreate(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		DeviceID  int64  `json:"device_id"`
		Name      string `json:"name"`
		ProfileID string `json:"profile_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !userOwnsDevice(r, u.ID, req.DeviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		Error(w, http.StatusBadRequest, "name required")
		return
	}
	maxProfiles := store.LimitsFor(u.Role).MaxProfiles
	if maxProfiles > 0 && store.CountProfiles(r.Context(), req.DeviceID) >= maxProfiles {
		Error(w, http.StatusForbidden, "profile limit reached")
		return
	}
	profileID := strings.TrimSpace(req.ProfileID)
	if profileID == "" {
		profileID = randHex(4)
	}
	lp, err := store.CreateProfile(r.Context(), req.DeviceID, profileID, req.Name, "")
	if err != nil {
		if strings.Contains(err.Error(), "uq_lampa_profile") {
			Error(w, http.StatusConflict, "profile id already exists")
		} else {
			Error(w, http.StatusInternalServerError, "db error")
		}
		return
	}
	JSON(w, http.StatusOK, map[string]any{
		"ok": true, "profile_id": lp.ProfileID, "name": lp.Name,
	})
}

