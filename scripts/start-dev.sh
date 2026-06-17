#!/usr/bin/env bash
# start-dev.sh — bring up the docker stack and launch the agent locally.
#
# Usage: bash scripts/start-dev.sh
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

GO="${GO:-$(command -v go 2>/dev/null || echo /usr/local/go/bin/go)}"

echo "==> Starting docker stack (postgres, gateway, backend, dashboard)..."
docker compose up -d

echo "==> Waiting for gateway to be reachable..."
for i in $(seq 1 30); do
  if docker compose ps --status running | grep -q gateway; then
    break
  fi
  sleep 1
done

echo "==> Building agent..."
"$GO" build -o bin/agent ./cmd/agent

echo "==> Launching agent..."
exec ./bin/agent
