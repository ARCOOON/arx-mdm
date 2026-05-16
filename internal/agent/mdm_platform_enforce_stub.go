//go:build !windows && !(linux && !android)

package agent

import (
	"encoding/json"
	"log/slog"
)

func platformEnforceMergedPayload(logger *slog.Logger, osFamily string, raw json.RawMessage) error {
	if logger != nil && len(raw) > 2 {
		logger.Debug("mdm merged payload ignored on this platform", "os_family", osFamily)
	}
	return nil
}
