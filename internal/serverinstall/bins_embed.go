//go:build embedbins

package serverinstall

import _ "embed"

//go:embed arx-agent-linux
var embeddedAgentLinux []byte

//go:embed arx-agent-windows.exe
var embeddedAgentWindows []byte

func agentLinuxBytes() []byte   { return embeddedAgentLinux }
func agentWindowsBytes() []byte { return embeddedAgentWindows }
