package auth

import (
	"crypto/x509"
	"encoding/pem"
)

func certificatePEM(c *x509.Certificate) string {
	if c == nil {
		return ""
	}
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
	return string(block)
}
