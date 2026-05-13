package httpapi

import (
	"net/http"
	"strconv"

	"github.com/XrayR-project/XrayR/xrayr-center/internal/hsign"
)

func (s *Server) agentWebSocket(w http.ResponseWriter, r *http.Request) {
	nodeIDStr := r.URL.Query().Get("node_id")
	tsStr := r.URL.Query().Get("ts")
	nonce := r.URL.Query().Get("nonce")
	bh := r.URL.Query().Get("body_hash")
	sig := r.URL.Query().Get("signature")
	if nodeIDStr == "" || tsStr == "" || nonce == "" || bh == "" || sig == "" {
		http.Error(w, "missing auth query", http.StatusUnauthorized)
		return
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		http.Error(w, "bad timestamp", http.StatusUnauthorized)
		return
	}
	nodeID, err := strconv.ParseInt(nodeIDStr, 10, 64)
	if err != nil {
		http.Error(w, "bad node id", http.StatusUnauthorized)
		return
	}
	var secret string
	err = s.pool.QueryRow(r.Context(), `SELECT node_hmac_key FROM node WHERE id=$1 AND disabled=false`, nodeID).Scan(&secret)
	if err != nil || secret == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body := []byte{}
	path := "/ws/agent"
	if err := hsign.Verify(secret, http.MethodGet, path, ts, nonce, bh, sig, body, s.nonce.Skew(), nil); err != nil {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	if s.nonce.Seen(nonce) {
		http.Error(w, "nonce replay", http.StatusUnauthorized)
		return
	}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	hc := s.hub.register(nodeID, c)
	defer s.hub.remove(hc)
	defer c.Close()

	ctx := r.Context()
	go s.flushPendingCommands(ctx, nodeID)

	for {
		_, _, err := c.ReadMessage()
		if err != nil {
			break
		}
	}
}
