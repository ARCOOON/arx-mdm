#!/usr/bin/env bash
# Ubuntu host: install Docker Engine + Compose plugin (if missing), write a secure .env, start the stack.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run as root: sudo $0" >&2
  exit 1
fi

export DEBIAN_FRONTEND=noninteractive
if ! command -v docker >/dev/null 2>&1; then
  apt-get update -y
  apt-get install -y docker.io docker-compose-plugin
  systemctl enable --now docker
fi

rand_b64() { openssl rand -base64 32 | tr -d '\n' | tr '/+' 'Aa'; }
rand_hex() { openssl rand -hex 24; }

POSTGRES_PASSWORD="$(rand_hex)"
ARX_JWT_SECRET="$(rand_b64)"
ARX_BOOTSTRAP_ADMIN_PASSWORD="$(rand_b64)"
ARX_BOOTSTRAP_ADMIN_USERNAME="${ARX_BOOTSTRAP_ADMIN_USERNAME:-admin}"

PRIMARY_IP="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
if [[ -z "${PRIMARY_IP:-}" ]]; then
  PRIMARY_IP="127.0.0.1"
fi
ARX_PUBLISH_PORT="${ARX_PUBLISH_PORT:-8080}"
ORIGIN_HTTP="http://${PRIMARY_IP}:${ARX_PUBLISH_PORT}"
ORIGIN_LOCAL="http://127.0.0.1:${ARX_PUBLISH_PORT},http://localhost:${ARX_PUBLISH_PORT}"
ARX_DASHBOARD_ORIGINS="${ORIGIN_HTTP},${ORIGIN_LOCAL}"

ENV_FILE="$ROOT/.env"
umask 077
cat >"$ENV_FILE" <<EOF
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
ARX_JWT_SECRET=${ARX_JWT_SECRET}
ARX_BOOTSTRAP_ADMIN_USERNAME=${ARX_BOOTSTRAP_ADMIN_USERNAME}
ARX_BOOTSTRAP_ADMIN_PASSWORD=${ARX_BOOTSTRAP_ADMIN_PASSWORD}
ARX_DASHBOARD_ORIGINS=${ARX_DASHBOARD_ORIGINS}
ARX_PUBLISH_PORT=${ARX_PUBLISH_PORT}
EOF
chmod 600 "$ENV_FILE"

echo "Wrote $ENV_FILE (mode 600). Dashboard admin: ${ARX_BOOTSTRAP_ADMIN_USERNAME}"

docker compose --project-directory "$ROOT" up -d --build

echo "Server listening on port ${ARX_PUBLISH_PORT} (host ${PRIMARY_IP})."
