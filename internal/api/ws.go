package api

import (
	"lampa-api/internal/ws"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// TimecodeHub broadcasts timecode/favorite/profile_updated events.
var TimecodeHub = ws.NewHub()

// SettingsHub broadcasts plugin-settings patch events.
var SettingsHub = ws.NewHub()

// GET /timecode/ws?token=
func handleTimecodeWS(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	c := &ws.Conn{UserID: d.UserID, DeviceID: d.ID, WS: conn}
	TimecodeHub.Register(c)
	defer TimecodeHub.Unregister(c)

	conn.SetReadLimit(512)
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

// GET /api/plugin-settings/ws?token=
func handlePluginSettingsWS(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	c := &ws.Conn{UserID: d.UserID, DeviceID: d.ID, WS: conn}
	SettingsHub.Register(c)
	defer SettingsHub.Unregister(c)

	conn.SetReadLimit(512)
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}
