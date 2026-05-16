package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/auth"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const dashReadDeadline = 120 * time.Second

type dashboardCommand struct {
	Action      string `json:"action"`
	TargetArxID string `json:"target_arx_id"`
}

func dashWritePump(c *dashboardClient, logger *slog.Logger) {
	for msg := range c.send {
		_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteDeadline))
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			if logger != nil {
				logger.Debug("dashboard websocket write failed", "err", err)
			}
			_ = c.conn.Close()
			return
		}
	}
}

// RunDashboardSession serves one dashboard browser client after a successful WS upgrade.
func RunDashboardSession(r *http.Request, c *dashboardClient, pool *pgxpool.Pool, c2 *Hub, logger *slog.Logger) {
	defer func() {
		_ = c.conn.Close()
		c.hub.unregister(c)
		if logger != nil {
			logger.Info("dashboard websocket disconnected", "request_id", r.Header.Get("X-Request-Id"))
		}
	}()

	go dashWritePump(c, logger)

	ctx := r.Context()
	assets, err := LoadAssetSnapshot(ctx, pool, c2)
	if err != nil {
		if logger != nil {
			logger.Error("dashboard asset snapshot failed", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		}
		replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: false, Message: "snapshot query failed"})
		return
	}
	snap, err := json.Marshal(AssetSnapshotMsg{Type: MsgTypeAssetSnapshot, Assets: assets})
	if err != nil {
		replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: false, Message: "snapshot encode failed"})
		return
	}
	select {
	case c.send <- snap:
	case <-time.After(5 * time.Second):
		if logger != nil {
			logger.Warn("dashboard snapshot send timed out", "request_id", r.Header.Get("X-Request-Id"))
		}
		return
	}

	incidents, err := LoadIncidentSnapshot(ctx, pool)
	if err != nil {
		if logger != nil {
			logger.Error("dashboard incident snapshot failed", "err", err, "request_id", r.Header.Get("X-Request-Id"))
		}
		replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: false, Message: "incident snapshot query failed"})
		return
	}
	tickBytes, err := json.Marshal(IncidentSnapshotMsg{Type: MsgTypeIncidentSnapshot, Incidents: incidents})
	if err != nil {
		replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: false, Message: "ticket snapshot encode failed"})
		return
	}
	select {
	case c.send <- tickBytes:
	case <-time.After(5 * time.Second):
		if logger != nil {
			logger.Warn("dashboard incident snapshot send timed out", "request_id", r.Header.Get("X-Request-Id"))
		}
		return
	}

	_ = c.conn.SetReadDeadline(time.Now().Add(dashReadDeadline))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(dashReadDeadline))
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure, websocket.CloseNoStatusReceived) &&
				!errors.Is(err, websocket.ErrCloseSent) {
				if logger != nil {
					logger.Debug("dashboard websocket read ended", "err", err)
				}
			}
			return
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(dashReadDeadline))

		var cmd dashboardCommand
		if err := json.Unmarshal(data, &cmd); err != nil {
			replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: false, Message: "invalid JSON command"})
			continue
		}
		action := strings.TrimSpace(strings.ToLower(cmd.Action))
		target := strings.TrimSpace(cmd.TargetArxID)
		if action == "" {
			replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: false, Message: "action is required"})
			continue
		}
		if target == "" {
			replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: false, Message: "target_arx_id is required"})
			continue
		}
		if c.principal.Role == auth.RoleViewer {
			replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: false, Message: "viewer role cannot dispatch commands"})
			continue
		}

		switch action {
		case "shutdown":
			if c2 == nil || pool == nil {
				replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: false, Message: "command dispatch unavailable"})
				continue
			}
			dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			err := c2.DispatchJSONByHumanID(dctx, pool, target, map[string]string{"action": "shutdown"})
			cancel()
			if err != nil {
				replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: false, Message: err.Error()})
				continue
			}
			replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: true, Message: "shutdown dispatched"})
			auditDashboardDispatch(r, ctx, pool, logger, c.principal.UserID, "shutdown", target)
		case "lock", "wipe":
			if c.principal.Role != auth.RoleAdmin {
				replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: false, Message: "admin role required"})
				continue
			}
			if c2 == nil || pool == nil {
				replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: false, Message: "command dispatch unavailable"})
				continue
			}
			dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			err := c2.DispatchJSONByHumanID(dctx, pool, target, map[string]string{"action": action})
			cancel()
			if err != nil {
				replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: false, Message: err.Error()})
				continue
			}
			replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: true, Message: action + " dispatched"})
			auditDashboardDispatch(r, ctx, pool, logger, c.principal.UserID, action, target)
		case "registry_read", "registry_write", "registry_delete", "pty_start", "pty_data", "pty_resize", "pty_close", "deploy_package", "install_app",
			"fs_listdir", "fs_download", "fs_upload_begin", "fs_upload_chunk", "fs_upload_finish", "fs_upload_abort",
			"net_list", "hostname_set":
			if c2 == nil || pool == nil {
				replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: false, Message: "command dispatch unavailable"})
				continue
			}
			var raw map[string]any
			if err := json.Unmarshal(data, &raw); err != nil {
				replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: false, Message: "invalid JSON command"})
				continue
			}
			delete(raw, "target_arx_id")
			dctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			err := c2.DispatchJSONByHumanID(dctx, pool, target, raw)
			cancel()
			if err != nil {
				replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: false, Message: err.Error()})
				continue
			}
			replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: true, Message: action + " dispatched"})
			auditDashboardDispatch(r, ctx, pool, logger, c.principal.UserID, action, target)
		default:
			replyJSON(c, CommandResultMsg{Type: MsgTypeCommandResult, OK: false, Message: "unknown action"})
		}
	}
}

func auditDashboardDispatch(r *http.Request, ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, userID uuid.UUID, transportAction, targetHuman string) {
	if pool == nil || userID == uuid.Nil {
		return
	}
	transportAction = strings.TrimSpace(strings.ToLower(transportAction))
	if transportAction == "" {
		return
	}
	var assetID *uuid.UUID
	var id uuid.UUID
	err := pool.QueryRow(ctx, `SELECT id FROM assets WHERE human_id = $1 LIMIT 1`, targetHuman).Scan(&id)
	if err == nil {
		assetID = &id
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) && logger != nil {
		logger.Debug("audit ws asset lookup failed", "err", err)
	}
	details := map[string]any{
		"target_arx_id":    targetHuman,
		"channel":          "dashboard_websocket",
		"transport_action": transportAction,
	}
	actx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := auth.InsertAuditRecord(actx, pool, auth.AuditRecord{
		UserID:        userID,
		Action:        "command_executed",
		ResourceType:  "device",
		ResourceID:    assetID,
		TargetAssetID: assetID,
		Details:       details,
		IPAddress:     auth.ClientIP(r),
	}); err != nil && logger != nil {
		logger.Warn("audit ws log insert failed", "err", err, "action", "command_executed")
	}
}

func replyJSON(c *dashboardClient, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case c.send <- b:
	default:
	}
}
