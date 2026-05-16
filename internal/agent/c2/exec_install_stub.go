//go:build !(linux || windows)

package c2

import (
	"context"
	"fmt"
)

func executeInstallerNative(ctx context.Context, localPath string, installArgs string) (int, error) {
	return -1, fmt.Errorf("silent catalog install unsupported on this platform")
}
