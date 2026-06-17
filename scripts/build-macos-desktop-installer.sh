#!/usr/bin/env bash
# build-macos-desktop-installer.sh
# Builds a universal (arm64 + amd64) macOS .pkg installer that includes both
# the Themisto desktop app (Wails) and the agent daemon.
#
# Usage:
#   bash scripts/build-macos-desktop-installer.sh [VERSION] [OUT_DIR] [FLAGS...]
#
# Positional args (all optional):
#   VERSION      package version string  (default: 0.1.0)
#   OUT_DIR      output directory        (default: dist/installers)
#
# Named flags (all optional):
#   --config-file <path>           embed this agent.json instead of installer/macos/agent.json
#   --sign <identity>              sign the .pkg with a Developer ID Installer certificate
#   --notarize                     submit the signed .pkg to Apple for notarization and staple
#   --apple-id <email>             Apple ID for notarization (required with --notarize)
#   --team-id <id>                 Apple Developer Team ID (required with --notarize)
#   --app-password <secret>        app-specific password for notarization (required with --notarize)

set -euo pipefail

# ── Argument parsing ──────────────────────────────────────────────────────────

VERSION="${1:-0.1.0}"
OUT_DIR="${2:-dist/installers}"

CONFIG_FILE=""
SIGN_IDENTITY=""
NOTARIZE=""
APPLE_ID=""
TEAM_ID=""
APP_PASSWORD=""

shift 2 2>/dev/null || true
while [[ $# -gt 0 ]]; do
  case "$1" in
    --config-file)   CONFIG_FILE="$2";    shift 2 ;;
    --sign)          SIGN_IDENTITY="$2";  shift 2 ;;
    --notarize)      NOTARIZE=1;          shift   ;;
    --apple-id)      APPLE_ID="$2";       shift 2 ;;
    --team-id)       TEAM_ID="$2";        shift 2 ;;
    --app-password)  APP_PASSWORD="$2";   shift 2 ;;
    *) echo "unknown flag: $1" >&2; exit 1 ;;
  esac
done

# ── Paths ─────────────────────────────────────────────────────────────────────

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
WORK_DIR="$(mktemp -d)"
PKG_ROOT="$WORK_DIR/root"
PKG_SCRIPTS="$WORK_DIR/scripts"
PKG_OUT="$OUT_DIR/ThemistoDesktop-macos-${VERSION}.pkg"

APP_BUNDLE_NAME="Themisto.app"
APP_EXECUTABLE="themisto-desktop"
AGENT_EXECUTABLE="themisto-agent"

# Resolve Go and Wails
GO="${GO:-$(command -v go 2>/dev/null || echo /opt/homebrew/bin/go)}"
WAILS="${WAILS:-$(command -v wails 2>/dev/null || echo "$HOME/go/bin/wails")}"
GOCACHE="${GOCACHE:-$ROOT_DIR/.gocache-macpkg}"
GOMODCACHE="${GOMODCACHE:-$ROOT_DIR/.gomodcache-macpkg}"

cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

# ── Validate tools ────────────────────────────────────────────────────────────

for tool in "$GO" "$WAILS" lipo pkgbuild; do
  if ! command -v "$tool" &>/dev/null 2>&1 && [[ ! -x "$tool" ]]; then
    echo "error: required tool not found: $tool" >&2
    exit 1
  fi
done

# ── Create package directory layout ──────────────────────────────────────────

mkdir -p \
  "$OUT_DIR" \
  "$PKG_ROOT/Applications" \
  "$PKG_ROOT/usr/local/bin" \
  "$PKG_ROOT/etc/themisto" \
  "$PKG_ROOT/Library/LaunchDaemons" \
  "$PKG_SCRIPTS"

# ── Build universal agent binary ──────────────────────────────────────────────

echo "==> Building agent (arm64)..."
GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" \
  GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 \
  "$GO" build -buildvcs=false \
    -o "$WORK_DIR/themisto-agent-arm64" \
    "$ROOT_DIR/cmd/agent"

echo "==> Building agent (amd64)..."
GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" \
  GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 \
  CC="clang -target x86_64-apple-macos10.15" \
  "$GO" build -buildvcs=false \
    -o "$WORK_DIR/themisto-agent-amd64" \
    "$ROOT_DIR/cmd/agent"

echo "==> Creating universal agent binary..."
lipo -create -output "$PKG_ROOT/usr/local/bin/$AGENT_EXECUTABLE" \
  "$WORK_DIR/themisto-agent-arm64" \
  "$WORK_DIR/themisto-agent-amd64"
