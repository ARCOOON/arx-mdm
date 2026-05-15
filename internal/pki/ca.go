package pki

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	rootCertFile        = "root-ca.pem"
	rootKeyFile         = "root-ca.key"
	intermediateCertFile = "intermediate-ca.pem"
	intermediateKeyFile  = "intermediate-ca.key"
	mtlsClientBundleFile = "mtls-client-ca-bundle.pem"
)

// IssueClientCertificateResult is PEM material returned to enrolling agents.
type IssueClientCertificateResult struct {
	// ClientChainPEM is the leaf certificate followed by the intermediate CA (PEM concatenated).
	ClientChainPEM string
	// RootCAPEM is the embedded root CA certificate (PEM) for trusting the MDM server TLS chain.
	RootCAPEM string
}

// Authority holds an embedded two-tier CA (root + intermediate) and issues client certificates.
type Authority struct {
	mu sync.Mutex

	storageDir string

	rootCert          *x509.Certificate
	rootKey           *ecdsa.PrivateKey
	intermediateCert  *x509.Certificate
	intermediateKey   *ecdsa.PrivateKey

	rootPEM         []byte
	intermediatePEM []byte
}

// LoadOrInitialize creates storageDir if needed, loads existing PEM material, or generates a new
// root and intermediate CA (ECDSA P-384). Private keys are written with restrictive file permissions.
func LoadOrInitialize(storageDir string) (*Authority, error) {
	storageDir = strings.TrimSpace(storageDir)
	if storageDir == "" {
		return nil, errors.New("pki: storage directory is required")
	}
	if err := os.MkdirAll(storageDir, 0o700); err != nil {
		return nil, fmt.Errorf("pki: create storage directory: %w", err)
	}

	a := &Authority{storageDir: storageDir}

	rootCertPath := filepath.Join(storageDir, rootCertFile)
	rootKeyPath := filepath.Join(storageDir, rootKeyFile)
	intCertPath := filepath.Join(storageDir, intermediateCertFile)
	intKeyPath := filepath.Join(storageDir, intermediateKeyFile)

	allPresent := fileReadable(rootCertPath) && fileReadable(rootKeyPath) &&
		fileReadable(intCertPath) && fileReadable(intKeyPath)

	if allPresent {
		if err := a.loadFromDisk(rootCertPath, rootKeyPath, intCertPath, intKeyPath); err != nil {
			return nil, err
		}
	} else {
		if err := a.generateAndPersist(rootCertPath, rootKeyPath, intCertPath, intKeyPath); err != nil {
			return nil, err
		}
	}

	bundlePath := filepath.Join(storageDir, mtlsClientBundleFile)
	bundle := concatPEM(a.intermediatePEM, a.rootPEM)
	if err := writeFileExclusive(bundlePath, bundle, 0o644); err != nil {
		return nil, fmt.Errorf("pki: write %s: %w", mtlsClientBundleFile, err)
	}

	return a, nil
}

func fileReadable(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode().IsRegular()
}

func (a *Authority) loadFromDisk(rootCertPath, rootKeyPath, intCertPath, intKeyPath string) error {
	rootPEM, err := os.ReadFile(rootCertPath)
	if err != nil {
		return fmt.Errorf("pki: read root cert: %w", err)
	}
	rootKeyPEM, err := os.ReadFile(rootKeyPath)
	if err != nil {
		return fmt.Errorf("pki: read root key: %w", err)
	}
	intPEM, err := os.ReadFile(intCertPath)
	if err != nil {
		return fmt.Errorf("pki: read intermediate cert: %w", err)
	}
	intKeyPEM, err := os.ReadFile(intKeyPath)
	if err != nil {
		return fmt.Errorf("pki: read intermediate key: %w", err)
	}

	rootCert, rootKey, err := parseECCertAndKey(rootPEM, rootKeyPEM)
	if err != nil {
		return fmt.Errorf("pki: parse root material: %w", err)
	}
	intCert, intKey, err := parseECCertAndKey(intPEM, intKeyPEM)
	if err != nil {
		return fmt.Errorf("pki: parse intermediate material: %w", err)
	}

	if err := verifyHierarchy(rootCert, intCert); err != nil {
		return err
	}

	a.rootCert = rootCert
	a.rootKey = rootKey
	a.intermediateCert = intCert
	a.intermediateKey = intKey
	a.rootPEM = pemEncodeCertificate(rootCert.Raw)
	a.intermediatePEM = pemEncodeCertificate(intCert.Raw)
	return nil
}

