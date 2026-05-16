package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/ARCOOON/arx-mdm/internal/agent"
	"github.com/ARCOOON/arx-mdm/internal/agent/c2"
	"github.com/ARCOOON/arx-mdm/internal/agent/cmdloop"
	"github.com/ARCOOON/arx-mdm/pkg/packagemanager"
	"github.com/ARCOOON/arx-mdm/pkg/system"

	"github.com/gorilla/websocket"
)

// Agent uplink JSON types (agent -> server -> dashboards).
const (
	agentMsgRegistryResult = "registry_result"
	agentMsgPtyOutput      = "pty_output"
	agentMsgPtyExit        = "pty_exit"
)

type agentRuntime struct {
	mu     sync.Mutex
	conn   *websocket.Conn
	logger *slog.Logger

	ptyMu   sync.Mutex
	ptySess *system.PTYSession
}

func (rt *agentRuntime) writeJSON(v any) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.conn == nil {
		return fmt.Errorf("ws: no connection")
	}
	return rt.conn.WriteJSON(v)
}

func init() {
	cmdloop.Register(runAgentCommandLoop)
}

// RunAgentCommandLoop reads server downlink commands until the connection closes or ctx ends.
func RunAgentCommandLoop(ctx context.Context, logger *slog.Logger, conn *websocket.Conn) error {
	return runAgentCommandLoop(ctx, logger, conn)
}

func runAgentCommandLoop(ctx context.Context, logger *slog.Logger, conn *websocket.Conn) error {
	rt := &agentRuntime{conn: conn, logger: logger}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if err := rt.handleDownlink(data); err != nil {
			logger.Error("agent command handling failed", "err", err)
		}
	}
}

func (rt *agentRuntime) handleDownlink(data []byte) error {
	var probe struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	action := strings.TrimSpace(strings.ToLower(probe.Action))
	if action == "" {
		return nil
	}
	switch action {
	case "shutdown":
		rt.logger.Info("executing shutdown command from server")
		if err := agent.InitiateHostShutdown(); err != nil {
			return fmt.Errorf("host shutdown: %w", err)
		}
		return nil
	case "registry_read":
		return rt.handleRegistryRead(data)
	case "registry_write":
		return rt.handleRegistryWrite(data)
	case "registry_delete":
		return rt.handleRegistryDelete(data)
	case "pty_start":
		return rt.handlePTYStart(data)
	case "pty_data":
		return rt.handlePTYData(data)
	case "pty_resize":
		return rt.handlePTYResize(data)
	case "pty_close":
		return rt.handlePTYClose()
	case "deploy_package":
		return rt.handleDeployPackage(data)
	case "fs_listdir":
		return rt.handleFsListDir(data)
	case "fs_download":
		return rt.handleFsDownload(data)
	case "fs_upload_begin":
		return rt.handleFsUploadBegin(data)
	case "fs_upload_chunk":
		return rt.handleFsUploadChunk(data)
	case "fs_upload_finish":
		return rt.handleFsUploadFinish(data)
	case "fs_upload_abort":
		return rt.handleFsUploadAbort(data)
	case "net_list":
		return rt.handleNetList(data)
	case "hostname_set":
		return rt.handleHostnameSet(data)
	case "install_app":
		go c2.RunCatalogInstall(context.Background(), rt.logger, func(v any) error { return rt.writeJSON(v) }, data)
		return nil
	case "lock":
		rt.logger.Info("executing remote lock from server")
		if err := c2.ExecuteRemoteLock(); err != nil {
			return err
		}
		return nil
	case "wipe":
		rt.logger.Warn("executing enterprise wipe from server")
		c2.ExecuteEnterpriseWipe(rt.logger)
		return nil
	case "device_command":
		return c2.HandleDownlink(context.Background(), rt.logger, func(v any) error { return rt.writeJSON(v) }, data)
	default:
		return nil
	}
}

type registryReadCmd struct {
	Action    string `json:"action"`
	RequestID string `json:"request_id"`
	KeyPath   string `json:"key_path"`
	ValueName string `json:"value_name"`
}

