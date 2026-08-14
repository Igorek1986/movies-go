package ws

import (
	"sync"

	"github.com/gorilla/websocket"
)

// Hub manages WebSocket connections grouped by userID.
type Hub struct {
	mu      sync.Mutex
	clients map[int64]map[*Conn]struct{}
}

// Conn is a single WebSocket connection bound to a device.
//
// ClientID identifies the physical browser tab/session (an in-memory-only id,
// regenerated on every page load — never persisted) — distinct from DeviceID,
// which identifies the activation token and can deliberately be shared across
// several physical screens (same token used on multiple TVs/phones/PCs).
// Broadcast excludes by ClientID when the sender provides one, so those
// screens still reach each other; DeviceID stays only as a fallback for
// requests that don't send a client_id yet.
type Conn struct {
	UserID   int64
	DeviceID int64
	ClientID string
	WS       *websocket.Conn

	writeMu sync.Mutex
}

// WriteMessage serializes concurrent writers — gorilla/websocket allows at
// most one goroutine writing to a Conn at a time, but Broadcast can be called
// concurrently from multiple HTTP handlers (timecode/favorite/profile_updated)
// targeting the same connection.
func (c *Conn) WriteMessage(messageType int, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.WS.WriteMessage(messageType, data)
}

func NewHub() *Hub {
	return &Hub{clients: make(map[int64]map[*Conn]struct{})}
}

func (h *Hub) Register(c *Conn) {
	h.mu.Lock()
	if h.clients[c.UserID] == nil {
		h.clients[c.UserID] = make(map[*Conn]struct{})
	}
	h.clients[c.UserID][c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) Unregister(c *Conn) {
	h.mu.Lock()
	if m := h.clients[c.UserID]; m != nil {
		delete(m, c)
		if len(m) == 0 {
			delete(h.clients, c.UserID)
		}
	}
	h.mu.Unlock()
}

// Broadcast sends msg to all connections of userID except the originating one.
// exceptClientID identifies the sending browser tab/session and takes
// priority when non-empty — this is what lets several screens share one
// activation token (same exceptDeviceID) and still reach each other.
// exceptDeviceID is only used as a fallback for connections/requests that
// don't carry a client_id yet. Safe to call concurrently.
func (h *Hub) Broadcast(userID, exceptDeviceID int64, exceptClientID string, msg []byte) {
	h.mu.Lock()
	all := h.clients[userID]
	conns := make([]*Conn, 0, len(all))
	skipped := 0
	for c := range all {
		var skip bool
		if exceptClientID != "" && c.ClientID != "" {
			skip = c.ClientID == exceptClientID
		} else {
			skip = c.DeviceID == exceptDeviceID
		}
		if skip {
			skipped++
		} else {
			conns = append(conns, c)
		}
	}
	h.mu.Unlock()

	for _, c := range conns {
		c.WriteMessage(websocket.TextMessage, msg) //nolint:errcheck
	}
}
