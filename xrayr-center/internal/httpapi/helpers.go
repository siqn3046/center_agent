package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func sha256SumHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
