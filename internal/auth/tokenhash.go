package auth

import (
	"crypto/sha256"
	"encoding/hex"
)

// EnrollmentPresentationHash returns the lowercase hex-encoded SHA-256 of the UTF-8
// presentation secret. This must match the value stored in enrollment_tokens.token_hash.
func EnrollmentPresentationHash(presentationSecret string) string {
	sum := sha256.Sum256([]byte(presentationSecret))
	return hex.EncodeToString(sum[:])
}
