package api

import (
	"movies-api/internal/ws"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// TimecodeHub broadcasts timecode/favorite/profile_updated events.
var TimecodeHub = ws.NewHub()

// SettingsHub broadcasts plugin-settings patch events.
var SettingsHub = ws.NewHub()

// Keepalive: without pings a connection that dies silently (app backgrounded
// on a TV, NAT/proxy dropping an idle socket) stays registered in the Hub
// forever — Broadcast "succeeds" into the void and the read loop never errors
// out to trigger Unregister.
const (
	wsPongWait   = 60 * time.Second
	wsPingPeriod = wsPongWait * 9 / 10
)

// GET /timecode/ws?token=
func handleTimecodeWS(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	serveHubWS(TimecodeHub, d.UserID, d.ID, w, r)
}

// GET /api/plugin-settings/ws?token=
func handlePluginSettingsWS(w http.ResponseWriter, r *http.Request) {
	d := deviceFromRequest(r)
	if d == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	serveHubWS(SettingsHub, d.UserID, d.ID, w, r)
}

// GET /api/web/ws — веб-эквивалент handleTimecodeWS: тот же TimecodeHub (статус/
// таймкод/избранное/профиль), но авторизация по сессионной cookie вместо
// device-токена — браузер токена устройства не получает и не должен (см.
// обсуждение в dev-заметках: отдавать его в JS небезопасно). deviceID=0 —
// у веб-соединения нет своего устройства; исключение эха собственных изменений
// идёт через client_id (см. handleWebSetStatus), а не через DeviceID.
func handleWebWS(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r)
	if u == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	serveHubWS(TimecodeHub, u.ID, 0, w, r)
}

func serveHubWS(hub *ws.Hub, userID, deviceID int64, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	c := &ws.Conn{UserID: userID, DeviceID: deviceID, ClientID: r.URL.Query().Get("client_id"), WS: conn}
	hub.Register(c)
	defer hub.Unregister(c)

	conn.SetReadLimit(512)
	conn.SetReadDeadline(time.Now().Add(wsPongWait)) //nolint:errcheck
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})

	done := make(chan struct{})
	defer close(done)

	go func() {
		ticker := time.NewTicker(wsPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// WriteControl is safe to call concurrently with WriteMessage
				// (gorilla/websocket serializes control frames internally),
				// so it doesn't need to go through Conn.WriteMessage's mutex.
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
					conn.Close()
					return
				}
			case <-done:
				return
			}
		}
	}()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}
