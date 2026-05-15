#!/usr/bin/env sh
# ARX MDM agent — zero-touch install for Linux (systemd).
# Required env: ARX_SERVER_URL (e.g. https://mdm.example.com), ARX_ENROLL_TOKEN (presentation secret).
# Optional: ARX_INSECURE_TLS=1 to skip TLS verification when downloading the binary (lab only).
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "re-run as root: sudo env ARX_SERVER_URL=... ARX_ENROLL_TOKEN=... bash" >&2
  exit 1
fi

ARX_SERVER_URL="${ARX_SERVER_URL:-}"
ARX_ENROLL_TOKEN="${ARX_ENROLL_TOKEN:-}"
ARX_INSECURE_TLS="${ARX_INSECURE_TLS:-0}"

if [ -z "$ARX_SERVER_URL" ] || [ -z "$ARX_ENROLL_TOKEN" ]; then
  echo "ARX_SERVER_URL and ARX_ENROLL_TOKEN must be set." >&2
  exit 1
fi

case "$ARX_SERVER_URL" in
  http://*|https://*) ;;
  *) echo "ARX_SERVER_URL must start with http:// or https://" >&2; exit 1 ;;
esac

BIN_URL="${ARX_SERVER_URL%/}/v1/install/bin/linux"
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT

CURL_OPTS="-fsSL"
if [ "$ARX_INSECURE_TLS" = "1" ]; then
  CURL_OPTS="$CURL_OPTS -k"
fi
curl $CURL_OPTS "$BIN_URL" -o "$TMP"
install -m 0755 "$TMP" /usr/local/bin/arx-agent

install -d /var/lib/arx-agent/certs
if ! /usr/local/bin/arx-agent enroll -server "$ARX_SERVER_URL" -token "$ARX_ENROLL_TOKEN" -certdir /var/lib/arx-agent/certs >/dev/null 2>&1; then
  echo "arx-agent enroll failed" >&2
  exit 1
fi

cat >/etc/systemd/system/arx-agent.service <<EOF
[Unit]
Description=ARX MDM Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/arx-agent run -server $ARX_SERVER_URL -certdir /var/lib/arx-agent/certs
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now arx-agent.service
