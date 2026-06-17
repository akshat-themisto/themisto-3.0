#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-0.1.0}"
OUT_DIR="${2:-dist/installers}"
CONFIG_FILE="${3:-}"

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
WORK_DIR="$(mktemp -d)"
BUNDLE_DIR="$WORK_DIR/ThemistoAgent-windows-${VERSION}"
ZIP_PATH="$OUT_DIR/ThemistoAgent-windows-${VERSION}.zip"
if [[ "$ZIP_PATH" = /* ]]; then
  ZIP_TARGET="$ZIP_PATH"
else
  ZIP_TARGET="$ROOT_DIR/$ZIP_PATH"
fi

cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

mkdir -p "$BUNDLE_DIR" "$OUT_DIR"

echo "==> Building Windows agent binary..."
(
  cd "$ROOT_DIR"
  GOCACHE="${GOCACHE:-$ROOT_DIR/.gocache}" \
  GOMODCACHE="${GOMODCACHE:-$ROOT_DIR/.gomodcache}" \
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -buildvcs=false -o "$BUNDLE_DIR/themisto-agent.exe" ./cmd/agent
)

cp "$ROOT_DIR/installer/windows/install.ps1" "$BUNDLE_DIR/install.ps1"
cp "$ROOT_DIR/installer/windows/install.bat" "$BUNDLE_DIR/install.bat"
cp "$ROOT_DIR/installer/windows/uninstall.ps1" "$BUNDLE_DIR/uninstall.ps1"
cp "$ROOT_DIR/installer/windows/uninstall.bat" "$BUNDLE_DIR/uninstall.bat"
cp "$ROOT_DIR/installer/windows/themisto-service.ps1" "$BUNDLE_DIR/themisto-service.ps1"
cp "$ROOT_DIR/installer/windows/themisto-activate.ps1" "$BUNDLE_DIR/themisto-activate.ps1"
cp -R "$ROOT_DIR/semantic-classifier/deberta" "$BUNDLE_DIR/classifier"
if [[ -n "$CONFIG_FILE" ]]; then
  cp "$CONFIG_FILE" "$BUNDLE_DIR/agent.json"
else
  cp "$ROOT_DIR/installer/windows/agent.json" "$BUNDLE_DIR/agent.json"
fi

(
  cd "$WORK_DIR"
  if command -v zip >/dev/null 2>&1; then
    zip -r "$ZIP_TARGET" "$(basename "$BUNDLE_DIR")" >/dev/null
  else
    tar -czf "${ZIP_TARGET%.zip}.tar.gz" "$(basename "$BUNDLE_DIR")"
  fi
)

echo "==> Done: $ZIP_PATH"