func (rt *agentRuntime) handleRegistryRead(data []byte) error {
	var cmd registryReadCmd
	if err := json.Unmarshal(data, &cmd); err != nil {
		return err
	}
	val, err := system.RegistryRead(cmd.KeyPath, cmd.ValueName)
	if err != nil {
		return rt.writeJSON(map[string]any{
			"type":       agentMsgRegistryResult,
			"request_id": cmd.RequestID,
			"ok":         false,
			"error":      err.Error(),
			"key_path":   cmd.KeyPath,
			"value_name": cmd.ValueName,
		})
	}
	return rt.writeJSON(map[string]any{
		"type":       agentMsgRegistryResult,
		"request_id": cmd.RequestID,
		"ok":         true,
		"key_path":   cmd.KeyPath,
		"value_name": val.ValueName,
		"value_type": val.Type,
		"data":       val.Data,
	})
}

type registryWriteCmd struct {
	Action    string `json:"action"`
	RequestID string `json:"request_id"`
	system.RegistryWriteInput
}

func (rt *agentRuntime) handleRegistryWrite(data []byte) error {
	var cmd registryWriteCmd
	if err := json.Unmarshal(data, &cmd); err != nil {
		return err
	}
	err := system.RegistryWrite(cmd.RegistryWriteInput)
	if err != nil {
		return rt.writeJSON(map[string]any{
			"type":       agentMsgRegistryResult,
			"request_id": cmd.RequestID,
			"ok":         false,
			"error":      err.Error(),
			"key_path":   cmd.KeyPath,
			"value_name": cmd.ValueName,
		})
	}
	return rt.writeJSON(map[string]any{
		"type":       agentMsgRegistryResult,
		"request_id": cmd.RequestID,
		"ok":         true,
		"key_path":   cmd.KeyPath,
		"value_name": cmd.ValueName,
		"message":    "value written",
	})
}

type registryDeleteCmd struct {
	Action    string `json:"action"`
	RequestID string `json:"request_id"`
	KeyPath   string `json:"key_path"`
	ValueName string `json:"value_name"`
	DeleteKey bool   `json:"delete_key"`
}

func (rt *agentRuntime) handleRegistryDelete(data []byte) error {
	var cmd registryDeleteCmd
	if err := json.Unmarshal(data, &cmd); err != nil {
		return err
	}
	err := system.RegistryDelete(cmd.KeyPath, cmd.ValueName, cmd.DeleteKey)
	if err != nil {
		return rt.writeJSON(map[string]any{
			"type":       agentMsgRegistryResult,
			"request_id": cmd.RequestID,
			"ok":         false,
			"error":      err.Error(),
		})
	}
	return rt.writeJSON(map[string]any{
		"type":       agentMsgRegistryResult,
		"request_id": cmd.RequestID,
		"ok":         true,
		"message":    "delete completed",
	})
}

type ptyStartCmd struct {
	Action    string   `json:"action"`
	RequestID string   `json:"request_id"`
	Cols      uint16   `json:"cols"`
	Rows      uint16   `json:"rows"`
	Argv      []string `json:"argv"`
}

func (rt *agentRuntime) handlePTYStart(data []byte) error {
	var cmd ptyStartCmd
	if err := json.Unmarshal(data, &cmd); err != nil {
		return err
	}
	rt.ptyMu.Lock()
	defer rt.ptyMu.Unlock()
	if rt.ptySess != nil {
		_ = rt.ptySess.Close()
		rt.ptySess = nil
	}

	sess, err := system.SpawnPTY(context.Background(), cmd.Cols, cmd.Rows, cmd.Argv)
	if err != nil {
		return rt.writeJSON(map[string]any{
			"type":       agentMsgPtyExit,
			"request_id": cmd.RequestID,
			"code":       1,
			"error":      err.Error(),
		})
	}
	rt.ptySess = sess

	go rt.ptyReadLoop(sess, cmd.RequestID)
	return rt.writeJSON(map[string]any{
		"type":       MsgTypePtyStarted,
		"request_id": cmd.RequestID,
		"ok":         true,
	})
}

