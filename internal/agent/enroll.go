package agent

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// EnrollOptions configures a single enrollment run against the MDM server.
type EnrollOptions struct {
	ServerURL string
	Token     string
	CertDir   string
}

type enrollWireRequest struct {
	CSR   string `json:"csr"`
	Token string `json:"token"`
}

type enrollWireResponse struct {
	ClientCert string `json:"client_cert"`
	RootCA     string `json:"root_ca"`
}

type enrollWireError struct {
	Error string `json:"error"`
}

// Enroll generates a P-256 key and CSR, calls POST /v1/enroll, and writes PEM material to CertDir.
func Enroll(ctx context.Context, logger *slog.Logger, opts EnrollOptions) error {
	if logger == nil {
		return errors.New("agent: logger is required")
	}
	server := strings.TrimSpace(opts.ServerURL)
	token := strings.TrimSpace(opts.Token)
	if server == "" || token == "" {
		return errors.New("agent: ServerURL and Token are required")
	}
	certDir := strings.TrimSpace(opts.CertDir)
	if certDir == "" {
		certDir = defaultCertDir
	}

	priv, csrPEM, err := generateECDSACSR()
	if err != nil {
		return fmt.Errorf("agent: generate key/csr: %w", err)
	}

	enrollURL, err := joinEnrollURL(server)
	if err != nil {
		return err
	}

	body, err := json.Marshal(enrollWireRequest{CSR: string(csrPEM), Token: token})
	if err != nil {
		return fmt.Errorf("agent: marshal enroll request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, enrollURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("agent: build enroll request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := newPlainHTTPClient(enrollURL)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("agent: enroll http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("agent: read enroll response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		var ej enrollWireError
		if json.Unmarshal(respBody, &ej) == nil && ej.Error != "" {
			return fmt.Errorf("agent: enroll failed: status=%d message=%s", resp.StatusCode, ej.Error)
		}
		return fmt.Errorf("agent: enroll failed: status=%d body=%s", resp.StatusCode, truncateForLog(respBody))
	}

	var okResp enrollWireResponse
	if err := json.Unmarshal(respBody, &okResp); err != nil {
		return fmt.Errorf("agent: decode enroll success json: %w", err)
	}
	if strings.TrimSpace(okResp.ClientCert) == "" || strings.TrimSpace(okResp.RootCA) == "" {
		return fmt.Errorf("agent: enroll response missing client_cert or root_ca")
	}

	keyPath, certPath, rootPath := ClientMaterialPaths(certDir)
	keyPEM, err := marshalECPrivateKeyPKCS8(priv)
	if err != nil {
		return fmt.Errorf("agent: marshal private key: %w", err)
	}

	if err := os.MkdirAll(certDir, 0o700); err != nil {
		return fmt.Errorf("agent: create cert directory: %w", err)
	}
	if err := writeFileAtomic(keyPath, keyPEM, 0o600); err != nil {
		return fmt.Errorf("agent: write private key: %w", err)
	}
	if err := writeFileAtomic(certPath, []byte(okResp.ClientCert), 0o644); err != nil {
		return fmt.Errorf("agent: write client certificate: %w", err)
	}
	if err := writeFileAtomic(rootPath, []byte(okResp.RootCA), 0o644); err != nil {
		return fmt.Errorf("agent: write root ca: %w", err)
	}

	logger.Info("enrollment completed",
		"cert_dir", certDir,
		"client_cert", certPath,
		"root_ca", rootPath,
	)
	return nil
}

func joinEnrollURL(serverBase string) (string, error) {
	u, err := url.Parse(serverBase)
	if err != nil {
		return "", fmt.Errorf("agent: parse ServerURL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("agent: ServerURL must include scheme and host")
	}
	u = u.JoinPath("v1", "enroll")
	return u.String(), nil
}

func newPlainHTTPClient(requestURL string) *http.Client {
	u, err := url.Parse(requestURL)
	if err != nil {
		return &http.Client{Timeout: 120 * time.Second}
	}
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if u.Scheme == "https" {
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return &http.Client{Transport: tr, Timeout: 120 * time.Second}
}

func generateECDSACSR() (*ecdsa.PrivateKey, []byte, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "arx-agent"
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
		return nil, nil, err
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}); err != nil {
		return nil, nil, err
	}
	return priv, buf.Bytes(), nil
}

func marshalECPrivateKeyPKCS8(priv *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: "PRIVATE KEY", Bytes: der}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
