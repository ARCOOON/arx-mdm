package c2

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var serviceNameRe = regexp.MustCompile(`^[a-zA-Z0-9_.@-]{1,96}$`)

// BuildRestartServiceScript emits an OS-aware script body executed by executeScript().
func BuildRestartServiceScript(serviceName string) (string, error) {
	serviceName = strings.TrimSpace(serviceName)
	if !serviceNameRe.MatchString(serviceName) {
		return "", fmt.Errorf("invalid restart_service service name")
	}
	switch runtime.GOOS {
	case "windows":
		esc := strings.ReplaceAll(serviceName, "'", "''")
		return fmt.Sprintf(
			"$ErrorActionPreference = 'Stop'\n"+
				"Restart-Service -Name '%s' -Force\n"+
				"Write-Output \"restart_service completed for '%s'\"", esc, esc,
		), nil
	case "linux":
		safe := strings.ReplaceAll(serviceName, "'", `'\''`)
		return fmt.Sprintf(
			"set -euo pipefail\n"+
				"systemctl restart '%s'\n"+
				"printf 'restart_service completed for %s\\n'\n",
			safe,
			safe,
		), nil
	default:
		return "", fmt.Errorf("restart_service unsupported on %s", runtime.GOOS)
	}
}

// BuildPushConfigScript writes compact JSON payload to disk from a UTF-8 base64 transcript.
func BuildPushConfigScript(jsonPayload string) (string, error) {
	jsonPayload = strings.TrimSpace(jsonPayload)
	var probe any
	if err := json.Unmarshal([]byte(jsonPayload), &probe); err != nil {
		return "", fmt.Errorf("push_config payload must be valid JSON")
	}
	compact, err := json.Marshal(probe)
	if err != nil {
		return "", err
	}
	b64 := base64.StdEncoding.EncodeToString(compact)
	switch runtime.GOOS {
	case "windows":
		return fmt.Sprintf(
			"$ErrorActionPreference = 'Stop'\n"+
				"$p = Join-Path $env:ProgramData 'arx-mdm\\managed-config.json'\n"+
				"$dir = Split-Path -LiteralPath $p\n"+
				"New-Item -ItemType Directory -Force -Path $dir | Out-Null\n"+
				"$bytes = [Convert]::FromBase64String('%s')\n"+
				"[IO.File]::WriteAllBytes($p, $bytes)\n"+
				"Write-Output \"push_config wrote $($bytes.Length) bytes to $p\"\n",
			b64,
		), nil
	case "linux":
		target := filepath.Clean("/var/lib/arx-mdm/push-config.json")
		dir := filepath.Dir(target)
		return fmt.Sprintf(
			"set -euo pipefail\n"+
				"install -d -m 0750 '%s'\n"+
				"tmp=\"$(mktemp)\"\n"+
				"base64 -d <<'ARB64' > \"$tmp\"\n%s\nARB64\n"+
				"install -m 0640 \"$tmp\" '%s'\n"+
				"rm -f \"$tmp\"\n"+
				"printf 'push_config completed for %s\\n'\n",
			dir,
			chunkBase64Lines(b64),
			target,
			target,
		), nil
	default:
		return "", fmt.Errorf("push_config unsupported on %s", runtime.GOOS)
	}
}

func chunkBase64Lines(b64 string) string {
	const step = 120
	if len(b64) <= step {
		return b64
	}
	var b strings.Builder
	for i := 0; i < len(b64); i += step {
		j := i + step
		if j > len(b64) {
			j = len(b64)
		}
		b.WriteString(b64[i:j])
		if j < len(b64) {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
