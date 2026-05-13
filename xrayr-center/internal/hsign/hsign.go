package hsign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func BodyHash(body []byte) string {
	h := sha256.Sum256(body)
	return hex.EncodeToString(h[:])
}

func SigningString(method, path string, ts int64, nonce, bodyHash string) string {
	return strings.ToUpper(method) + "\n" + path + "\n" + strconv.FormatInt(ts, 10) + "\n" + nonce + "\n" + bodyHash
}

func Sign(secret, method, path string, ts int64, nonce string, body []byte) string {
	bh := BodyHash(body)
	ss := SigningString(method, path, ts, nonce, bh)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(ss))
	return hex.EncodeToString(mac.Sum(nil))
}

func ConstantTimeEqual(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

func Verify(secret, method, path string, ts int64, nonce, bodyHash, sig string, body []byte, skew time.Duration, seen func(string) bool) error {
	if seen != nil && seen(nonce) {
		return fmt.Errorf("nonce replay")
	}
	now := time.Now().Unix()
	if abs(now-ts) > int64(skew.Seconds()) {
		return fmt.Errorf("timestamp skew")
	}
	if bodyHash != BodyHash(body) {
		return fmt.Errorf("body hash mismatch")
	}
	want := Sign(secret, method, path, ts, nonce, body)
	if !ConstantTimeEqual(want, sig) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
