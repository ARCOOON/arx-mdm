//go:build linux

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
	base := strings.ToLower(filepath.Base(localPath))
	switch {
	case strings.HasSuffix(base, ".deb"):
		cmd := exec.CommandContext(runCtx, "dpkg", "-i", localPath)
		return runSilentExitCode(cmd)
	case strings.HasSuffix(base, ".rpm"):
		cmd := exec.CommandContext(runCtx, "rpm", "-U", "--replacepkgs", localPath)
		return runSilentExitCode(cmd)
	}
	if err := os.Chmod(localPath, 0o700); err != nil && !strings.HasSuffix(base, ".msi") && !strings.HasSuffix(base, ".exe") && !strings.HasSuffix(base, ".apk") {
		return -1, fmt.Errorf("chmod: %w", err)
	}
	args := shellSplitArgs(installArgs)
	cmd := exec.CommandContext(runCtx, localPath, args...)
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
