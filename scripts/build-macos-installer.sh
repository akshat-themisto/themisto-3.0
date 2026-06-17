#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-0.1.0}"
OUT_DIR="${2:-dist/installers}"
CONFIG_FILE="${3:-}"

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
WORK_DIR="$(mktemp -d)"
PKG_ROOT="$WORK_DIR/root"
PKG_SCRIPTS="$WORK_DIR/scripts"
PKG_OUT="$OUT_DIR/ThemistoAgent-macos-${VERSION}.pkg"

cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

mkdir -p "$OUT_DIR" "$PKG_ROOT/usr/local/bin" "$PKG_ROOT/etc/themisto" "$PKG_ROOT/Library/LaunchDaemons" "$PKG_SCRIPTS"

echo "==> Building macOS agent binary..."
(
  cd "$ROOT_DIR"
  GOCACHE="${GOCACHE:-$ROOT_DIR/.gocache}" \
  GOMODCACHE="${GOMODCACHE:-$ROOT_DIR/.gomodcache}" \
  GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 \
  go build -buildvcs=false -o "$PKG_ROOT/usr/local/bin/themisto-agent" ./cmd/agent
)

install -m 0755 "$ROOT_DIR/installer/macos/themisto-service" "$PKG_ROOT/usr/local/bin/themisto-service"
install -m 0755 "$ROOT_DIR/installer/macos/themisto-activate" "$PKG_ROOT/usr/local/bin/themisto-activate"
install -m 0755 "$ROOT_DIR/installer/macos/themisto-uninstall" "$PKG_ROOT/usr/local/bin/themisto-uninstall"
if [[ -n "$CONFIG_FILE" ]]; then
  install -m 0644 "$CONFIG_FILE" "$PKG_ROOT/etc/themisto/agent.json"
else
  install -m 0644 "$ROOT_DIR/installer/macos/agent.json" "$PKG_ROOT/etc/themisto/agent.json"
fi
install -m 0644 "$ROOT_DIR/installer/macos/com.themisto.agent.plist" "$PKG_ROOT/Library/LaunchDaemons/com.themisto.agent.plist"

cat > "$PKG_SCRIPTS/postinstall" <<'POSTINSTALL'
#!/bin/sh
set -eu
mkdir -p /var/log/themisto /etc/themisto
chown -R root:wheel /var/log/themisto /etc/themisto
chmod 0700 /var/log/themisto
chmod 0755 /etc/themisto
if [ -f /etc/themisto/agent.json ]; then
  chmod 0600 /etc/themisto/agent.json
fi

should_start=0
if [ -f /etc/themisto/agent.json ]; then
  if grep -Eq '"enrollment_token"[[:space:]]*:[[:space:]]*"[^"]+"' /etc/themisto/agent.json; then
    should_start=1
  fi
  if grep -Eq '"cert_path"[[:space:]]*:[[:space:]]*"[^"]+"' /etc/themisto/agent.json; then
    should_start=1
  fi
fi

if [ "$should_start" -eq 1 ]; then
  if /usr/local/bin/themisto-service install; then
    echo "Themisto Agent installed and service started."
    echo "Device will auto-activate using embedded enrollment config."
  else
    echo "warning: unable to start themisto service automatically" >&2
    echo "run manually: sudo /usr/local/bin/themisto-service install" >&2
  fi
else
  echo "Themisto Agent installed."
  echo "No enrollment data was embedded in /etc/themisto/agent.json."
  echo "Provide an enrollment config and run: sudo /usr/local/bin/themisto-service install"
fi
POSTINSTALL
chmod 0755 "$PKG_SCRIPTS/postinstall"

echo "==> Creating package: $PKG_OUT"
pkgbuild \
  --root "$PKG_ROOT" \
  --identifier "com.themisto.agent" \
  --version "$VERSION" \
  --scripts "$PKG_SCRIPTS" \
  "$PKG_OUT"

echo "==> Done: $PKG_OUT"
