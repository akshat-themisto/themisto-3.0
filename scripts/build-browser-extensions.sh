#!/usr/bin/env bash
set -euo pipefail

VERSION=""
OUT_DIR="dist/browser/extensions"

usage() {
  cat <<'USAGE'
Usage:
  scripts/build-browser-extensions.sh [--version <version>] [--out-dir <dir>]

Builds browser prompt-capture artifacts:
  - Chromium ZIP (for Chrome/Edge enterprise distribution)
  - Firefox XPI (to be signed before production force-install)
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

if ! command -v zip >/dev/null 2>&1; then
  echo "zip command is required (brew install zip or apt-get install zip)." >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
CHROMIUM_DIR="$ROOT_DIR/adapter/prompt/browser/chromium"
FIREFOX_DIR="$ROOT_DIR/adapter/prompt/browser/firefox"

if [[ ! -f "$CHROMIUM_DIR/manifest.json" || ! -f "$FIREFOX_DIR/manifest.json" ]]; then
  echo "browser adapter manifests not found; expected under adapter/prompt/browser/{chromium,firefox}" >&2
  exit 1
fi

if [[ -z "$VERSION" ]]; then
  VERSION="$(awk -F'"' '/"version"[[:space:]]*:/{print $4; exit}' "$CHROMIUM_DIR/manifest.json")"
fi
if [[ -z "$VERSION" ]]; then
  echo "unable to resolve extension version (pass --version explicitly)" >&2
  exit 1
fi

if [[ "$OUT_DIR" = /* ]]; then
  OUT_ABS="$OUT_DIR"
else
  OUT_ABS="$ROOT_DIR/$OUT_DIR"
fi
mkdir -p "$OUT_ABS"

CHROME_ZIP="$OUT_ABS/themisto-prompt-capture-chromium-$VERSION.zip"
FIREFOX_XPI="$OUT_ABS/themisto-prompt-capture-firefox-$VERSION.xpi"
CHECKSUMS="$OUT_ABS/SHA256SUMS"

rm -f "$CHROME_ZIP" "$FIREFOX_XPI" "$CHECKSUMS"

(
  cd "$CHROMIUM_DIR"
  zip -q -r "$CHROME_ZIP" .
)

(
  cd "$FIREFOX_DIR"
  zip -q -r "$FIREFOX_XPI" .
)

if command -v shasum >/dev/null 2>&1; then
  shasum -a 256 "$CHROME_ZIP" "$FIREFOX_XPI" > "$CHECKSUMS"
elif command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$CHROME_ZIP" "$FIREFOX_XPI" > "$CHECKSUMS"
fi

echo "==> Browser artifacts created:"
echo "    Chromium: $CHROME_ZIP"
echo "    Firefox:  $FIREFOX_XPI"
if [[ -f "$CHECKSUMS" ]]; then
  echo "    Checksums: $CHECKSUMS"
fi
echo ""
echo "Note: Firefox production deployment requires signing the XPI before force-install."
