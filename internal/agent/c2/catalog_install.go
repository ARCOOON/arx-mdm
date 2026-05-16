package c2

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/agent"
)

type installAppDownlink struct {
	Action       string `json:"action"`
	AppID        string `json:"app_id"`
	ArtifactPath string `json:"artifact_path"`
	InstallArgs  string `json:"install_args"`
}

// RunCatalogInstall downloads staged catalog payloads and installs them silently on Windows/Linux desktops.
func RunCatalogInstall(ctx context.Context, logger *slog.Logger, write func(any) error, raw []byte) {
	var cmd installAppDownlink
	if err := json.Unmarshal(raw, &cmd); err != nil {
		if logger != nil {
			logger.Warn("install_app decode failed", "err", err)
		}
		return
	}
	appID := strings.TrimSpace(cmd.AppID)
	pathOrURL := strings.TrimSpace(cmd.ArtifactPath)
	args := strings.TrimSpace(cmd.InstallArgs)
	if appID == "" || pathOrURL == "" {
		reportInstall(write, appID, false, -2, "missing app_id or artifact_path")
		return
	}

	bridge := agent.ActiveInstallBridge()
	if bridge == nil || strings.TrimSpace(bridge.ServerURL) == "" {
		reportInstall(write, appID, false, -3, "agent install bridge not configured")
		return
	}
	fullURL, err := resolveArtifactURL(bridge.ServerURL, pathOrURL)
	if err != nil {
		reportInstall(write, appID, false, -4, err.Error())
		return
	}

	tmpPath, cleanup, err := downloadArtifact(ctx, bridge.ServerURL, bridge.CertDir, fullURL)
	if err != nil {
		reportInstall(write, appID, false, -5, err.Error())
		return
	}
	defer cleanup()

	exitCode, runErr := executeInstaller(ctx, tmpPath, args)
	ok := runErr == nil
	errStr := ""
	if runErr != nil {
		errStr = runErr.Error()
	}
	reportInstall(write, appID, ok, exitCode, errStr)
}

func reportInstall(write func(any) error, appID string, ok bool, exitCode int, errStr string) {
	if write == nil {
		return
	}
	_ = write(map[string]any{
		"type":      "install_app_result",
		"app_id":    strings.TrimSpace(appID),
		"ok":        ok,
		"exit_code": exitCode,
		"error":     errStr,
	})
}

func resolveArtifactURL(serverBase, artifactPath string) (string, error) {
	artifactPath = strings.TrimSpace(artifactPath)
	if artifactPath == "" {
		return "", fmt.Errorf("empty artifact path")
	}
	low := strings.ToLower(artifactPath)
	if strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "http://") {
		return artifactPath, nil
	}
	sb := strings.TrimRight(strings.TrimSpace(serverBase), "/")
	if sb == "" {
		return "", fmt.Errorf("server base url missing")
	}
	if !strings.HasPrefix(artifactPath, "/") {
		artifactPath = "/" + artifactPath
	}
	u, err := url.Parse(sb + artifactPath)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func downloadArtifact(ctx context.Context, serverBase, certDir, fullURL string) (string, func(), error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return "", nil, err
	}
	client, err := downloaderHTTPClient(serverBase, certDir, fullURL)
	if err != nil {
		return "", nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("download failed: http %d", resp.StatusCode)
	}
	dir, err := os.MkdirTemp("", "arx-install-*")
	if err != nil {
		return "", nil, err
	}
	base := filepath.Base(req.URL.Path)
	if base == "" || base == "." || base == "/" {
		base = "artifact.bin"
	}
	base = strings.ReplaceAll(base, string(os.PathSeparator), "_")
	dst := filepath.Join(dir, base)
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, err
	}
	const maxArtifact = int64(600 << 20)
	if _, err := io.Copy(f, io.LimitReader(resp.Body, maxArtifact)); err != nil {
		_ = f.Close()
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("save artifact: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, err
	}
	cleanup := func() {
		_ = os.RemoveAll(dir)
	}
	return dst, cleanup, nil
}

func downloaderHTTPClient(serverBase, certDir, fullURL string) (*http.Client, error) {
	if peerUsesEnrollmentCert(serverBase, fullURL) {
		return mtlsHTTPClient(serverBase, certDir)
	}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &http.Client{Transport: tr, Timeout: 45 * time.Minute}, nil
}

func peerUsesEnrollmentCert(serverBase, artifactURL string) bool {
	base, err := url.Parse(strings.TrimSpace(serverBase))
	dest, err2 := url.Parse(strings.TrimSpace(artifactURL))
	if err != nil || err2 != nil {
		return true
	}
	if !strings.EqualFold(dest.Scheme, "https") {
		return false
	}
	return hostsEqualEnrollmentScope(base.Host, dest.Host)
}

func hostsEqualEnrollmentScope(a, b string) bool {
	ha := hostStripPort(strings.TrimSpace(a))
	hb := hostStripPort(strings.TrimSpace(b))
	if ha == "" || hb == "" {
		return true
	}
	return strings.EqualFold(ha, hb)
}

func hostStripPort(h string) string {
	host, _, err := net.SplitHostPort(h)
	if err != nil {
		return h
	}
	return host
}

func mtlsHTTPClient(serverBase, certDir string) (*http.Client, error) {
	cfg, err := agent.MTLSClientConfig(serverBase, certDir)
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{
		TLSClientConfig: cfg.Clone(),
	}
	return &http.Client{
		Transport: tr,
		Timeout:   45 * time.Minute,
	}, nil
}

func executeInstaller(ctx context.Context, localPath, installArgs string) (exitCode int, err error) {
	return executeInstallerNative(ctx, localPath, installArgs)
}
