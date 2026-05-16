package notifications

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

func hmacSha256Hex(secret string, canonical string) string {
	m := hmac.New(sha256.New, []byte(secret))
	_, _ = m.Write([]byte(canonical))
	return hex.EncodeToString(m.Sum(nil))
}

func strconvFormatInt(v int64) string {
	return strconv.FormatInt(v, 10)
}
