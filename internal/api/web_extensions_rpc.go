package api

// Bridge between sandboxed extension iframes (see
// web/src/components/extensions/) and the backend. An extension never gets
// the user's session cookie or a device token — it can only ask for one of a
// fixed set of named actions, each implemented here with its own ownership
// checks, same as the equivalent /api/devices* endpoints it stands in for.
// Grow the allowlist as new extensions need more; there's no generic
// URL/method passthrough on purpose.

import (
	"encoding/json"
	"fmt"
	"movies-api/db/models"
	"movies-api/db/store"
	"net/http"
	"strings"
)

// POST /api/extensions/rpc
func handleWebExtensionRPC(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	var req struct {
		Action string          `json:"action"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid json")
		return
	}

	switch req.Action {
	case "devices.list":
		rpcDevicesList(w, r, u)
	case "devices.create":
		rpcDevicesCreate(w, r, u, req.Params)
	case "profiles.list":
		rpcProfilesList(w, r, u, req.Params)
	case "profiles.create":
		rpcProfilesCreate(w, r, u, req.Params)
	case "timecodes.importLampac":
		rpcImportLampac(w, r, u, req.Params)
	default:
		Error(w, http.StatusBadRequest, "unknown action")
	}
}

func rpcDevicesList(w http.ResponseWriter, r *http.Request, u *models.User) {
	devices := store.GetDevicesByUser(r.Context(), u.ID)
	type deviceView struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	result := make([]deviceView, len(devices))
	for i, d := range devices {
		result[i] = deviceView{ID: d.ID, Name: d.Name}
	}
	JSON(w, http.StatusOK, map[string]any{"devices": result})
}

func rpcDevicesCreate(w http.ResponseWriter, r *http.Request, u *models.User, raw json.RawMessage) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &params); err != nil || strings.TrimSpace(params.Name) == "" {
		Error(w, http.StatusBadRequest, "name required")
		return
	}
	maxDev := deviceLimit(u.Role)
	if maxDev > 0 && store.CountUserDevices(r.Context(), u.ID) >= maxDev {
		Error(w, http.StatusForbidden, "device limit reached")
		return
	}
	dev, err := store.CreateDevice(r.Context(), u.ID, strings.TrimSpace(params.Name))
	if err != nil {
		if strings.Contains(err.Error(), "uq_devices_user_name") {
			Error(w, http.StatusConflict, "device with this name already exists")
			return
		}
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	JSON(w, http.StatusOK, map[string]any{"id": dev.ID, "name": dev.Name})
}

func rpcProfilesList(w http.ResponseWriter, r *http.Request, u *models.User, raw json.RawMessage) {
	var params struct {
		DeviceID int64 `json:"deviceId"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		Error(w, http.StatusBadRequest, "invalid params")
		return
	}
	if params.DeviceID == 0 {
		Error(w, http.StatusBadRequest, "deviceId required — выберите устройство")
		return
	}
	if !userOwnsDevice(r, u.ID, params.DeviceID) {
		Error(w, http.StatusForbidden, fmt.Sprintf("forbidden: device %d not found or not yours", params.DeviceID))
		return
	}
	profiles := store.ListProfiles(r.Context(), params.DeviceID)
	if profiles == nil {
		profiles = []store.ProfileInfo{}
	}
	JSON(w, http.StatusOK, map[string]any{"profiles": profiles})
}

func rpcProfilesCreate(w http.ResponseWriter, r *http.Request, u *models.User, raw json.RawMessage) {
	var params struct {
		DeviceID int64  `json:"deviceId"`
		Name     string `json:"name"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		Error(w, http.StatusBadRequest, "invalid params")
		return
	}
	if params.DeviceID == 0 {
		Error(w, http.StatusBadRequest, "deviceId required — выберите устройство")
		return
	}
	if !userOwnsDevice(r, u.ID, params.DeviceID) {
		Error(w, http.StatusForbidden, fmt.Sprintf("forbidden: device %d not found or not yours", params.DeviceID))
		return
	}
	params.Name = strings.TrimSpace(params.Name)
	if params.Name == "" {
		Error(w, http.StatusBadRequest, "name required")
		return
	}
	maxProfiles := store.LimitsFor(u.Role).MaxProfiles
	if maxProfiles > 0 && store.CountProfiles(r.Context(), params.DeviceID) >= maxProfiles {
		Error(w, http.StatusForbidden, "profile limit reached")
		return
	}
	profileID := randHex(4)
	p, err := store.CreateProfile(r.Context(), params.DeviceID, profileID, params.Name, "")
	if err != nil {
		Error(w, http.StatusInternalServerError, "db error")
		return
	}
	// Тот же WS-бродкаст, что и у обычного создания профиля
	// (handleWebCreateProfile) — иначе переключатель профилей не узнаёт о
	// новом профиле, пока не перезагрузить страницу.
	go broadcastProfileUpdated(u.ID, 0, "", p.ProfileID, &p.Name, nil)
	JSON(w, http.StatusOK, map[string]any{"profile_id": p.ProfileID, "name": p.Name})
}

func rpcImportLampac(w http.ResponseWriter, r *http.Request, u *models.User, raw json.RawMessage) {
	var params struct {
		DeviceID  int64                        `json:"deviceId"`
		ProfileID string                       `json:"profileId"`
		Data      map[string]map[string]string `json:"data"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		Error(w, http.StatusBadRequest, "invalid params")
		return
	}
	if params.DeviceID == 0 {
		Error(w, http.StatusBadRequest, "deviceId required — выберите устройство")
		return
	}
	if !userOwnsDevice(r, u.ID, params.DeviceID) {
		Error(w, http.StatusForbidden, fmt.Sprintf("forbidden: device %d not found or not yours", params.DeviceID))
		return
	}
	var rows []store.TimecodeRow
	for cardID, items := range params.Data {
		for item, dataStr := range items {
			if json.Valid([]byte(dataStr)) {
				rows = append(rows, store.TimecodeRow{CardID: cardID, Item: item, Data: dataStr})
			}
		}
	}
	saved := store.UpsertTimecodes(r.Context(), params.DeviceID, params.ProfileID, rows)
	JSON(w, http.StatusOK, map[string]any{"imported": saved})
}
