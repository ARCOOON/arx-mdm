//go:build !(unix || windows)

package system

import (
	"context"
	"errors"
)

func spawnPTY(_ context.Context, _, _ uint16, _ []string) (*PTYSession, error) {
	return nil, errors.New("system: PTY is not supported on this platform")
}
