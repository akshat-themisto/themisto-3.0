#!/usr/bin/env bash
set -euo pipefail

# Never encode Finder/resource-fork metadata as AppleDouble payload files.
export COPYFILE_DISABLE=1

VERSION="${1:-0.1.0}"
OUT_DIR="${2:-dist/installers}"
CONFIG_FILE="${3:-}"
SIGN_IDENTITY="${MACOS_CODESIGN_IDENTITY:-}"
INSTALLER_IDENTITY="${MACOS_INSTALLER_IDENTITY:-}"
NOTARY_PROFILE="${MACOS_NOTARY_PROFILE:-}"

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
WORK_DIR="$(mktemp -d)"
PKG_ROOT="$WORK_DIR/root"
PKG_SCRIPTS="$WORK_DIR/scripts"
PKG_OUT="$OUT_DIR/ThemistoAgent-macos-universal-${VERSION}.pkg"
GO_BIN="${GO:-$(command -v go)}"

cleanup() { rm -rf "$WORK_DIR"; }
trap cleanup EXIT

for tool in "$GO_BIN" clang lipo codesign pkgbuild shasum; do
  if ! command -v "$tool" >/dev/null 2>&1 && [[ ! -x "$tool" ]]; then
    echo "error: required tool not found: $tool" >&2
    exit 1
  fi
done

mkdir -p \
  "$OUT_DIR" \
  "$PKG_ROOT/usr/local/bin" \
  "$PKG_ROOT/usr/local/share/themisto" \
  "$PKG_ROOT/Library/LaunchDaemons" \
  "$PKG_SCRIPTS" \
  "$WORK_DIR/gocache"

build_arch() {
  local goarch="$1"
  local target="$WORK_DIR/themisto-agent-$goarch"
  local cc="clang"
  local cflags="-arch arm64"
  if [[ "$goarch" == "amd64" ]]; then
    cc="clang -target x86_64-apple-macos10.15"
    cflags="-arch x86_64"
  fi
  echo "==> Building macOS agent ($goarch)..."
  (
    cd "$ROOT_DIR"
    GOCACHE="$WORK_DIR/gocache" GOOS=darwin GOARCH="$goarch" CGO_ENABLED=1 \
      CC="$cc" CGO_CFLAGS="$cflags" CGO_LDFLAGS="$cflags" \
      "$GO_BIN" build -buildvcs=false -trimpath -ldflags "-s -w -X main.version=$VERSION" \
      -o "$target" ./cmd/agent
  )
}

build_arch arm64
build_arch amd64
lipo -create \
  "$WORK_DIR/themisto-agent-arm64" \
  "$WORK_DIR/themisto-agent-amd64" \
  -output "$PKG_ROOT/usr/local/bin/themisto-agent"
chmod 0755 "$PKG_ROOT/usr/local/bin/themisto-agent"
xattr -cr "$PKG_ROOT/usr/local/bin/themisto-agent" 2>/dev/null || true
if [[ -n "$SIGN_IDENTITY" ]]; then
  codesign --force --options runtime --timestamp --sign "$SIGN_IDENTITY" "$PKG_ROOT/usr/local/bin/themisto-agent"
else
  codesign --force --timestamp=none --sign - "$PKG_ROOT/usr/local/bin/themisto-agent"
fi
codesign --verify --strict --verbose=2 "$PKG_ROOT/usr/local/bin/themisto-agent"
lipo -info "$PKG_ROOT/usr/local/bin/themisto-agent"

install -m 0755 "$ROOT_DIR/installer/macos/themisto-service" "$PKG_ROOT/usr/local/bin/themisto-service"
install -m 0755 "$ROOT_DIR/installer/macos/themisto-activate" "$PKG_ROOT/usr/local/bin/themisto-activate"
install -m 0755 "$ROOT_DIR/installer/macos/themisto-rollback" "$PKG_ROOT/usr/local/bin/themisto-rollback"
install -m 0755 "$ROOT_DIR/installer/macos/themisto-uninstall" "$PKG_ROOT/usr/local/bin/themisto-uninstall"
if [[ -n "$CONFIG_FILE" ]]; then
  install -m 0600 "$CONFIG_FILE" "$PKG_ROOT/usr/local/share/themisto/agent.default.json"
else
  install -m 0600 "$ROOT_DIR/installer/macos/agent.json" "$PKG_ROOT/usr/local/share/themisto/agent.default.json"
