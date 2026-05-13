package httpapi

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

type agentHub struct {
	mu    sync.RWMutex
	nodes map[int64]*hubConn
}

type hubConn struct {
	nodeID int64
	conn   *websocket.Conn
	wmu    sync.Mutex
}

func newAgentHub() *agentHub {
	return &agentHub{nodes: make(map[int64]*hubConn)}
}

func (h *agentHub) register(nodeID int64, c *websocket.Conn) *hubConn {
	hc := &hubConn{nodeID: nodeID, conn: c}
	h.mu.Lock()
	if old, ok := h.nodes[nodeID]; ok && old != nil && old.conn != nil {
		_ = old.conn.Close()
	}
	h.nodes[nodeID] = hc
	h.mu.Unlock()
	return hc
}

func (h *agentHub) remove(hc *hubConn) {
	if hc == nil {
		return
	}
	h.mu.Lock()
	if cur, ok := h.nodes[hc.nodeID]; ok && cur == hc {
		delete(h.nodes, hc.nodeID)
	}
	h.mu.Unlock()
}

func (h *agentHub) sendJSON(nodeID int64, v any) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	h.mu.RLock()
	hc, ok := h.nodes[nodeID]
	h.mu.RUnlock()
	if !ok || hc == nil {
		return false
	}
	hc.wmu.Lock()
	defer hc.wmu.Unlock()
	if err := hc.conn.WriteMessage(websocket.TextMessage, b); err != nil {
		return false
	}
	return true
}
