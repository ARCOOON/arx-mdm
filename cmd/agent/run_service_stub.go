//go:build !windows

package main

import "log/slog"

func tryRunWindowsAgentService(*slog.Logger) (bool, error) {
	return false, nil
}
