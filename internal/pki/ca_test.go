package pki_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"os"
	"testing"

	"github.com/ARCOOON/arx-mdm/internal/pki"

	"github.com/google/uuid"
)

func TestLoadOrInitialize_GeneratesAndReloads(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a1, err := pki.LoadOrInitialize(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(a1.RootCAPEM()) == 0 {
		t.Fatal("expected root PEM")
	}
	bundle := a1.MTLSClientCABundlePath()
	if _, err := os.Stat(bundle); err != nil {
		t.Fatalf("bundle: %v", err)
	}

	a2, err := pki.LoadOrInitialize(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(a2.RootCAPEM()) != string(a1.RootCAPEM()) {
		t.Fatal("reload should preserve root material")
	}
}

func TestIssueClientCertificate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, err := pki.LoadOrInitialize(dir)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM := mustGenerateTestCSR(t, "test-host.example")
	cr := mustParseCSR(t, csrPEM)
	res, err := a.IssueClientCertificate(context.Background(), uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8"), "test-host.example", cr)
	if err != nil {
		t.Fatal(err)
	}
	if res.ClientChainPEM == "" || res.RootCAPEM == "" {
		t.Fatalf("empty result: %+v", res)
	}
}

func mustGenerateTestCSR(t *testing.T, host string) []byte {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: host,
		},
		DNSNames:           []string{host},
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tpl, priv)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mustParseCSR(t *testing.T, pemData []byte) *x509.CertificateRequest {
	t.Helper()
	block, _ := pem.Decode(pemData)
	if block == nil {
		t.Fatal("pem decode")
	}
	cr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cr
}