chmod 0755 "$PKG_ROOT/usr/local/bin/$AGENT_EXECUTABLE"
# lipo invalidates the per-arch ad-hoc signatures Go applied at link time; re-sign
# so the binary is runnable on Apple Silicon.
xattr -cr "$PKG_ROOT/usr/local/bin/$AGENT_EXECUTABLE" 2>/dev/null || true
codesign --force --sign - --timestamp=none "$PKG_ROOT/usr/local/bin/$AGENT_EXECUTABLE"

# ── Build universal desktop app ───────────────────────────────────────────────

# iCloud File Provider (for Documents-synced repos) attaches com.apple.FinderInfo
# and com.apple.fileprovider.fpfs#P xattrs to files created under ~/Documents, which
# makes codesign fail with "resource fork, Finder information, or similar detritus
# not allowed". Wails' internal self-sign step trips this. We let wails finish the
# compile/package step, then move the bundle to $WORK_DIR (under /tmp, outside the
# iCloud provider), strip xattrs there, and sign it ourselves.
build_desktop() {
  local arch="$1"
  local dest="$WORK_DIR/${APP_BUNDLE_NAME}-${arch}"

  echo "==> Building desktop app (${arch})..."
  (
    cd "$ROOT_DIR/desktop"
    rm -rf "build/bin/$APP_BUNDLE_NAME"
    set +e
    GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" \
      "$WAILS" build -platform "darwin/${arch}"
    local wails_rc=$?
    set -e
    if [[ ! -d "build/bin/$APP_BUNDLE_NAME" ]]; then
      echo "error: wails did not produce $APP_BUNDLE_NAME (exit=$wails_rc)" >&2
      exit 1
    fi
    if [[ $wails_rc -ne 0 ]]; then
      echo "==> wails codesign step failed (expected under iCloud); re-signing manually..."
    fi
  )

  # Move the bundle out of iCloud territory before touching xattrs or codesign.
  rm -rf "$dest"
  ditto --noextattr --norsrc "$ROOT_DIR/desktop/build/bin/$APP_BUNDLE_NAME" "$dest"
  xattr -cr "$dest" 2>/dev/null || true
  codesign --force --deep --sign - --timestamp=none "$dest"
  codesign --verify --deep --strict "$dest"
}

build_desktop arm64
build_desktop amd64

echo "==> Creating universal desktop app bundle..."
# Start with the arm64 bundle as the base (carries all resources/plist/icons)
ditto --noextattr --norsrc "$WORK_DIR/${APP_BUNDLE_NAME}-arm64" "$PKG_ROOT/Applications/$APP_BUNDLE_NAME"
lipo -create -output "$PKG_ROOT/Applications/$APP_BUNDLE_NAME/Contents/MacOS/$APP_EXECUTABLE" \
  "$WORK_DIR/${APP_BUNDLE_NAME}-arm64/Contents/MacOS/$APP_EXECUTABLE" \
  "$WORK_DIR/${APP_BUNDLE_NAME}-amd64/Contents/MacOS/$APP_EXECUTABLE"
chmod 0755 "$PKG_ROOT/Applications/$APP_BUNDLE_NAME/Contents/MacOS/$APP_EXECUTABLE"

# lipo produced a new Mach-O, invalidating the signature we just applied. Sign again.
xattr -cr "$PKG_ROOT/Applications/$APP_BUNDLE_NAME" 2>/dev/null || true
codesign --force --deep --sign - --timestamp=none "$PKG_ROOT/Applications/$APP_BUNDLE_NAME"
codesign --verify --deep --strict "$PKG_ROOT/Applications/$APP_BUNDLE_NAME"

# ── Copy helper scripts ───────────────────────────────────────────────────────

install -m 0755 "$ROOT_DIR/installer/macos/themisto-service"  "$PKG_ROOT/usr/local/bin/themisto-service"
install -m 0755 "$ROOT_DIR/installer/macos/themisto-activate" "$PKG_ROOT/usr/local/bin/themisto-activate"
install -m 0755 "$ROOT_DIR/installer/macos/themisto-uninstall" "$PKG_ROOT/usr/local/bin/themisto-uninstall"

# ── Agent config ─────────────────────────────────────────────────────────────

if [[ -n "$CONFIG_FILE" ]]; then
  install -m 0644 "$CONFIG_FILE" "$PKG_ROOT/etc/themisto/agent.json"
else
  install -m 0644 "$ROOT_DIR/installer/macos/agent.json" "$PKG_ROOT/etc/themisto/agent.json"
fi

# ── launchd plist ─────────────────────────────────────────────────────────────

install -m 0644 \
  "$ROOT_DIR/installer/macos/com.themisto.agent.plist" \
  "$PKG_ROOT/Library/LaunchDaemons/com.themisto.agent.plist"

# ── preinstall: stop any running agent before upgrade ─────────────────────────

