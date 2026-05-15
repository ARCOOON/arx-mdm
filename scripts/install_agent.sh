#!/usr/bin/env bash
# Install the ARX MDM agent binary and register a native systemd service (arx-agent).
# Requires root.
#
# Required: ARX_SERVER_URL (MDM base URL, e.g. https://mdm.example:8443)
# Optional: ARX_CERT_DIR (default /var/lib/arx-agent/certs), ARX_INTERVAL (default 60s)
#
# Usage:
#   sudo ARX_SERVER_URL='https://mdm.example:8443' ./scripts/install_agent.sh [/path/to/arx-agent-binary]
#
set -euo pipefail

log() {
	echo "[arx-agent-install] $*" >&2
}

if [[ "$(id -u)" -ne 0 ]]; then
	log "error: root privileges required (use sudo)"
	exit 1
fi

if [[ -z "${ARX_SERVER_URL:-}" ]]; then
	log "error: set ARX_SERVER_URL to your MDM base URL"
	exit 1
fi

readonly binary_src="${1:-./arx-agent}"
readonly install_path="/usr/local/bin/arx-agent"
readonly unit_path="/etc/systemd/system/arx-agent.service"
readonly env_path="/etc/arx-agent/environment"
readonly cert_root="${ARX_CERT_DIR:-/var/lib/arx-agent/certs}"
readonly interval="${ARX_INTERVAL:-60s}"

if [[ ! -f "${binary_src}" ]]; then
	log "error: agent binary not found: ${binary_src}"
	exit 1
fi

log "installing binary to ${install_path}"
install -m 0755 -T "${binary_src}" "${install_path}"

mkdir -p /etc/arx-agent
mkdir -p "${cert_root}"
chmod 0755 /etc/arx-agent
chmod 0750 "$(dirname "${cert_root}")"
chmod 0700 "${cert_root}"

umask 077
{
	echo "ARX_SERVER_URL=${ARX_SERVER_URL}"
	echo "ARX_CERT_DIR=${cert_root}"
	echo "ARX_INTERVAL=${interval}"
} >"${env_path}"
chmod 0640 "${env_path}"

log "writing systemd unit ${unit_path}"
cat >"${unit_path}" <<'UNIT'
[Unit]
Description=ARX MDM Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=-/etc/arx-agent/environment
ExecStart=/usr/local/bin/arx-agent run -server ${ARX_SERVER_URL} -certdir ${ARX_CERT_DIR} -interval ${ARX_INTERVAL}
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

log "reloading systemd and enabling arx-agent"
systemctl daemon-reload
systemctl enable arx-agent.service
systemctl restart arx-agent.service

log "install complete; check status with: systemctl status arx-agent.service"
exit 0
