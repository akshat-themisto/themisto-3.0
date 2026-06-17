#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 6 || $# -gt 7 ]]; then
  cat >&2 <<'USAGE'
Usage:
  scripts/render-agent-config.sh <output_json> <backend_url> <gateway_url> <device_id> <org_name> <enrollment_token> [agent_id]

Example:
  scripts/render-agent-config.sh dist/installers/agent-device-1.json \
    http://165.232.186.51:8443 https://165.232.186.51 device-1 Themisto token123
USAGE
  exit 1
fi

OUT_FILE="$1"
BACKEND_URL="$2"
GATEWAY_URL="$3"
DEVICE_ID="$4"
ORG_NAME="$5"
ENROLLMENT_TOKEN="$6"
AGENT_ID="${7:-$DEVICE_ID}"

json_escape() {
  local s="$1"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  s="${s//$'\n'/\\n}"
  s="${s//$'\r'/\\r}"
  s="${s//$'\t'/\\t}"
  printf '%s' "$s"
}

mkdir -p "$(dirname "$OUT_FILE")"

cat > "$OUT_FILE" <<EOF
{
  "agent_id": "$(json_escape "$AGENT_ID")",
  "gateway_url": "$(json_escape "$GATEWAY_URL")",
  "listen_addr": "127.0.0.1:8080",
  "default_decision": "bypass",
  "telemetry_flush_interval": "5s",
  "backend_url": "$(json_escape "$BACKEND_URL")",
  "device_id": "$(json_escape "$DEVICE_ID")",
  "enrollment_token": "$(json_escape "$ENROLLMENT_TOKEN")",
  "org_name": "$(json_escape "$ORG_NAME")"
}
EOF

echo "==> Wrote $OUT_FILE"
