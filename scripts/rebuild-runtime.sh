#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

START_STACK=0
CHECK_HEALTH=0

usage() {
  cat <<'EOF'
Usage:
  scripts/rebuild-runtime.sh [--up] [--health]

Rebuilds the local runtime pieces for development:
  - bin/agent
  - bin/gateway
  - bin/backend
  - Docker images for backend, gateway, and dashboard

Options:
  --up      Start the compose stack after rebuilding
  --health  Run backend/gateway health checks after rebuilding
  -h, --help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --up)
      START_STACK=1
      shift
      ;;
    --health)
      CHECK_HEALTH=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ -d "${ROOT_DIR}/.tools/bin" ]]; then
  export PATH="${ROOT_DIR}/.tools/bin:${PATH}"
fi
export GOCACHE="${GOCACHE:-${ROOT_DIR}/.gocache}"
export GOMODCACHE="${GOMODCACHE:-${ROOT_DIR}/.gomodcache}"
export GOFLAGS="${GOFLAGS:--buildvcs=false}"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

need_cmd go
need_cmd docker

mkdir -p "${ROOT_DIR}/bin"

echo "==> Building local agent binary..."
go build -o "${ROOT_DIR}/bin/agent" ./cmd/agent

echo "==> Building local gateway binary..."
(
  cd "${ROOT_DIR}/gateway"
  go build -o "${ROOT_DIR}/bin/gateway" ./cmd/gateway
)

echo "==> Building local backend binary..."
(
  cd "${ROOT_DIR}/backend"
  go build -o "${ROOT_DIR}/bin/backend" ./cmd/backend
)

echo "==> Rebuilding Docker images (backend, gateway, dashboard)..."
docker compose build backend gateway dashboard

if [[ "${START_STACK}" -eq 1 ]]; then
  echo "==> Starting local MVP stack..."
  bash "${ROOT_DIR}/scripts/run-local-mvp.sh" up
fi

if [[ "${CHECK_HEALTH}" -eq 1 ]]; then
  echo "==> Running health checks..."
  bash "${ROOT_DIR}/scripts/run-local-mvp.sh" health
fi

echo "==> Runtime rebuild complete."
