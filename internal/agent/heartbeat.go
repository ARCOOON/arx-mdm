package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/api"
	"github.com/ARCOOON/arx-mdm/pkg/packagemanager"
	"github.com/ARCOOON/arx-mdm/pkg/system"
)

// HeartbeatOptions configures the periodic telemetry sender.
type HeartbeatOptions struct {
	ServerURL string
	CertDir   string
	Interval  time.Duration
}

// RunHeartbeat sends native system telemetry to POST /v1/telemetry over mTLS every Interval until ctx ends.
func RunHeartbeat(ctx context.Context, logger *slog.Logger, opts HeartbeatOptions) error {
	if logger == nil {
		return errors.New("agent: logger is required")
	}
	server := strings.TrimSpace(opts.ServerURL)
	if server == "" {
		return errors.New("agent: ServerURL is required")
	}
	certDir := strings.TrimSpace(opts.CertDir)
	if certDir == "" {
		certDir = defaultCertDir
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = 60 * time.Second
	}

	client, err := newTelemetryHTTPClient(server, certDir)
	if err != nil {
		return err
	}

	telemetryURL, err := joinTelemetryURL(server)
	if err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	send := func() error {
		info, err := system.CollectSystemInfo()
		if err != nil {
			return fmt.Errorf("agent: collect system info: %w", err)
		}
		inv, _ := packagemanager.ListInstalled()
		sw := make([]api.TelemetryInstalledApp, 0, len(inv))
		for _, a := range inv {
			sw = append(sw, api.TelemetryInstalledApp{
				Name: a.Name, Version: a.Version, Source: a.Source, ID: a.ID,
			})
		}
		payload := api.TelemetryPayload{
			Hostname:          info.Hostname,
			OSFamily:          info.OSFamily,
			OSVersion:         info.OSVersion,
			TotalRAMBytes:     info.TotalRAMBytes,
			CPUModel:          info.CPUModel,
			CPULogicalCores:   info.CPULogicalCores,
			CPUUsagePercent:   info.CPUUsagePercent,
			MemoryUsedBytes:   info.MemoryUsedBytes,
			InstalledSoftware: sw,
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("agent: marshal telemetry: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, telemetryURL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("agent: build telemetry request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("agent: telemetry request: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		if err != nil {
			return fmt.Errorf("agent: read telemetry response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("agent: telemetry failed: status=%d body=%s", resp.StatusCode, truncateForLog(respBody))
		}
		return nil
	}

	if err := send(); err != nil {
		logger.Warn("telemetry send failed", "err", err)
	} else {
		logger.Info("telemetry sent successfully")
	}

	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			return ctx.Err()
		case <-ticker.C:
			if err := send(); err != nil {
				logger.Warn("telemetry send failed", "err", err)
			} else {
				logger.Debug("telemetry sent")
			}
		}
	}
}

func joinTelemetryURL(serverBase string) (string, error) {
	u, err := url.Parse(serverBase)
	if err != nil {
		return "", fmt.Errorf("agent: parse ServerURL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("agent: ServerURL must include scheme and host")
	}
	u = u.JoinPath("v1", "telemetry")
	return u.String(), nil
}

func newTelemetryHTTPClient(serverBase, certDir string) (*http.Client, error) {
	tlsCfg, err := MTLSClientConfig(serverBase, certDir)
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{
		TLSClientConfig: tlsCfg,
		Proxy:           http.ProxyFromEnvironment,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   90 * time.Second,
	}, nil
}
