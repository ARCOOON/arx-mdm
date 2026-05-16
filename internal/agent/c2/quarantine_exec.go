package c2

import (
	"encoding/json"
	"fmt"
	"strings"
)

type quarantineWire struct {
	Enabled    bool     `json:"enabled"`
	AllowHosts []string `json:"allow_hosts"`
	AllowPorts []uint16 `json:"allow_ports"`
}

func executeQuarantine(payload string) (string, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "", fmt.Errorf("quarantine payload is required")
	}
	var w quarantineWire
	if err := json.Unmarshal([]byte(payload), &w); err != nil {
		return "", fmt.Errorf("quarantine payload JSON: %w", err)
	}
	hosts := w.AllowHosts
	ports := w.AllowPorts
	out, err := platformApplyQuarantine(w.Enabled, hosts, ports)
	if err != nil {
		return "", err
	}
	return out, nil
}