func (rt *agentRuntime) ptyReadLoop(sess *system.PTYSession, requestID string) {
	buf := make([]byte, 32*1024)
	for {
		n, err := sess.Stdout.Read(buf)
		if n > 0 {
			out := base64.StdEncoding.EncodeToString(buf[:n])
			if werr := rt.writeJSON(map[string]any{
				"type":       agentMsgPtyOutput,
				"request_id": requestID,
				"data_b64":   out,
			}); werr != nil {
				rt.logger.Debug("pty uplink write failed", "err", werr)
				return
			}
		}
		if err != nil {
			_ = rt.writeJSON(map[string]any{
				"type":       agentMsgPtyExit,
				"request_id": requestID,
				"code":       0,
			})
			return
		}
	}
}

type ptyDataCmd struct {
	Action  string `json:"action"`
	DataB64 string `json:"data_b64"`
}

func (rt *agentRuntime) handlePTYData(data []byte) error {
	var cmd ptyDataCmd
	if err := json.Unmarshal(data, &cmd); err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(cmd.DataB64)
	if err != nil {
		return err
	}
	rt.ptyMu.Lock()
	sess := rt.ptySess
	rt.ptyMu.Unlock()
	if sess == nil {
		return nil
	}
	_, err = sess.Stdin.Write(raw)
	return err
}

type ptyResizeCmd struct {
	Action string `json:"action"`
	Cols   uint16 `json:"cols"`
	Rows   uint16 `json:"rows"`
}

func (rt *agentRuntime) handlePTYResize(data []byte) error {
	var cmd ptyResizeCmd
	if err := json.Unmarshal(data, &cmd); err != nil {
		return err
	}
	rt.ptyMu.Lock()
	sess := rt.ptySess
	rt.ptyMu.Unlock()
	if sess == nil || sess.Resize == nil {
		return nil
	}
	return sess.Resize(cmd.Cols, cmd.Rows)
}

func (rt *agentRuntime) handlePTYClose() error {
	rt.ptyMu.Lock()
	defer rt.ptyMu.Unlock()
	if rt.ptySess != nil {
		_ = rt.ptySess.Close()
		rt.ptySess = nil
	}
	return nil
}

type deployPackageCmd struct {
	Action       string `json:"action"`
	DeploymentID string `json:"deployment_id"`
	RequestID    string `json:"request_id"`
	Operation    string `json:"operation"`
	PackageType  string `json:"package_type"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	InstallCmd   string `json:"install_cmd"`
}

func (rt *agentRuntime) handleDeployPackage(data []byte) error {
	var cmd deployPackageCmd
	if err := json.Unmarshal(data, &cmd); err != nil {
		return err
	}
	op := strings.TrimSpace(strings.ToLower(cmd.Operation))
	if op == "" {
		op = "install"
	}
	go rt.runDeployPackage(cmd, op)
	return nil
}

func (rt *agentRuntime) runDeployPackage(cmd deployPackageCmd, op string) {
	ctx := context.Background()
	var err error
	switch op {
	case "install":
		err = packagemanager.Install(ctx, cmd.PackageType, cmd.Name, cmd.Version, cmd.InstallCmd)
	case "uninstall":
		err = packagemanager.Uninstall(ctx, cmd.PackageType, cmd.Name, cmd.Version, cmd.InstallCmd)
	default:
		err = fmt.Errorf("unsupported operation %q", op)
	}
	ok := err == nil
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	_ = rt.writeJSON(map[string]any{
		"type":          agentMsgPackageResult,
		"deployment_id": strings.TrimSpace(cmd.DeploymentID),
		"request_id":    strings.TrimSpace(cmd.RequestID),
		"ok":            ok,
		"error":         errStr,
		"operation":     op,
		"package_type":  cmd.PackageType,
	})
}
