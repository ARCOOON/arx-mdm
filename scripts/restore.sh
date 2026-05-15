#!/usr/bin/env bash
# Restore ARX MDM from a tarball produced by scripts/backup.sh (PostgreSQL + optional embedded PKI).
# Stops dependent Docker Compose services, restores the database, replaces PKI files on the host, then starts the stack.
#
# Prerequisites: Docker Compose v2, same compose file / volumes as backup time.
#
# Environment (optional):
#   ARX_COMPOSE_DIR       — directory containing docker-compose.yml (default: repo root)
#   COMPOSE_FILE          — explicit compose file path (default: ${ARX_COMPOSE_DIR}/docker-compose.yml)
#   ARX_POSTGRES_SERVICE  — Postgres service name (default: postgres)
#   ARX_SERVER_SERVICE    — MDM server service name (default: arx-server)
#   ARX_TUNNEL_SERVICE    — edge tunnel service (default: cloudflared); stopped before server, started after
#   ARX_PKI_STORAGE_PATH  — host path where pki/ contents must be written when the backup includes PKI
#
# Usage:
#   sudo ARX_PKI_STORAGE_PATH=/var/lib/arx-pki ./scripts/restore.sh /path/to/arx-mdm-backup-....tar.gz
#
set -euo pipefail

log() {
	echo "[arx-restore] $*" >&2
}

readonly script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly default_root="$(cd "${script_dir}/.." && pwd)"
readonly arx_compose_dir="${ARX_COMPOSE_DIR:-${default_root}}"
readonly compose_file="${COMPOSE_FILE:-${arx_compose_dir}/docker-compose.yml}"
readonly pg_service="${ARX_POSTGRES_SERVICE:-postgres}"
readonly server_service="${ARX_SERVER_SERVICE:-arx-server}"
readonly tunnel_service="${ARX_TUNNEL_SERVICE:-cloudflared}"

compose() {
	docker compose -f "${compose_file}" "$@"
}

if ! command -v docker >/dev/null 2>&1; then
	log "error: docker not found in PATH"
	exit 1
fi

if [[ "${#}" -lt 1 ]] || [[ "${1}" == "-h" ]] || [[ "${1}" == "--help" ]]; then
	log "usage: $0 /path/to/arx-mdm-backup-*.tar.gz"
	exit 1
fi

if command -v realpath >/dev/null 2>&1; then
	readonly tarball="$(realpath "${1}")"
else
	readonly tarball="$(readlink -f "${1}")"
fi

if [[ ! -f "${compose_file}" ]]; then
	log "error: compose file not found: ${compose_file}"
	exit 1
fi

if [[ ! -f "${tarball}" ]]; then
	log "error: backup file not found: ${tarball}"
	exit 1
fi

readonly work="$(mktemp -d "${TMPDIR:-/tmp}/arx-restore.XXXXXX")"

cleanup() {
	rm -rf "${work}"
}
trap cleanup EXIT

log "extracting ${tarball}"
tar -xzf "${tarball}" -C "${work}"

if [[ ! -f "${work}/postgres.dump" ]]; then
	log "error: backup missing postgres.dump"
	exit 1
fi

if [[ ! -f "${work}/MANIFEST.txt" ]]; then
	log "warning: MANIFEST.txt missing (older backup?); continuing"
fi

if ! compose ps -q "${pg_service}" 2>/dev/null | grep -q .; then
	log "error: compose service ${pg_service} is not running (start postgres before restore)"
	exit 1
fi

stop_stack() {
	log "stopping ${tunnel_service} (if defined)"
	if compose config --services 2>/dev/null | grep -qx "${tunnel_service}"; then
		compose stop "${tunnel_service}" 2>/dev/null || true
	fi

	log "stopping ${server_service}"
	compose stop "${server_service}" 2>/dev/null || true
}

start_stack() {
	log "starting ${server_service}"
	compose up -d "${server_service}"

	if compose config --services 2>/dev/null | grep -qx "${tunnel_service}"; then
		log "starting ${tunnel_service}"
		compose up -d "${tunnel_service}"
	fi
}

restore_pki() {
	local src=""
	if [[ -d "${work}/pki" ]] && [[ -n "$(ls -A "${work}/pki" 2>/dev/null)" ]]; then
		src="${work}/pki"
	elif [[ -d "${work}/step-ca" ]] && [[ -n "$(ls -A "${work}/step-ca" 2>/dev/null)" ]]; then
		log "warning: backup uses legacy step-ca/ layout; migrating files into ARX_PKI_STORAGE_PATH"
		src="${work}/step-ca"
	fi

	if [[ -z "${src}" ]]; then
		log "warning: backup has no PKI directory; skipping filesystem PKI restore"
		return 0
	fi

	if [[ -z "${ARX_PKI_STORAGE_PATH:-}" ]]; then
		log "error: backup includes PKI data; set ARX_PKI_STORAGE_PATH to the host directory that should receive CA material."
		exit 1
	fi

	if [[ ! -d "${ARX_PKI_STORAGE_PATH}" ]]; then
		log "creating PKI directory ${ARX_PKI_STORAGE_PATH}"
		mkdir -p "${ARX_PKI_STORAGE_PATH}"
	fi

	log "replacing PKI contents under ${ARX_PKI_STORAGE_PATH}"
	find "${ARX_PKI_STORAGE_PATH}" -mindepth 1 -maxdepth 1 -exec rm -rf {} +
	cp -a "${src}/." "${ARX_PKI_STORAGE_PATH}/"
	chmod -R o-rwx "${ARX_PKI_STORAGE_PATH}" 2>/dev/null || true
}

restore_db() {
	log "recreating database arx on ${pg_service}"
	compose exec -T "${pg_service}" psql -U arx -d postgres -v ON_ERROR_STOP=1 \
		-c 'DROP DATABASE IF EXISTS arx WITH (FORCE);' \
		-c 'CREATE DATABASE arx OWNER arx;'

	log "restoring postgres.dump (this may take a while)"
	set +e
	compose exec -T "${pg_service}" pg_restore -U arx -d arx --no-owner --no-acl <"${work}/postgres.dump"
	local prc=$?
	set -e
	if [[ "${prc}" -gt 1 ]]; then
		log "error: pg_restore failed (exit ${prc})"
		exit 1
	fi
	if [[ "${prc}" -eq 1 ]]; then
		log "warning: pg_restore reported non-fatal issues; verify the database"
	fi
}

stop_stack
restore_db
restore_pki
start_stack

log "restore complete; verify with: docker compose -f ${compose_file} ps"
exit 0
