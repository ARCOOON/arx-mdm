package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/mdm/compliance"
)

type agentEffectivePolicyPullResponse struct {
	EffectivePayload json.RawMessage `json:"effective_payload"`
}

type telemetryDeclarativeEnvelope struct {
	MDMProfiles []telemetryProfileEnvelope `json:"mdm_configuration_profiles"`
}

// ApplyMDMTelemetryAck processes optional MDM payloads embedded inside POST /v1/telemetry responses.
func ApplyMDMTelemetryAck(logger *slog.Logger, body []byte) {
	var env telemetryDeclarativeEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return
	}
	if len(env.MDMProfiles) == 0 {
		return
	}
	enforceDeclarativeProfiles(logger, env.MDMProfiles)
}

func joinAgentEffectivePolicyURL(serverBase string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(serverBase))
	if err != nil {
		return "", fmt.Errorf("agent: parse ServerURL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("agent: ServerURL must include scheme and host")
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.JoinPath("v1", "agent", "effective-policy").String(), nil
}

func fetchEffectivePolicy(ctx context.Context, serverBase, certDir string) ([]byte, error) {
	client, err := newTelemetryHTTPClient(serverBase, certDir)
	if err != nil {
		return nil, err
	}
	target, err := joinAgentEffectivePolicyURL(serverBase)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, bytes.NewReader(nil))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent: effective policy pull failed: status=%d body=%s", resp.StatusCode, truncateForLog(body))
	}
	return body, nil
}

func applyEffectivePolicyHTTPBody(logger *slog.Logger, body []byte) {
	noteMDMPolicyEnforcement(nil, false)
	var decoded agentEffectivePolicyPullResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		noteMDMPolicyEnforcement(err, true)
		if logger != nil {
			logger.Warn("mdm effective policy decode failed", "err", err)
		}
		return
	}
	raw := decoded.EffectivePayload
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage([]byte("{}"))
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		noteMDMPolicyEnforcement(err, true)
		if logger != nil {
			logger.Warn("mdm effective policy payload decode failed", "err", err)
		}
		return
	}
	critical := compliance.PayloadCarriesCriticalSecurityControls(root)
	enforceEffectiveMergedPayload(logger, runtime.GOOS, raw, critical)
}

func runMDMDeclarativeSyncLoop(ctx context.Context, logger *slog.Logger, serverBase, certDir string, interval time.Duration) {
	if interval <= 0 {
		interval = 90 * time.Second
	}
	if _, err := newTelemetryHTTPClient(serverBase, certDir); err != nil {
		if logger != nil {
			logger.Warn("mdm effective policy sync bootstrap failed", "err", err)
		}
		return
	}

	doFetch := func() {
		bg, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		body, ferr := fetchEffectivePolicy(bg, serverBase, certDir)
		if ferr != nil {
			noteMDMPolicyEnforcement(fmt.Errorf("effective policy fetch: %w", ferr), true)
			if logger != nil {
				logger.Warn("mdm effective policy fetch failed", "err", ferr)
			}
			return
		}
		applyEffectivePolicyHTTPBody(logger, body)
	}

	doFetch()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			doFetch()
		}
	}
}
