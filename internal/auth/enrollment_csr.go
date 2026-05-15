package auth

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

// ParseCertificateRequestPEM decodes the first PEM CSR block.
func ParseCertificateRequestPEM(pemData []byte) (*x509.CertificateRequest, error) {
	var block *pem.Block
	for {
		block, pemData = pem.Decode(pemData)
		if block == nil {
			return nil, errors.New("auth: no PEM block found in CSR")
		}
		if block.Type == "CERTIFICATE REQUEST" || block.Type == "NEW CERTIFICATE REQUEST" {
			break
		}
		if len(pemData) == 0 {
			return nil, fmt.Errorf("auth: unsupported PEM type %q for CSR", block.Type)
		}
	}

	cr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("auth: parse CSR: %w", err)
	}
	return cr, nil
}

// HostnameFromCSR returns the primary hostname: first DNS SAN, else trimmed CN.
func HostnameFromCSR(cr *x509.CertificateRequest) string {
	if cr == nil {
		return ""
	}
	if len(cr.DNSNames) > 0 {
		return strings.TrimSpace(cr.DNSNames[0])
	}
	return strings.TrimSpace(cr.Subject.CommonName)
}

// ValidateCSRPEM parses and verifies the CSR PEM self-signature.
func ValidateCSRPEM(csrPEM []byte) error {
	cr, err := ParseCertificateRequestPEM(csrPEM)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrEnrollmentCSRInvalid, err)
	}
	if err := cr.CheckSignature(); err != nil {
		return fmt.Errorf("%w: %v", ErrEnrollmentCSRInvalid, err)
	}
	return nil
}
