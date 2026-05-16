//go:build android

package c2

func platformApplyQuarantine(enabled bool, hosts []string, ports []uint16) (string, error) {
	_ = hosts
	_ = ports
	invokeAndroidQuarantine(enabled)
	if enabled {
		return "android application isolation enabled (non-system packages suspended)", nil
	}
	return "android application isolation cleared", nil
}
