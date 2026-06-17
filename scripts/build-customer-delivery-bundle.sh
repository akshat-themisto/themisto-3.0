#!/usr/bin/env bash
set -euo pipefail

VERSION="0.1.0"
OUT_DIR="dist/customer-delivery"

CONFIG_FILE=""
DEVICE_ID=""
ORG_NAME=""
TOKEN=""
BACKEND_URL=""
GATEWAY_URL=""

CHROME_EXTENSION_ID=""
CHROME_UPDATE_URL="https://clients2.google.com/service/update2/crx"
FIREFOX_EXTENSION_ID="themisto-prompt-capture@themisto.local"
FIREFOX_INSTALL_URL=""

usage() {
  cat <<'USAGE'
Usage:
  scripts/build-customer-delivery-bundle.sh \
    [--version <version>] \
    [--out-dir <dir>] \
    [--config-file <agent_json_from_dashboard> | \
      --device-id <id> --org-name <name> --token <token> --backend-url <url> --gateway-url <url>] \
    --chrome-extension-id <id> \
    --firefox-install-url <signed_xpi_url> \
    [--chrome-update-url <url>] \
    [--firefox-extension-id <id>]

Builds one customer delivery bundle containing:
  - per-device endpoint installers (macOS/Windows + agent.json)
  - browser extension artifacts (Chromium ZIP, Firefox XPI)
  - managed browser policy files for Chrome + Firefox
  - DELIVERY_README.md summary
USAGE
}

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
    --config-file)
      CONFIG_FILE="$2"
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
    --chrome-extension-id)
      CHROME_EXTENSION_ID="$2"
      shift 2
      ;;
    --chrome-update-url)
      CHROME_UPDATE_URL="$2"
      shift 2
      ;;
    --firefox-extension-id)
      FIREFOX_EXTENSION_ID="$2"
      shift 2
      ;;
    --firefox-install-url)
      FIREFOX_INSTALL_URL="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ -z "$CHROME_EXTENSION_ID" ]]; then
  echo "--chrome-extension-id is required." >&2
  exit 1
fi
if [[ -z "$FIREFOX_INSTALL_URL" ]]; then
  echo "--firefox-install-url is required." >&2
  exit 1
fi

if [[ -z "$CONFIG_FILE" && ( -z "$DEVICE_ID" || -z "$ORG_NAME" || -z "$TOKEN" || -z "$BACKEND_URL" || -z "$GATEWAY_URL" ) ]]; then
  echo "Provide either --config-file or full inline enrollment fields." >&2
  usage >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
if [[ "$OUT_DIR" = /* ]]; then
  OUT_ABS="$OUT_DIR"
else
  OUT_ABS="$ROOT_DIR/$OUT_DIR"
fi
mkdir -p "$OUT_ABS"

if [[ -n "$CONFIG_FILE" && ! -f "$CONFIG_FILE" ]]; then
  echo "config file not found: $CONFIG_FILE" >&2
  exit 1
fi

if [[ -z "$DEVICE_ID" && -n "$CONFIG_FILE" ]]; then
  DEVICE_ID="$(awk -F'"' '/"device_id"[[:space:]]*:/{print $4; exit}' "$CONFIG_FILE")"
fi
if [[ -z "$DEVICE_ID" ]]; then
  echo "unable to determine device ID (set --device-id or provide one in --config-file)." >&2
  exit 1
fi

INSTALLERS_OUT="$OUT_ABS/installers"
BROWSER_EXT_OUT="$OUT_ABS/browser/extensions"
BROWSER_POLICIES_OUT="$OUT_ABS/browser/policies"

INSTALLER_CMD=(
  "$ROOT_DIR/scripts/build-device-installers.sh"
  --version "$VERSION"
  --out-dir "$INSTALLERS_OUT"
)
if [[ -n "$CONFIG_FILE" ]]; then
  INSTALLER_CMD+=(--config-file "$CONFIG_FILE")
  if [[ -n "$DEVICE_ID" ]]; then
    INSTALLER_CMD+=(--device-id "$DEVICE_ID")
  fi
else
  INSTALLER_CMD+=(
    --device-id "$DEVICE_ID"
    --org-name "$ORG_NAME"
    --token "$TOKEN"
    --backend-url "$BACKEND_URL"
    --gateway-url "$GATEWAY_URL"
  )
fi

echo "==> Building per-device endpoint installers..."
"${INSTALLER_CMD[@]}"

echo "==> Building browser extension artifacts..."
"$ROOT_DIR/scripts/build-browser-extensions.sh" \
  --version "$VERSION" \
  --out-dir "$BROWSER_EXT_OUT"

echo "==> Rendering managed browser policy files..."
"$ROOT_DIR/scripts/render-browser-policies.sh" \
  --out-dir "$BROWSER_POLICIES_OUT" \
  --chrome-extension-id "$CHROME_EXTENSION_ID" \
  --chrome-update-url "$CHROME_UPDATE_URL" \
  --firefox-extension-id "$FIREFOX_EXTENSION_ID" \
  --firefox-install-url "$FIREFOX_INSTALL_URL"

DEVICE_DIR="$INSTALLERS_OUT/device-$DEVICE_ID"
README_FILE="$OUT_ABS/DELIVERY_README.md"
cat > "$README_FILE" <<EOF
# Themisto Customer Delivery Bundle

Generated: $(date -u +"%Y-%m-%dT%H:%M:%SZ")
Device ID: $DEVICE_ID
Version: $VERSION

## 1) Endpoint Installers
- macOS package: \`$DEVICE_DIR/ThemistoAgent-macos-$VERSION.pkg\`
- Windows bundle: \`$DEVICE_DIR/ThemistoAgent-windows-$VERSION.zip\`
- Embedded config: \`$DEVICE_DIR/agent.json\`

## 2) Browser Extension Artifacts
- Chromium upload zip: \`$BROWSER_EXT_OUT/themisto-prompt-capture-chromium-$VERSION.zip\`
- Firefox xpi (sign before production): \`$BROWSER_EXT_OUT/themisto-prompt-capture-firefox-$VERSION.xpi\`

## 3) Managed Browser Policies
- Chrome: \`$BROWSER_POLICIES_OUT/chrome-enterprise-force-install.json\`
- Firefox: \`$BROWSER_POLICIES_OUT/firefox-enterprise-policies.json\`

## 4) Safari Status
Safari is not in the current GA gate. Track Safari as a separate milestone
for signed/notarized Safari Web Extension + MDM force-enable rollout.
EOF

echo "==> Customer delivery bundle ready: $OUT_ABS"
echo "    Summary: $README_FILE"
