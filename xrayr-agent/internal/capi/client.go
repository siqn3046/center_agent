package capi

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/XrayR-project/XrayR/xrayr-agent/internal/agentcfg"
	"github.com/XrayR-project/XrayR/xrayr-agent/internal/hsign"
)

type Client struct {
	BaseURL string
	NodeID  string
	Secret  string
	HTTP    *http.Client
}

func New(cfg *agentcfg.File) *Client {
	cl := &http.Client{Timeout: 30 * time.Second}
	if !cfg.Center.TLSVerify {
		cl.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	return &Client{
		BaseURL: stringsTrim(cfg.Center.URL),
		NodeID:  cfg.Center.NodeID,
		Secret:  cfg.Center.NodeSecret,
		HTTP:    cl,
	}
}

func stringsTrim(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func (c *Client) Register(payload any) (map[string]any, error) {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, c.BaseURL+"/api/agent/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("register %d: %s", resp.StatusCode, string(b))
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) SignedJSON(method, path string, payload any) error {
	body, _ := json.Marshal(payload)
	return c.signedRequest(method, path, body)
}

func (c *Client) SignedGET(path string) ([]byte, error) {
	return c.signedRequestBytes(http.MethodGet, path, nil)
}

func (c *Client) signedRequest(method, path string, body []byte) error {
	_, err := c.signedRequestBytes(method, path, body)
	return err
}

func (c *Client) signedRequestBytes(method, path string, body []byte) ([]byte, error) {
	if body == nil {
		body = []byte{}
	}
	ts := time.Now().Unix()
	nonce := randomNonce()
	bh := hsign.BodyHash(body)
	sig := hsign.Sign(c.Secret, method, path, ts, nonce, body)
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	req, _ := http.NewRequest(method, c.BaseURL+path, rdr)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Node-Id", c.NodeID)
	req.Header.Set("X-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Body-Hash", bh)
	req.Header.Set("X-Signature", sig)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, string(out))
	}
	return out, nil
}

func randomNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