func verifyHierarchy(root, intermediate *x509.Certificate) error {
	if root == nil || intermediate == nil {
		return errors.New("pki: loaded CA certificates are nil")
	}
	if !root.IsCA {
		return errors.New("pki: root certificate is not a CA")
	}
	if !intermediate.IsCA {
		return errors.New("pki: intermediate certificate is not a CA")
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	opts := x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}
	if _, err := intermediate.Verify(opts); err != nil {
		return fmt.Errorf("pki: intermediate does not chain to root: %w", err)
	}
	return nil
}

func parseECCertAndKey(certPEM, keyPEM []byte) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	cert, err := parseFirstCertificatePEM(certPEM)
	if err != nil {
		return nil, nil, err
	}
	key, err := parseECPrivateKeyPEM(keyPEM)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func (a *Authority) generateAndPersist(rootCertPath, rootKeyPath, intCertPath, intKeyPath string) error {
	now := time.Now().UTC()

	rootKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return fmt.Errorf("pki: generate root key: %w", err)
	}
	rootSKID, err := subjectKeyID(&rootKey.PublicKey)
	if err != nil {
		return err
	}

	rootSerial, err := randomSerial()
	if err != nil {
		return err
	}
	rootTpl := &x509.Certificate{
		SerialNumber: rootSerial,
		Subject: pkix.Name{
			Organization: []string{"ARX MDM"},
			CommonName:   "ARX MDM Embedded Root CA",
		},
		NotBefore:             now.Add(-2 * time.Minute),
		NotAfter:              now.AddDate(20, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
		SignatureAlgorithm:    x509.ECDSAWithSHA384,
		SubjectKeyId:          rootSKID,
	}

	rootDER, err := x509.CreateCertificate(rand.Reader, rootTpl, rootTpl, &rootKey.PublicKey, rootKey)
	if err != nil {
		return fmt.Errorf("pki: create root certificate: %w", err)
	}
	rootCert, err := x509.ParseCertificate(rootDER)
	if err != nil {
		return fmt.Errorf("pki: parse generated root: %w", err)
	}

	intKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return fmt.Errorf("pki: generate intermediate key: %w", err)
	}
	intSKID, err := subjectKeyID(&intKey.PublicKey)
	if err != nil {
		return err
	}
	intSerial, err := randomSerial()
	if err != nil {
		return err
	}
	intTpl := &x509.Certificate{
		SerialNumber: intSerial,
		Subject: pkix.Name{
			Organization: []string{"ARX MDM"},
			CommonName:   "ARX MDM Embedded Intermediate CA",
		},
		NotBefore:             now.Add(-2 * time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
		SignatureAlgorithm:    x509.ECDSAWithSHA384,
		SubjectKeyId:          intSKID,
		AuthorityKeyId:        rootCert.SubjectKeyId,
	}

	intDER, err := x509.CreateCertificate(rand.Reader, intTpl, rootCert, &intKey.PublicKey, rootKey)
	if err != nil {
		return fmt.Errorf("pki: create intermediate certificate: %w", err)
	}
	intCert, err := x509.ParseCertificate(intDER)
	if err != nil {
		return fmt.Errorf("pki: parse generated intermediate: %w", err)
	}

	rootCertPEM := pemEncodeCertificate(rootDER)
	rootKeyOut, err := marshalECPrivateKeyPEM(rootKey)
	if err != nil {
		return err
	}
	intCertPEM := pemEncodeCertificate(intDER)
	intKeyOut, err := marshalECPrivateKeyPEM(intKey)
	if err != nil {
		return err
	}

	if err := writeFileExclusive(rootCertPath, rootCertPEM, 0o644); err != nil {
		return fmt.Errorf("pki: write root cert: %w", err)
	}
	if err := writeFileExclusive(rootKeyPath, rootKeyOut, 0o600); err != nil {
		return fmt.Errorf("pki: write root key: %w", err)
	}
	if err := writeFileExclusive(intCertPath, intCertPEM, 0o644); err != nil {
		return fmt.Errorf("pki: write intermediate cert: %w", err)
	}
	if err := writeFileExclusive(intKeyPath, intKeyOut, 0o600); err != nil {
		return fmt.Errorf("pki: write intermediate key: %w", err)
	}

	a.rootCert = rootCert
	a.rootKey = rootKey
	a.intermediateCert = intCert
	a.intermediateKey = intKey
	a.rootPEM = rootCertPEM
	a.intermediatePEM = intCertPEM
	return nil
}

