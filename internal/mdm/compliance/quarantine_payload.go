package compliance

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

// QuarantineWire is JSON embedded in device_commands.payload for command_type quarantine.
type QuarantineWire struct {
	Enabled    bool     `json:"enabled"`
	AllowHosts []string `json:"allow_hosts"`
	AllowPorts []uint16 `json:"allow_ports"`
}

// BuildQuarantinePayloadJSON builds JSON from ARX_MDM_PUBLIC_HOST (comma-separated)
// and ARX_MDM_PUBLIC_PORT (single port, default 443).
func BuildQuarantinePayloadJSON(enabled bool) string {
	hostRaw := strings.TrimSpace(os.Getenv("ARX_MDM_PUBLIC_HOST"))
	var hosts []string
	for _, p := range strings.Split(hostRaw, ",") {
		h := strings.TrimSpace(p)
		if h != "" {
			hosts = append(hosts, h)
		}
	}
	portStr := strings.TrimSpace(os.Getenv("ARX_MDM_PUBLIC_PORT"))
	var ports []uint16
	if portStr == "" {
		ports = []uint16{443}
	} else if n, err := strconv.ParseUint(portStr, 10, 16); err == nil && n > 0 {
		ports = []uint16{uint16(n)}
	} else {
		ports = []uint16{443}
	}
	w := QuarantineWire{Enabled: enabled, AllowHosts: hosts, AllowPorts: ports}
	b, _ := json.Marshal(w)
	return string(b)
}
