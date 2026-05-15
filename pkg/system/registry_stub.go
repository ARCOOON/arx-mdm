//go:build !windows

package system

import "errors"

var ErrRegistryUnsupported = errors.New("system: registry operations are only supported on Windows")

// RegistryRead is a stub on non-Windows builds.
func RegistryRead(_, _ string) (RegistryValue, error) {
	return RegistryValue{}, ErrRegistryUnsupported
}

// RegistryWrite is a stub on non-Windows builds.
func RegistryWrite(_ RegistryWriteInput) error {
	return ErrRegistryUnsupported
}

// RegistryDelete is a stub on non-Windows builds.
func RegistryDelete(_, _ string, _ bool) error {
	return ErrRegistryUnsupported
}
