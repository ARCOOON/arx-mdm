package agent

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// MTLSClientConfig loads enrollment material from certDir and returns a tls.Config suitable for
// HTTPS and WSS connections to the MDM server at serverBase.
func MTLSClientConfig(serverBase, certDir string) (*tls.Config, error) {
	serverBase = strings.TrimSpace(serverBase)
	if serverBase == "" {
		return nil, fmt.Errorf("agent: server URL is required")
	}
	certDir = strings.TrimSpace(certDir)
	if certDir == "" {
		certDir = defaultCertDir
	}
	keyPath, certPath, rootPath := ClientMaterialPaths(certDir)
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("agent: load client tls material: %w", err)
	}
	rootPEM, err := os.ReadFile(rootPath)
	if err != nil {
		return nil, fmt.Errorf("agent: read root ca bundle: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(rootPEM) {
		return nil, fmt.Errorf("agent: no certificates parsed from root ca pem")
	}

	u, err := url.Parse(serverBase)
	if err != nil {
		return nil, fmt.Errorf("agent: parse server url: %w", err)
	}

	tlsCfg := &tls.Config{
		RootCAs:            pool,
		Certificates:       []tls.Certificate{cert},
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: false,
	}
	if host := u.Hostname(); host != "" {
		if ip := net.ParseIP(host); ip == nil {
			tlsCfg.ServerName = host
		}
	}
	return tlsCfg, nil
}
