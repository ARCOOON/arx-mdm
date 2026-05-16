//go:build android

package agentbind

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ARCOOON/arx-mdm/internal/agent/c2"
	"github.com/ARCOOON/arx-mdm/internal/agent/cmdloop"
	"github.com/gorilla/websocket"
)

func init() {
	cmdloop.Register(androidC2ReadLoop)
}

type androidWSWriter struct {
	mu  sync.Mutex
	c   *websocket.Conn
	log *slog.Logger
}

func (w *androidWSWriter) writeJSON(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.c == nil {
		return nil
	}
	_ = w.c.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return w.c.WriteJSON(v)
}

// androidC2ReadLoop processes server downlinks; device_command is executed via the shared c2 dispatcher.
func androidC2ReadLoop(ctx context.Context, logger *slog.Logger, conn *websocket.Conn) error {
	rt := &androidWSWriter{c: conn, log: logger}
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
		var probe struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(data, &probe); err != nil {
			continue
		}
		action := strings.ToLower(strings.TrimSpace(probe.Action))
		if action == "device_command" {
			if err := c2.HandleDownlink(context.Background(), logger, rt.writeJSON, data); err != nil && logger != nil {
				logger.Error("android c2 device_command", "err", err)
			}
			continue
		}
		switch action {
		case "lock":
			DispatchAndroidLock()
		case "wipe":
			DispatchAndroidWipe()
		}
	}
}
