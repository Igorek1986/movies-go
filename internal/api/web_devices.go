package api

// Web form handlers for device management (/profiles/* routes).

import (
	"encoding/json"
	"lampa-api/db/store"
	"lampa-api/internal/render"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// GET /profiles
func handleProfilesPage(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	devices := store.GetDevicesWithStats(r.Context(), u.ID)
	lim := store.LimitsFor(u.Role)
	render.Page(w, r, "profiles", u, profilesPageData{
		Devices:          devices,
		DeviceLimit:      lim.MaxDevices,
		ProfileLimit:     lim.MaxProfiles,
		TimecodeLimit:    lim.MaxTimecodes,
		TotpEnabled:      u.TotpEnabled,
		BackupCodesCount: countBackupCodes(u.BackupCodes),
		Registered:       r.URL.Query().Get("registered") == "1",
		Success:          r.URL.Query().Get("success"),
	})
}

// POST /profiles/create
func handleProfilesCreate(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/profiles", http.StatusFound)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Redirect(w, r, "/profiles", http.StatusFound)
		return
	}
	lim := store.LimitsFor(u.Role)
	if lim.MaxDevices > 0 && store.CountUserDevices(r.Context(), u.ID) >= lim.MaxDevices {
		http.Redirect(w, r, "/profiles", http.StatusFound)
		return
	}
	dev, err := store.CreateDevice(r.Context(), u.ID, name)
	if err != nil {
		http.Redirect(w, r, "/profiles", http.StatusFound)
		return
	}
	// Show the token once
	render.Page(w, r, "profile_key_once", u, profileKeyOnceData{
		DeviceName: dev.Name,
		Token:      dev.Token,
	})
}

// POST /profiles/{id}/regenerate
func handleProfilesRegenerate(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/profiles", http.StatusFound)
		return
	}
	token, err := store.RegenerateToken(r.Context(), id, u.ID)
	if err != nil {
		http.Redirect(w, r, "/profiles", http.StatusFound)
		return
	}
	d := store.GetDeviceByID(r.Context(), id)
	name := ""
	if d != nil {
		name = d.Name
	}
	render.Page(w, r, "profile_key_once", u, profileKeyOnceData{
		DeviceName: name,
		Token:      token,
	})
}

// POST /profiles/{id}/clear-timecodes
func handleProfilesClearTimecodes(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	_ = store.ClearDeviceTimecodes(r.Context(), id, u.ID)
	http.Redirect(w, r, "/profiles", http.StatusFound)
}

// POST /profiles/{id}/delete
func handleProfilesDelete(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	_ = store.DeleteDevice(r.Context(), id, u.ID)
	http.Redirect(w, r, "/profiles", http.StatusFound)
}

// POST /profiles/{id}/rename — AJAX, returns JSON
func handleProfilesRename(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := r.ParseForm(); err != nil {
		Error(w, http.StatusBadRequest, "invalid form")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		Error(w, http.StatusBadRequest, "name required")
		return
	}
	if err := store.RenameDevice(r.Context(), id, u.ID, name); err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ─── /api/lampa-profile/* aliases (used by profiles.js) ─────────────────────

// PATCH /api/lampa-profile — update profile (child flag or params)
func handleLampaProfilePatch(w http.ResponseWriter, r *http.Request) {
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
func handleLampaProfileDelete(w http.ResponseWriter, r *http.Request) {
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
func handleLampaProfileClear(w http.ResponseWriter, r *http.Request) {
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
func handleLampaProfileQuota(w http.ResponseWriter, r *http.Request) {
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
func handleLampaProfileCreate(w http.ResponseWriter, r *http.Request) {
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
		"ok": true, "profile_id": lp.LampaProfileID, "name": lp.Name,
	})
}

// ─── Data types ───────────────────────────────────────────────────────────────

type profilesPageData struct {
	Devices          []store.DeviceWithStats
	DeviceLimit      int
	ProfileLimit     int
	TimecodeLimit    int
	TotpEnabled      bool
	BackupCodesCount int
	Registered       bool
	Error            string
	Success          string
}

type profileKeyOnceData struct {
	DeviceName string
	Token      string
}

func countBackupCodes(codes *string) int {
	if codes == nil || *codes == "" {
		return 0
	}
	var list []string
	if err := json.Unmarshal([]byte(*codes), &list); err != nil {
		return 0
	}
	return len(list)
}
