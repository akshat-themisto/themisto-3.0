#!/usr/bin/env bash
set -euo pipefail

VERSION="0.1.0"
OUT_DIR="dist/installers"
DEVICE_ID=""
ORG_NAME=""
TOKEN=""
BACKEND_URL=""
GATEWAY_URL=""
CONFIG_FILE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      VERSION="$2"
      shift 2
      ;;
    --out-dir)
      OUT_DIR="$2"
      shift 2
      ;;
    --device-id)
      DEVICE_ID="$2"
      shift 2
      ;;
    --org-name)
      ORG_NAME="$2"
      shift 2
      ;;
    --token)
      TOKEN="$2"
      shift 2
      ;;
    --backend-url)
      BACKEND_URL="$2"
      shift 2
      ;;
    --gateway-url)
      GATEWAY_URL="$2"
      shift 2
      ;;
    --config-file)
      CONFIG_FILE="$2"
      shift 2
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

if [[ -z "$CONFIG_FILE" && ( -z "$DEVICE_ID" || -z "$ORG_NAME" || -z "$TOKEN" || -z "$BACKEND_URL" || -z "$GATEWAY_URL" ) ]]; then
  cat >&2 <<'USAGE'
Usage:
  scripts/build-device-installers.sh \
    [--config-file <agent_json_from_dashboard>] \
    [--device-id <device_id>] \
    [--org-name <org_name>] \
    [--token <enrollment_token>] \
    [--backend-url <backend_url>] \
    [--gateway-url <gateway_url>] \
    [--version <version>] \
    [--out-dir <output_dir>]
USAGE
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
if [[ -n "$CONFIG_FILE" ]]; then
  if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "config file not found: $CONFIG_FILE" >&2
    exit 1
  fi
  if [[ -z "$DEVICE_ID" ]]; then
    DEVICE_ID="$(awk -F'"' '/"device_id"[[:space:]]*:/{print $4; exit}' "$CONFIG_FILE")"
  fi
  if [[ -z "$DEVICE_ID" ]]; then
    echo "unable to infer device_id from $CONFIG_FILE; pass --device-id explicitly" >&2
    exit 1
  fi
fi

DEVICE_DIR="$OUT_DIR/device-$DEVICE_ID"
CONFIG_PATH="$DEVICE_DIR/agent.json"

mkdir -p "$DEVICE_DIR"

if [[ -n "$CONFIG_FILE" ]]; then
  src_real="$(cd "$(dirname "$CONFIG_FILE")" && pwd)/$(basename "$CONFIG_FILE")"
  dst_real="$(cd "$(dirname "$CONFIG_PATH")" && pwd)/$(basename "$CONFIG_PATH")"
  if [[ "$src_real" != "$dst_real" ]]; then
    cp "$CONFIG_FILE" "$CONFIG_PATH"
  fi
else
  "$ROOT_DIR/scripts/render-agent-config.sh" \
    "$CONFIG_PATH" \
    "$BACKEND_URL" \
    "$GATEWAY_URL" \
    "$DEVICE_ID" \
    "$ORG_NAME" \
    "$TOKEN"
fi

echo "==> Building Windows installer bundle with embedded enrollment..."
"$ROOT_DIR/scripts/build-windows-bundle.sh" "$VERSION" "$DEVICE_DIR" "$CONFIG_PATH"

if command -v pkgbuild >/dev/null 2>&1; then
  echo "==> Building macOS pkg with embedded enrollment..."
  "$ROOT_DIR/scripts/build-macos-installer.sh" "$VERSION" "$DEVICE_DIR" "$CONFIG_PATH"
else
  echo "==> Skipping macOS pkg (pkgbuild not available on this host)."
fi

echo "==> Device installer artifacts written to: $DEVICE_DIR"