cat > "$PKG_SCRIPTS/preinstall" <<'PREINSTALL'
#!/bin/sh
set -eu

PLIST_PATH="/Library/LaunchDaemons/com.themisto.agent.plist"
LABEL="com.themisto.agent"

# Unload the launchd daemon if it is currently loaded
if /bin/launchctl print "system/$LABEL" >/dev/null 2>&1; then
  echo "Stopping Themisto agent service..."
  /bin/launchctl bootout "system/$LABEL" >/dev/null 2>&1 || true
  # Give the process a moment to exit cleanly
  sleep 1
fi

# Clear system proxy settings owned by the agent so the upgrade
# does not leave a dangling proxy if the binary is replaced mid-run.
if [ -x "/usr/local/bin/themisto-agent" ]; then
  /usr/local/bin/themisto-agent -proxy-off >/dev/null 2>&1 || true
fi

exit 0
PREINSTALL
chmod 0755 "$PKG_SCRIPTS/preinstall"

# ── postinstall: set permissions and reload daemon ────────────────────────────

cat > "$PKG_SCRIPTS/postinstall" <<'POSTINSTALL'
#!/bin/sh
set -eu

PLIST_PATH="/Library/LaunchDaemons/com.themisto.agent.plist"
LABEL="com.themisto.agent"

# Create runtime directories
mkdir -p /var/log/themisto /etc/themisto
chown -R root:wheel /var/log/themisto /etc/themisto
chmod 0700 /var/log/themisto
chmod 0755 /etc/themisto
if [ -f /etc/themisto/agent.json ]; then
  chmod 0600 /etc/themisto/agent.json
fi

# Fix ownership of the LaunchDaemon plist
chown root:wheel "$PLIST_PATH"
chmod 0644 "$PLIST_PATH"

# Register the daemon so it starts at next boot.
# The agent will not run until enrollment config is placed at /etc/themisto/agent.json
# via the Themisto dashboard and the service is started.
/bin/launchctl bootstrap system "$PLIST_PATH" 2>/dev/null || true

echo "Themisto installed successfully."
echo "Open the Themisto app and complete enrollment via the dashboard to activate."

exit 0
POSTINSTALL
chmod 0755 "$PKG_SCRIPTS/postinstall"

# ── Build the component .pkg ──────────────────────────────────────────────────

COMPONENT_PKG="$WORK_DIR/ThemistoDesktop-component.pkg"

echo "==> Building component package..."
pkgbuild \
  --root       "$PKG_ROOT" \
  --identifier "com.themisto.desktop" \
  --version    "$VERSION" \
  --scripts    "$PKG_SCRIPTS" \
  "$COMPONENT_PKG"

# ── Build distribution .pkg (installer UI with welcome/readme) ───────────────

DIST_XML="$ROOT_DIR/installer/macos/distribution.xml"
RESOURCES_DIR="$ROOT_DIR/installer/macos/resources"

if [[ -f "$DIST_XML" && -d "$RESOURCES_DIR" ]]; then
  echo "==> Creating distribution package: $PKG_OUT"
  productbuild \
    --distribution "$DIST_XML" \
    --package-path "$WORK_DIR" \
    --resources    "$RESOURCES_DIR" \
    --version      "$VERSION" \
    "$PKG_OUT"
else
  echo "==> Distribution XML or resources not found; falling back to component package."
  cp "$COMPONENT_PKG" "$PKG_OUT"
fi

# ── Code signing (optional) ──────────────────────────────────────────────────

if [[ -n "$SIGN_IDENTITY" ]]; then
  SIGNED_PKG="${PKG_OUT%.pkg}-signed.pkg"
  echo "==> Signing package with identity: $SIGN_IDENTITY"
  productsign --sign "$SIGN_IDENTITY" "$PKG_OUT" "$SIGNED_PKG"
  mv "$SIGNED_PKG" "$PKG_OUT"
fi

# ── Notarization (optional) ──────────────────────────────────────────────────

if [[ -n "$NOTARIZE" ]]; then
  if [[ -z "$SIGN_IDENTITY" ]]; then
    echo "error: --notarize requires --sign" >&2
    exit 1
  fi
  if [[ -z "$APPLE_ID" || -z "$TEAM_ID" || -z "$APP_PASSWORD" ]]; then
    echo "error: --notarize requires --apple-id, --team-id, and --app-password" >&2
    exit 1
  fi

  echo "==> Submitting package for notarization..."
  xcrun notarytool submit "$PKG_OUT" \
    --apple-id "$APPLE_ID" \
    --team-id  "$TEAM_ID" \
    --password "$APP_PASSWORD" \
    --wait

  echo "==> Stapling notarization ticket..."
  xcrun stapler staple "$PKG_OUT"
fi

echo "==> Done: $PKG_OUT"
