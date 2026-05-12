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
type Conn struct {
	UserID   int64
	DeviceID int64
	WS       *websocket.Conn
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

// Broadcast sends msg to all connections of userID except the one with exceptDeviceID.
// Safe to call concurrently.
func (h *Hub) Broadcast(userID, exceptDeviceID int64, msg []byte) {
	h.mu.Lock()
	conns := make([]*Conn, 0, len(h.clients[userID]))
	for c := range h.clients[userID] {
		if c.DeviceID != exceptDeviceID {
			conns = append(conns, c)
		}
	}
	h.mu.Unlock()

	for _, c := range conns {
		c.WS.WriteMessage(websocket.TextMessage, msg) //nolint:errcheck
	}
}
