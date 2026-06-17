#!/usr/bin/env bash
set -euo pipefail

OUT_DIR="dist/browser/policies"
CHROME_EXTENSION_ID=""
CHROME_UPDATE_URL="https://clients2.google.com/service/update2/crx"
FIREFOX_EXTENSION_ID="themisto-prompt-capture@themisto.local"
FIREFOX_INSTALL_URL=""

usage() {
  cat <<'USAGE'
Usage:
  scripts/render-browser-policies.sh \
    --chrome-extension-id <id> \
    --firefox-install-url <signed_xpi_url> \
    [--chrome-update-url <url>] \
    [--firefox-extension-id <id>] \
    [--out-dir <dir>]

Outputs:
  - chrome-enterprise-force-install.json
  - firefox-enterprise-policies.json
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-dir)
      OUT_DIR="$2"
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
  echo "--firefox-install-url is required (URL to signed Firefox XPI)." >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
if [[ "$OUT_DIR" = /* ]]; then
  OUT_ABS="$OUT_DIR"
else
  OUT_ABS="$ROOT_DIR/$OUT_DIR"
fi
mkdir -p "$OUT_ABS"

CHROME_POLICY="$OUT_ABS/chrome-enterprise-force-install.json"
FIREFOX_POLICY="$OUT_ABS/firefox-enterprise-policies.json"

cat > "$CHROME_POLICY" <<EOF
{
  "ExtensionInstallForcelist": [
    "$CHROME_EXTENSION_ID;$CHROME_UPDATE_URL"
  ]
}
EOF

cat > "$FIREFOX_POLICY" <<EOF
{
  "policies": {
    "ExtensionSettings": {
      "*": {
        "installation_mode": "allowed"
      },
      "$FIREFOX_EXTENSION_ID": {
        "installation_mode": "force_installed",
        "install_url": "$FIREFOX_INSTALL_URL"
      }
    }
  }
}
EOF

echo "==> Browser policy files created:"
echo "    Chrome policy:  $CHROME_POLICY"
echo "    Firefox policy: $FIREFOX_POLICY"