// IssueClientCertificate verifies csr and issues a client authentication leaf signed by the
// intermediate CA. hostname is applied as CN (and DNS SAN when it parses as a hostname or IP).
func (a *Authority) IssueClientCertificate(ctx context.Context, assetID uuid.UUID, hostname string, csr *x509.CertificateRequest) (*IssueClientCertificateResult, error) {
	if a == nil || csr == nil {
		return nil, errors.New("pki: authority or csr is nil")
	}
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return nil, errors.New("pki: hostname is required")
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("pki: invalid csr signature: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if err := validateCSRPublicKey(csr.PublicKey); err != nil {
		return nil, err
	}

	sigAlg, err := signatureAlgorithmForPub(csr.PublicKey)
	if err != nil {
		return nil, err
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	leafSKID, err := subjectKeyID(csr.PublicKey)
	if err != nil {
		return nil, err
	}

	dnsNames := uniqueStrings(append([]string{}, csr.DNSNames...))
	if hostIsDNS(hostname) {
		dnsNames = append([]string{hostname}, dnsNames...)
		dnsNames = uniqueStrings(dnsNames)
	}

	ipSans := append([]net.IP(nil), csr.IPAddresses...)
	if ip := net.ParseIP(hostname); ip != nil {
		ipSans = append([]net.IP{ip}, ipSans...)
		ipSans = uniqueIPs(ipSans)
	}

	subject := csr.Subject
	subject.CommonName = hostname

	now := time.Now().UTC()
	leafTpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      subject,
		NotBefore:    now.Add(-2 * time.Minute),
		NotAfter:     now.AddDate(1, 0, 0),

		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},

		BasicConstraintsValid: true,
		IsCA:                  false,

		SubjectKeyId:          leafSKID,
		AuthorityKeyId:        a.intermediateCert.SubjectKeyId,
		SignatureAlgorithm:    sigAlg,
		DNSNames:              dnsNames,
		IPAddresses:           ipSans,
		PublicKey:             csr.PublicKey,
		PublicKeyAlgorithm:    csr.PublicKeyAlgorithm,
	}

	if assetID != uuid.Nil {
		uriStr := fmt.Sprintf("urn:uuid:%s", assetID.String())
		u, err := url.Parse(uriStr)
		if err != nil {
			return nil, fmt.Errorf("pki: asset uri: %w", err)
		}
		leafTpl.URIs = []*url.URL{u}
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	leafDER, err := x509.CreateCertificate(rand.Reader, leafTpl, a.intermediateCert, csr.PublicKey, a.intermediateKey)
	if err != nil {
		return nil, fmt.Errorf("pki: sign client certificate: %w", err)
	}

	leafPEM := pemEncodeCertificate(leafDER)
	chain := concatPEM(leafPEM, a.intermediatePEM)

	return &IssueClientCertificateResult{
		ClientChainPEM: string(chain),
		RootCAPEM:      string(a.rootPEM),
	}, nil
}

func hostIsDNS(host string) bool {
	if host == "" {
		return false
	}
	if net.ParseIP(host) != nil {
		return false
	}
	return true
}

func validateCSRPublicKey(pub crypto.PublicKey) error {
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		if k == nil || k.Curve == nil {
			return errors.New("pki: csr public key is invalid ecdsa")
		}
		switch k.Curve {
		case elliptic.P256(), elliptic.P384(), elliptic.P521():
			return nil
		default:
			return fmt.Errorf("pki: unsupported ecdsa curve %s", k.Params().Name)
		}
	case *rsa.PublicKey:
		if k == nil || k.N == nil {
			return errors.New("pki: csr public key is invalid rsa")
		}
		if k.N.BitLen() < 2048 {
			return errors.New("pki: rsa public key must be at least 2048 bits")
		}
		return nil
	default:
		return fmt.Errorf("pki: unsupported public key type %T", pub)
	}
}

