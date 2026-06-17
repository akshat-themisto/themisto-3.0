#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

ACTION="${1:-help}"
AGENT_CONFIG_PATH="${AGENT_CONFIG:-${ROOT_DIR}/agent.json}"
HEALTH_GATEWAY="${HEALTH_GATEWAY:-http://127.0.0.1:18080/healthz}"
HEALTH_BACKEND="${HEALTH_BACKEND:-http://localhost:8443/healthz}"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

build_agent() {
  need_cmd go
  mkdir -p "${ROOT_DIR}/bin"
  echo "[build] agent binary -> bin/agent"
  go build -o "${ROOT_DIR}/bin/agent" ./cmd/agent
}

build_containers() {
  need_cmd docker
  echo "[build] backend + gateway + dashboard containers"
  docker compose build backend gateway dashboard
}

up_containers() {
  need_cmd docker
  echo "[up] starting postgres + backend + gateway + dashboard"
  docker compose up -d postgres backend gateway dashboard
  echo "[up] container status"
  docker compose ps
}

check_health() {
  need_cmd curl
  echo "[health] gateway: ${HEALTH_GATEWAY}"
  curl -fsS --max-time 8 "${HEALTH_GATEWAY}"
  echo
  echo "[health] backend: ${HEALTH_BACKEND}"
  curl -fsS --max-time 8 "${HEALTH_BACKEND}"
  echo
}

run_agent() {
  need_cmd go
  if [[ ! -x "${ROOT_DIR}/bin/agent" ]]; then
    build_agent
  fi
  if [[ ! -f "${AGENT_CONFIG_PATH}" ]]; then
    echo "agent config not found: ${AGENT_CONFIG_PATH}" >&2
    exit 1
  fi
  echo "[agent] running with config: ${AGENT_CONFIG_PATH}"
  "${ROOT_DIR}/bin/agent" -config "${AGENT_CONFIG_PATH}"
}

print_help() {
  cat <<EOF
Usage: scripts/run-local-mvp.sh <command>

Commands:
  build          Build agent binary and Docker images (backend/gateway/dashboard)
  up             Start containers: postgres, backend, gateway, dashboard
  down           Stop and remove compose stack
  status         Show compose container status
  health         Check backend/gateway health endpoints
  agent          Build (if needed) and run local agent foreground
  logs [svc...]  Follow compose logs (default: backend gateway dashboard postgres)
  all            Build -> start containers -> health check -> run agent
  help           Show this help

Environment overrides:
  AGENT_CONFIG   Agent config path (default: ${ROOT_DIR}/agent.json)
  HEALTH_GATEWAY Gateway health URL (default: http://127.0.0.1:18080/healthz)
  HEALTH_BACKEND Backend health URL (default: http://localhost:8443/healthz)
EOF
}

case "${ACTION}" in
  build)
    build_agent
    build_containers
    ;;
  up)
    up_containers
    ;;
  down)
    need_cmd docker
    docker compose down
    ;;
  status)
    need_cmd docker
    docker compose ps
    ;;
  health)
    check_health
    ;;
  agent)
    run_agent
    ;;
  logs)
    need_cmd docker
    shift || true
    if [[ "$#" -eq 0 ]]; then
      docker compose logs -f backend gateway dashboard postgres
    else
      docker compose logs -f "$@"
    fi
    ;;
  all)
    build_agent
    build_containers
    up_containers
    check_health
    run_agent
    ;;
  help|-h|--help)
    print_help
    ;;
  *)
    echo "unknown command: ${ACTION}" >&2
    print_help
    exit 1
    ;;
esac

