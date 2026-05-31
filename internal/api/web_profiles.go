package api

// Web (session-auth) wrappers for profiles — used by the web UI.

import (
	"encoding/json"
	"movies-api/db/store"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// GET /api/devices/{id}/profiles
func handleWebListProfiles(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	deviceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	profiles := store.ListProfiles(r.Context(), deviceID)
	if profiles == nil {
		profiles = []store.ProfileInfo{}
	}
	lim := store.LimitsFor(u.Role).MaxProfiles
	JSON(w, http.StatusOK, map[string]any{"profiles": profiles, "limit": lim})
}

// POST /api/devices/{id}/profiles
func handleWebCreateProfile(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	deviceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
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
	maxProfiles := store.LimitsFor(u.Role).MaxProfiles
	if maxProfiles > 0 && store.CountProfiles(r.Context(), deviceID) >= maxProfiles {
		Error(w, http.StatusForbidden, "profile limit reached")
		return
	}
	profileID := strings.TrimSpace(req.ProfileID)
	if profileID == "" {
		profileID = randHex(4)
	}
	isFirst := store.CountProfiles(r.Context(), deviceID) == 0
	lp, err := store.CreateProfile(r.Context(), deviceID, profileID, req.Name, req.Icon)
	if err != nil {
		if strings.Contains(err.Error(), "uq_profile") {
			Error(w, http.StatusConflict, "profile id already exists")
		} else {
			Error(w, http.StatusInternalServerError, "db error")
		}
		return
	}
	if isFirst {
		store.MigrateDefaultTimecodes(r.Context(), deviceID, lp.ProfileID) //nolint:errcheck
	}
	JSON(w, http.StatusOK, map[string]any{
		"ok": true, "profile_id": lp.ProfileID, "name": lp.Name,
	})
}

// DELETE /api/devices/{id}/profiles/{profile_id}
func handleWebDeleteProfile(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	deviceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := store.DeleteProfile(r.Context(), deviceID, chi.URLParam(r, "profile_id")); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// PATCH /api/devices/{id}/profiles/{profile_id}
func handleWebUpdateProfile(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	deviceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	profileID := chi.URLParam(r, "profile_id")
	var req struct {
		Name           *string        `json:"name"`
		Icon           *string        `json:"icon"`
		Child          *bool          `json:"child"`
		ChildBirthYear *int           `json:"child_birth_year"`
		Params         map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := store.UpdateProfile(r.Context(), deviceID, profileID, req.Name, req.Icon, req.Child, req.ChildBirthYear, req.Params); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]any{"ok": true, "profile_id": profileID})
}

// DELETE /api/devices/{id}/profiles/{profile_id}/timecodes
func handleWebClearProfileTimecodes(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	deviceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := store.ClearProfileTimecodes(r.Context(), deviceID, chi.URLParam(r, "profile_id")); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// POST /api/devices/{id}/migrate-default-timecodes
func handleWebMigrateDefaultTimecodes(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	deviceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	var req struct {
		ProfileID string `json:"profile_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProfileID == "" {
		Error(w, http.StatusBadRequest, "profile_id required")
		return
	}
	if err := store.MigrateDefaultTimecodes(r.Context(), deviceID, req.ProfileID); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// DELETE /api/devices/{id}/default-timecodes
func handleWebDeleteDefaultTimecodes(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	deviceID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || !userOwnsDevice(r, u.ID, deviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	store.ClearProfileTimecodes(r.Context(), deviceID, "") //nolint:errcheck
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func userOwnsDevice(r *http.Request, userID, deviceID int64) bool {
	d := store.GetDeviceByID(r.Context(), deviceID)
	return d != nil && d.UserID == userID
}

// POST /api/profile-name — rename a profile (used by history.js)
func handleProfileName(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	var req struct {
		DeviceID  int64  `json:"device_id"`
		ProfileID string `json:"profile_id"`
		Name      string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !userOwnsDevice(r, u.ID, req.DeviceID) {
		Error(w, http.StatusForbidden, "forbidden")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		Error(w, http.StatusBadRequest, "name required")
		return
	}
	if err := store.UpdateProfile(r.Context(), req.DeviceID, req.ProfileID, &name, nil, nil, nil, nil); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
