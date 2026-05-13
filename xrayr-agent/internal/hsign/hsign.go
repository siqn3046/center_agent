package hsign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
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
