package pki_test

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/ARCOOON/arx-mdm/internal/pki"
)

func TestEnsureServerTLSMaterialMintAndReload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, err := pki.LoadOrInitialize(dir)
	if err != nil {
		t.Fatal(err)
	}

	certAbs, keyAbs, minted, err := a.EnsureServerTLSMaterial(context.Background(), []string{"arx.example.lan"})
	if err != nil {
		t.Fatal(err)
	}
	if !minted {
		t.Fatal("expected first call to mint material")
	}
	if _, statErr := os.Stat(certAbs); statErr != nil {
		t.Fatal(statErr)
	}
	if _, statErr := os.Stat(keyAbs); statErr != nil {
		t.Fatal(statErr)
	}

	certBytes, err := os.ReadFile(filepath.Join(dir, "server-cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(firstCertBlockDER(t, certBytes))
	if err != nil {
		t.Fatal(err)
	}

	var foundLocalhost, foundIP, foundDomain bool
	for _, d := range leaf.DNSNames {
		switch d {
		case "localhost":
			foundLocalhost = true
		case "arx.example.lan":
			foundDomain = true
		}
	}
	if !foundLocalhost || !foundDomain {
		t.Fatalf("unexpected DNS SANs %#v", leaf.DNSNames)
	}
	for _, ip := range leaf.IPAddresses {
		if ip.String() == "127.0.0.1" {
			foundIP = true
		}
	}
	if !foundIP {
		t.Fatalf("expected 127.0.0.1 in IP SANs, got %#v", leaf.IPAddresses)
	}

	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(a.RootCAPEM())
	opts := x509.VerifyOptions{
		DNSName:       "arx.example.lan",
		Intermediates: intermediatePoolFromAuthority(t, a),
		Roots:         roots,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if _, err := leaf.Verify(opts); err != nil {
		t.Fatalf("verify leaf: %v", err)
	}

	_, _, mintedAgain, err := a.EnsureServerTLSMaterial(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if mintedAgain {
		t.Fatal("expected second call not to regenerate")
	}
}

func intermediatePoolFromAuthority(t *testing.T, a *pki.Authority) *x509.CertPool {
	t.Helper()
	p := x509.NewCertPool()
	if !p.AppendCertsFromPEM(a.IntermediateCAPEM()) {
		t.Fatal("append intermediate PEM")
	}
	return p
}

func firstCertBlockDER(t *testing.T, pemBytes []byte) []byte {
	t.Helper()
	var block *pem.Block
	for {
		block, pemBytes = pem.Decode(pemBytes)
		if block == nil {
			t.Fatal("no pem block")
		}
		if block.Type == "CERTIFICATE" {
			return block.Bytes
		}
	}
}
