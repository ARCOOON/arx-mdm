#!/usr/bin/env bash
# Install the ARX MDM pre-commit hook (CRLF → LF for staged text files).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOOK_DIR="${ROOT}/.git/hooks"
HOOK_FILE="${HOOK_DIR}/pre-commit"

if [[ ! -d "${ROOT}/.git" ]]; then
  echo "error: ${ROOT}/.git not found — run 'git init' first" >&2
  exit 1
fi

mkdir -p "${HOOK_DIR}"

cat > "${HOOK_FILE}" << 'HOOK_EOF'
#!/bin/sh
# Normalize CRLF to LF for staged text files before each commit.
set -e

git diff --cached --name-only --diff-filter=ACM | while IFS= read -r f; do
  case "${f}" in
    *.go|*.tsx|*.sh|*.ps1) ;;
    *) continue ;;
  esac
  [ -f "${f}" ] || continue
  if grep -q "$(printf '\r')" "${f}" 2>/dev/null; then
    tmp="${f}.pre-commit-lf.$$"
    tr -d '\r' < "${f}" > "${tmp}"
    mv "${tmp}" "${f}"
    git add -- "${f}"
  fi
done

exit 0
HOOK_EOF

chmod +x "${HOOK_FILE}"
echo "Installed pre-commit hook: ${HOOK_FILE}"