fi
install -m 0644 "$ROOT_DIR/installer/macos/com.themisto.agent.plist" "$PKG_ROOT/Library/LaunchDaemons/com.themisto.agent.plist"

cat > "$PKG_SCRIPTS/preinstall" <<'PREINSTALL'
#!/bin/sh
set -eu
PLIST="/Library/LaunchDaemons/com.themisto.agent.plist"
LABEL="com.themisto.agent"
BACKUP="/Library/Application Support/Themisto/rollback/latest"
rm -rf "$BACKUP"
mkdir -p "$BACKUP"
[ ! -f /usr/local/bin/themisto-agent ] || cp -p /usr/local/bin/themisto-agent "$BACKUP/themisto-agent"
[ ! -f "$PLIST" ] || cp -p "$PLIST" "$BACKUP/com.themisto.agent.plist"
[ ! -f /etc/themisto/agent.json ] || cp -p /etc/themisto/agent.json "$BACKUP/agent.json"
/bin/launchctl bootout "system/$LABEL" >/dev/null 2>&1 || true
[ ! -x /usr/local/bin/themisto-agent ] || /usr/local/bin/themisto-agent -proxy-off >/dev/null 2>&1 || true
PREINSTALL
chmod 0755 "$PKG_SCRIPTS/preinstall"

cat > "$PKG_SCRIPTS/postinstall" <<'POSTINSTALL'
#!/bin/sh
set -eu
PLIST="/Library/LaunchDaemons/com.themisto.agent.plist"
LABEL="com.themisto.agent"
mkdir -p /var/log/themisto /etc/themisto
if [ ! -f /etc/themisto/agent.json ]; then
  cp /usr/local/share/themisto/agent.default.json /etc/themisto/agent.json
fi
chown -R root:wheel /var/log/themisto /etc/themisto /usr/local/share/themisto
chmod 0700 /var/log/themisto
chmod 0755 /etc/themisto
chmod 0600 /etc/themisto/agent.json /usr/local/share/themisto/agent.default.json
chown root:wheel "$PLIST"
chmod 0644 "$PLIST"
if grep -Eq '"enrollment_token"[[:space:]]*:[[:space:]]*"[^"]+"|"cert_path"[[:space:]]*:[[:space:]]*"[^"]+"' /etc/themisto/agent.json; then
  /bin/launchctl bootstrap system "$PLIST" >/dev/null 2>&1 || true
  /bin/launchctl kickstart -k "system/$LABEL" >/dev/null 2>&1 || true
  echo "Themisto Agent installed and started."
else
  echo "Themisto Agent installed; enrollment is required before startup."
fi
POSTINSTALL
chmod 0755 "$PKG_SCRIPTS/postinstall"

# Synced macOS workspaces can attach Finder/iCloud metadata to source files.
# Strip it from the staged payload and scripts so pkgbuild does not emit
# AppleDouble `._*` entries into the installer.
xattr -cr "$PKG_ROOT" "$PKG_SCRIPTS" 2>/dev/null || true

COMPONENT_PKG="$WORK_DIR/ThemistoAgent.pkg"
pkgbuild --root "$PKG_ROOT" --identifier "com.themisto.agent" --version "$VERSION" --scripts "$PKG_SCRIPTS" "$COMPONENT_PKG"
if [[ -n "$INSTALLER_IDENTITY" ]]; then
  productsign --sign "$INSTALLER_IDENTITY" "$COMPONENT_PKG" "$PKG_OUT"
else
  cp "$COMPONENT_PKG" "$PKG_OUT"
fi

if [[ -n "$NOTARY_PROFILE" ]]; then
  if [[ -z "$SIGN_IDENTITY" || -z "$INSTALLER_IDENTITY" ]]; then
    echo "error: notarization requires MACOS_CODESIGN_IDENTITY and MACOS_INSTALLER_IDENTITY" >&2
    exit 1
  fi
  xcrun notarytool submit "$PKG_OUT" --keychain-profile "$NOTARY_PROFILE" --wait
  xcrun stapler staple "$PKG_OUT"
fi

shasum -a 256 "$PKG_OUT" > "$PKG_OUT.sha256"
pkgutil --check-signature "$PKG_OUT" || true
echo "==> Package: $PKG_OUT"
echo "==> Checksum: $PKG_OUT.sha256"
