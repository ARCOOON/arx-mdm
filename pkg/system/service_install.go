package system

import "time"

// AgentServiceName is the Windows SCM name and matches the Linux systemd unit (arx-agent.service).
const AgentServiceName = "arx-agent"

// WindowsAgentInstallOptions configures native registration of the Windows agent service.
type WindowsAgentInstallOptions struct {
	ServerURL string
	CertDir   string
	// Interval is the telemetry heartbeat interval. Zero means omit -interval from the service command line.
	Interval time.Duration
}
