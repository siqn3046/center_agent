package wsclient

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/XrayR-project/XrayR/xrayr-agent/internal/agentcfg"
	"github.com/XrayR-project/XrayR/xrayr-agent/internal/hsign"
	"github.com/gorilla/websocket"
)

type Push struct {
	CommandID   string
	CommandType string
	Payload     map[string]any
}

func wsBase(httpBase string) string {
	if strings.HasPrefix(httpBase, "https://") {
		return "wss://" + strings.TrimPrefix(httpBase, "https://")
	}
	if strings.HasPrefix(httpBase, "http://") {
		return "ws://" + strings.TrimPrefix(httpBase, "http://")
	}
	return "ws://" + httpBase
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// Run 与 Center 建立 WebSocket，将 command_push 写入 out。
func Run(cfg *agentcfg.File, secret string, out chan<- Push) {
	base := trimSlash(cfg.Center.URL)
	path := "/ws/agent"
	emptyBody := []byte(nil)
	emptyHash := hsign.BodyHash(emptyBody)
	for {
		ts := time.Now().Unix()
		nonce := randomHex(16)
		sig := hsign.Sign(secret, "GET", path, ts, nonce, emptyBody)
		q := url.Values{}
		q.Set("node_id", cfg.Center.NodeID)
		q.Set("ts", strconv.FormatInt(ts, 10))
		q.Set("nonce", nonce)
		q.Set("body_hash", emptyHash)
		q.Set("signature", sig)
		u := wsBase(base) + path + "?" + q.Encode()
		d := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
		if !cfg.Center.TLSVerify {
			d.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		}
		conn, _, err := d.Dial(u, nil)
		if err != nil {
			log.Println("ws dial:", err)
			time.Sleep(5 * time.Second)
			continue
		}
		readLoop(conn, out)
		_ = conn.Close()
		time.Sleep(3 * time.Second)
	}
}

func readLoop(c *websocket.Conn, out chan<- Push) {
	for {
		_, data, err := c.ReadMessage()
		if err != nil {
			return
		}
		var msg map[string]any
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg["type"] != "command_push" {
			continue
		}
		cid, _ := msg["command_id"].(string)
		ct, _ := msg["command_type"].(string)
		var pay map[string]any
		if p, ok := msg["payload"].(map[string]any); ok {
			pay = p
		}
		if cid == "" || ct == "" {
			continue
		}
		select {
		case out <- Push{CommandID: cid, CommandType: strings.ToUpper(strings.TrimSpace(ct)), Payload: pay}:
		default:
			log.Println("ws command dropped (channel full)")
		}
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
