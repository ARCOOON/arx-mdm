#!/usr/bin/env bash
# ARX MDM agent removal (Linux). Run as root. Destructive: stops the service,
# removes the systemd unit, binary, configuration, and certificate stores.
set -euo pipefail

log() {
	echo "[arx-agent-uninstall] $*" >&2
}

if [[ "$(id -u)" -ne 0 ]]; then
	log "error: root privileges required (use sudo)"
	exit 1
fi

readonly unit_path="/etc/systemd/system/arx-agent.service"
readonly binary_path="/usr/local/bin/arx-agent"
readonly etc_dir="/etc/arx-agent"
readonly var_dir="/var/lib/arx-agent"

log "stopping arx-agent (ignore errors if unit is missing)"
systemctl stop arx-agent.service 2>/dev/null || true

log "disabling arx-agent (ignore errors if unit is missing)"
systemctl disable arx-agent.service 2>/dev/null || true

if [[ -f "${unit_path}" ]]; then
	log "removing systemd unit ${unit_path}"
	rm -f "${unit_path}"
else
	log "systemd unit not present: ${unit_path}"
fi

log "reloading systemd daemon"
systemctl daemon-reload

if [[ -e "${binary_path}" ]]; then
	log "removing agent binary ${binary_path}"
	rm -f "${binary_path}"
else
	log "agent binary not present: ${binary_path}"
fi

if [[ -d "${etc_dir}" ]]; then
	log "removing configuration directory ${etc_dir}"
	rm -rf "${etc_dir}"
else
	log "configuration directory not present: ${etc_dir}"
fi

if [[ -d "${var_dir}" ]]; then
	log "removing state directory ${var_dir}"
	rm -rf "${var_dir}"
else
	log "state directory not present: ${var_dir}"
fi

log "uninstall finished cleanly"
exit 0
