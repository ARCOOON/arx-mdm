//go:build windows

package c2

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func shellSplitArgs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

func executeInstallerNative(ctx context.Context, localPath string, installArgs string) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()

	localPath = filepath.Clean(localPath)
	lower := strings.ToLower(localPath)
	extra := shellSplitArgs(installArgs)

	if strings.HasSuffix(lower, ".msi") {
		win := filepath.Clean(strings.TrimSpace(os.Getenv("SYSTEMROOT")))
		if win == "." || win == "" {
			win = `C:\Windows`
		}
		msiexe := filepath.Join(win, "System32", "msiexec.exe")
		args := append([]string{"/i", localPath, "/qn", "/norestart"}, extra...)
		cmd := exec.CommandContext(runCtx, msiexe, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		return runSilentExitCode(cmd)
	}

	cmd := exec.CommandContext(runCtx, localPath, extra...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return runSilentExitCode(cmd)
}

func runSilentExitCode(cmd *exec.Cmd) (int, error) {
	buf := bytes.Buffer{}
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code := ee.ExitCode()
		if code == -1 && out != "" {
			return code, fmt.Errorf("%s", out)
		}
		if out != "" {
			return code, fmt.Errorf("%s (exit=%d)", out, code)
		}
		return code, ee
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}
