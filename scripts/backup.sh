#!/usr/bin/env bash
# Full ARX MDM backup: PostgreSQL (pg_dump custom format) + embedded PKI directory (optional).
# Intended for Ubuntu hosts running Docker Compose (see repository docker-compose.yml).
#
# Prerequisites: Docker Compose v2 (`docker compose`), access to the Docker socket.
#
# Environment (optional):
#   ARX_COMPOSE_DIR      — directory containing docker-compose.yml (default: repo root)
#   COMPOSE_FILE         — explicit compose file path (default: ${ARX_COMPOSE_DIR}/docker-compose.yml)
#   ARX_BACKUP_DIR       — where tarballs are written (default: ${ARX_COMPOSE_DIR}/backups)
#   ARX_BACKUP_RETENTION_DAYS — delete matching tarballs older than this many days (default: 14)
#   ARX_POSTGRES_SERVICE — compose service name for Postgres (default: postgres)
#   ARX_PKI_STORAGE_PATH — host path to embedded PKI directory (root/intermediate PEMs). If unset,
#                          falls back to ${ARX_COMPOSE_DIR}/certs when that directory is non-empty.
#
# Usage:
#   ./scripts/backup.sh
#   ARX_PKI_STORAGE_PATH=/var/lib/arx-pki ARX_BACKUP_DIR=/var/backups/arx-mdm ./scripts/backup.sh
#
set -euo pipefail

log() {
	echo "[arx-backup] $*" >&2
}

readonly script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly default_root="$(cd "${script_dir}/.." && pwd)"
readonly arx_compose_dir="${ARX_COMPOSE_DIR:-${default_root}}"
readonly compose_file="${COMPOSE_FILE:-${arx_compose_dir}/docker-compose.yml}"
readonly backup_dir="${ARX_BACKUP_DIR:-${arx_compose_dir}/backups}"
readonly retention_days="${ARX_BACKUP_RETENTION_DAYS:-14}"
readonly pg_service="${ARX_POSTGRES_SERVICE:-postgres}"
readonly ts="$(date -u +%Y%m%dT%H%M%SZ)"
readonly out_name="arx-mdm-backup-${ts}.tar.gz"
readonly work="$(mktemp -d "${TMPDIR:-/tmp}/arx-backup.XXXXXX")"

cleanup() {
	rm -rf "${work}"
}
trap cleanup EXIT

compose() {
	docker compose -f "${compose_file}" "$@"
}

if ! command -v docker >/dev/null 2>&1; then
	log "error: docker not found in PATH"
	exit 1
fi

if [[ ! -f "${compose_file}" ]]; then
	log "error: compose file not found: ${compose_file}"
	exit 1
fi

if ! compose ps --status running -q "${pg_service}" | grep -q .; then
	log "error: compose service ${pg_service} is not running (start the stack before backup)"
	exit 1
fi

mkdir -p "${backup_dir}"
chmod 0750 "${backup_dir}" 2>/dev/null || true

log "dumping PostgreSQL (${pg_service}) to custom-format archive"
if ! compose exec -T "${pg_service}" pg_dump -U arx -d arx -Fc >"${work}/postgres.dump"; then
	log "error: pg_dump failed"
	exit 1
fi

pki_source=""
if [[ -n "${ARX_PKI_STORAGE_PATH:-}" ]]; then
	if [[ ! -d "${ARX_PKI_STORAGE_PATH}" ]]; then
		log "error: ARX_PKI_STORAGE_PATH is not a directory: ${ARX_PKI_STORAGE_PATH}"
		exit 1
	fi
	pki_source="${ARX_PKI_STORAGE_PATH}"
elif [[ -d "${arx_compose_dir}/certs" ]] && [[ -n "$(ls -A "${arx_compose_dir}/certs" 2>/dev/null)" ]]; then
	pki_source="${arx_compose_dir}/certs"
fi

pki_archived=false
if [[ -n "${pki_source}" ]]; then
	mkdir -p "${work}/pki"
	log "archiving embedded PKI from ${pki_source}"
	cp -a "${pki_source}/." "${work}/pki/"
	pki_archived=true
else
	log "warning: no PKI directory found (set ARX_PKI_STORAGE_PATH or populate ${arx_compose_dir}/certs); tarball will omit pki/"
fi

{
	echo "arx-mdm-backup-version=2"
	echo "created_utc=${ts}"
	echo "postgres_service=${pg_service}"
	echo "pki_archived=${pki_archived}"
} >"${work}/MANIFEST.txt"

log "packaging ${out_name}"
if [[ "${pki_archived}" == true ]]; then
	tar -C "${work}" -czf "${backup_dir}/${out_name}" MANIFEST.txt postgres.dump pki
else
	tar -C "${work}" -czf "${backup_dir}/${out_name}" MANIFEST.txt postgres.dump
fi
chmod 0600 "${backup_dir}/${out_name}" 2>/dev/null || true

log "pruning backups in ${backup_dir} older than ${retention_days} days (name pattern arx-mdm-backup-*.tar.gz)"
find "${backup_dir}" -maxdepth 1 -type f -name 'arx-mdm-backup-*.tar.gz' -mtime "+${retention_days}" -print -delete || true

log "done: ${backup_dir}/${out_name}"
exit 0