func signatureAlgorithmForPub(pub crypto.PublicKey) (x509.SignatureAlgorithm, error) {
	switch k := pub.(type) {
	case *ecdsa.PublicKey:
		switch k.Curve {
		case elliptic.P521():
			return x509.ECDSAWithSHA512, nil
		case elliptic.P384():
			return x509.ECDSAWithSHA384, nil
		default:
			return x509.ECDSAWithSHA256, nil
		}
	case *rsa.PublicKey:
		return x509.SHA256WithRSA, nil
	default:
		return x509.UnknownSignatureAlgorithm, fmt.Errorf("pki: unsupported public key %T", pub)
	}
}

func subjectKeyID(pub crypto.PublicKey) ([]byte, error) {
	b, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("pki: marshal public key: %w", err)
	}
	sum := sha256.Sum256(b)
	return sum[:20], nil
}

func randomSerial() (*big.Int, error) {
	const serialBits = 159
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), serialBits))
	if err != nil {
		return nil, fmt.Errorf("pki: serial: %w", err)
	}
	if n.Sign() <= 0 {
		return nil, errors.New("pki: invalid serial")
	}
	return n, nil
}

func parseFirstCertificatePEM(data []byte) (*x509.Certificate, error) {
	var block *pem.Block
	for {
		block, data = pem.Decode(data)
		if block == nil {
			return nil, errors.New("pki: no certificate pem block")
		}
		if block.Type == "CERTIFICATE" {
			break
		}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("pki: parse certificate: %w", err)
	}
	return cert, nil
}

func parseECPrivateKeyPEM(data []byte) (*ecdsa.PrivateKey, error) {
	var block *pem.Block
	for {
		block, data = pem.Decode(data)
		if block == nil {
			return nil, errors.New("pki: no private key pem block")
		}
		if block.Type == "EC PRIVATE KEY" || block.Type == "PRIVATE KEY" {
			break
		}
	}
	if block.Type == "EC PRIVATE KEY" {
		k, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("pki: parse ec private key: %w", err)
		}
		return k, nil
	}
	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("pki: parse pkcs8 private key: %w", err)
	}
	k, ok := keyAny.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("pki: expected ecdsa private key, got %T", keyAny)
	}
	return k, nil
}

func marshalECPrivateKeyPEM(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("pki: marshal pkcs8: %w", err)
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "PRIVATE KEY", Bytes: der}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func pemEncodeCertificate(der []byte) []byte {
	var buf bytes.Buffer
	_ = pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	return buf.Bytes()
}

func concatPEM(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}

func writeFileExclusive(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".arx-pki-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	_ = os.Remove(path)
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func uniqueIPs(in []net.IP) []net.IP {
	seen := make(map[string]struct{}, len(in))
	out := make([]net.IP, 0, len(in))
	for _, ip := range in {
		if ip == nil {
			continue
		}
		k := ip.String()
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, ip)
	}
	return out
}

// StorageDir returns the directory holding CA material.
func (a *Authority) StorageDir() string {
	if a == nil {
		return ""
	}
	return a.storageDir
}

// RootCAPEM returns the PEM-encoded root certificate.
func (a *Authority) RootCAPEM() []byte {
	if a == nil {
		return nil
	}
	return append([]byte(nil), a.rootPEM...)
}

// IntermediateCAPEM returns the PEM-encoded intermediate CA certificate.
func (a *Authority) IntermediateCAPEM() []byte {
	if a == nil {
		return nil
	}
	return append([]byte(nil), a.intermediatePEM...)
}

// MTLSClientCABundlePath returns the path to intermediate+root bundle written during initialization.
func (a *Authority) MTLSClientCABundlePath() string {
	if a == nil {
		return ""
	}
	return filepath.Join(a.storageDir, mtlsClientBundleFile)
}
