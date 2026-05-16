//go:build windows

package agent

import (
	"encoding/json"
	"log/slog"
)

func platformEnforceMergedPayload(logger *slog.Logger, osFamily string, raw json.RawMessage) error {
	_ = osFamily
	return handleWindowsDeclarative(logger, "custom", raw)
}
