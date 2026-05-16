package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// tryBroadcastAgentUplink decodes agent-originated JSON and fans it out to dashboards with target_arx_id.
func tryBroadcastAgentUplink(ctx context.Context, pool *pgxpool.Pool, dash *DashboardHub, certSerial string, data []byte, logger *slog.Logger) bool {
	if pool == nil || dash == nil {
		return false
	}
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	switch strings.TrimSpace(probe.Type) {
	case agentMsgRegistryResult, agentMsgPtyOutput, agentMsgPtyExit, MsgTypePtyStarted, agentMsgPackageResult,
		agentMsgFsListDirResult, agentMsgNetListResult, agentMsgHostnameSetResult, agentMsgDeviceCommandResult:
	default:
		return false
	}
	hid, err := humanIDByCertSerial(ctx, pool, certSerial)
	if err != nil || strings.TrimSpace(hid) == "" {
		if logger != nil {
			logger.Debug("agent uplink dropped: no human_id for cert_serial", "cert_serial", certSerial, "err", err)
		}
		return true
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return true
	}
	m["target_arx_id"] = hid
	out, err := json.Marshal(m)
	if err != nil {
		return true
	}
	dash.Broadcast(out)
	return true
}

func humanIDByCertSerial(ctx context.Context, pool *pgxpool.Pool, certSerial string) (string, error) {
	certSerial = strings.TrimSpace(certSerial)
	if certSerial == "" {
		return "", nil
	}
	var hid string
	err := pool.QueryRow(ctx, `SELECT human_id FROM assets WHERE cert_serial = $1 LIMIT 1`, certSerial).Scan(&hid)
	return hid, err
}
