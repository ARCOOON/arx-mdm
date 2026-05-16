package pki

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"time"
)

const (
	serverCertFile = "server-cert.pem"
	serverKeyFile  = "server-key.pem"
)

// EnsureServerTLSMaterial writes server-cert.pem and server-key.pem signed by the intermediate CA
// when either file is missing from the PKI storage directory. extraDNS is appended as DNS SANs
// (typically from ARX_SERVER_DOMAIN). SANs always include DNS "localhost", IP "127.0.0.1", and extras.
//
// Returns absolute paths for the PEM files, whether new material was written, and nil on success when
// both files already existed.
func (a *Authority) EnsureServerTLSMaterial(ctx context.Context, extraDNS []string) (certAbs string, keyAbs string, minted bool, err error) {
	if a == nil {
		return "", "", false, errors.New("pki: authority is nil")
	}
	select {
	case <-ctx.Done():
		return "", "", false, ctx.Err()
	default:
	}

	certPath := filepath.Join(a.storageDir, serverCertFile)
	keyPath := filepath.Join(a.storageDir, serverKeyFile)
	if fileReadable(certPath) && fileReadable(keyPath) {
		return mustAbs(certPath), mustAbs(keyPath), false, nil
	}

	serverKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return "", "", false, fmt.Errorf("pki: generate server tls key: %w", err)
	}

	skID, err := subjectKeyID(&serverKey.PublicKey)
	if err != nil {
		return "", "", false, err
	}
	serial, err := randomSerial()
	if err != nil {
		return "", "", false, err
	}

	dnsNames := append([]string{"localhost"}, extraDNS...)
	dnsNames = uniqueStrings(dnsNames)

	cn := "localhost"
	for _, d := range dnsNames {
		if d != "localhost" {
			cn = d
			break
		}
	}

	ipSans := uniqueIPs([]net.IP{net.IPv4(127, 0, 0, 1)})
	now := time.Now().UTC()
	leafTpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"ARX MDM"},
			CommonName:   cn,
		},
		NotBefore:             now.Add(-2 * time.Minute),
		NotAfter:              now.AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,

		DNSNames:    dnsNames,
		IPAddresses: ipSans,

		SubjectKeyId:       skID,
		AuthorityKeyId:     a.intermediateCert.SubjectKeyId,
		SignatureAlgorithm: x509.ECDSAWithSHA384,

		PublicKey: serverKey.PublicKey,
	}

	select {
	case <-ctx.Done():
		return "", "", false, ctx.Err()
	default:
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	leafDER, err := x509.CreateCertificate(rand.Reader, leafTpl, a.intermediateCert, &serverKey.PublicKey, a.intermediateKey)
	if err != nil {
		return "", "", false, fmt.Errorf("pki: sign server tls certificate: %w", err)
	}

	serverCertPEM := pemEncodeCertificate(leafDER)
	serverKeyOut, err := marshalECPrivateKeyPEM(serverKey)
	if err != nil {
		return "", "", false, err
	}

	if err := writeFileExclusive(certPath, serverCertPEM, 0o644); err != nil {
		return "", "", false, fmt.Errorf("pki: write %s: %w", serverCertFile, err)
	}
	if err := writeFileExclusive(keyPath, serverKeyOut, 0o600); err != nil {
		return "", "", false, fmt.Errorf("pki: write %s: %w", serverKeyFile, err)
	}

	return mustAbs(certPath), mustAbs(keyPath), true, nil
}

func mustAbs(p string) string {
	s, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		return filepath.Clean(p)
	}
	return s
}

// NormalizeExtraTLSDNS splits and trims comma-separated hostnames into unique non-empty slices.
func NormalizeExtraTLSDNS(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var parts []string
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			parts = append(parts, s)
		}
	}
	return uniqueStrings(parts)
}
