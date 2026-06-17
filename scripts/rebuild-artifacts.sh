#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

VERSION="0.1.0"
OUT_DIR="dist/rebuild"
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

BUILD_WINDOWS=1
BUILD_MACOS=1
BUILD_BROWSER=1
BUILD_DEVICE=1
BUILD_CUSTOMER=1

usage() {
  cat <<'EOF'
Usage:
  scripts/rebuild-artifacts.sh [options]

Rebuilds packaged artifacts from the current checkout:
  - generic Windows installer ZIP
  - generic macOS installer PKG (when pkgbuild is available)
  - browser extension ZIP/XPI artifacts
  - optional per-device installers
  - optional customer delivery bundle

Options:
  --version <version>                Artifact version (default: 0.1.0)
  --out-dir <dir>                    Base output dir (default: dist/rebuild)
  --config-file <agent.json>         Embedded agent config for per-device/customer artifacts
  --device-id <id>                   Device ID for generated enrollment config
  --org-name <name>                  Org name for generated enrollment config
  --token <token>                    Enrollment token for generated enrollment config
  --backend-url <url>                Backend URL for generated enrollment config
  --gateway-url <url>                Gateway URL for generated enrollment config
  --chrome-extension-id <id>         Required for customer delivery bundle
  --chrome-update-url <url>          Optional Chrome update URL override
  --firefox-extension-id <id>        Optional Firefox extension ID override
  --firefox-install-url <url>        Required for customer delivery bundle
  --skip-windows                     Skip generic Windows bundle rebuild
  --skip-macos                       Skip generic macOS package rebuild
  --skip-browser                     Skip generic browser extension rebuild
  --skip-device-installers           Skip per-device installer rebuild
  --skip-customer-delivery           Skip customer delivery bundle rebuild
  -h, --help

Per-device installers are rebuilt only when either:
  - --config-file is provided, or
  - all of --device-id, --org-name, --token, --backend-url, and --gateway-url are provided.

Customer delivery is rebuilt only when:
  - per-device installer inputs are available, and
  - both --chrome-extension-id and --firefox-install-url are provided.
EOF
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
    --skip-windows)
      BUILD_WINDOWS=0
      shift
      ;;
    --skip-macos)
      BUILD_MACOS=0
      shift
      ;;
    --skip-browser)
      BUILD_BROWSER=0
      shift
      ;;
    --skip-device-installers)
      BUILD_DEVICE=0
      shift
      ;;
    --skip-customer-delivery)
      BUILD_CUSTOMER=0
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ -d "${ROOT_DIR}/.tools/bin" ]]; then
  export PATH="${ROOT_DIR}/.tools/bin:${PATH}"
fi
export GOCACHE="${GOCACHE:-${ROOT_DIR}/.gocache}"
export GOMODCACHE="${GOMODCACHE:-${ROOT_DIR}/.gomodcache}"
export GOFLAGS="${GOFLAGS:--buildvcs=false}"

if [[ "${OUT_DIR}" = /* ]]; then
  OUT_ABS="${OUT_DIR}"
else
  OUT_ABS="${ROOT_DIR}/${OUT_DIR}"
fi

GENERIC_INSTALLERS_OUT="${OUT_ABS}/generic/installers"
BROWSER_EXT_OUT="${OUT_ABS}/browser/extensions"
DEVICE_INSTALLERS_OUT="${OUT_ABS}/device/installers"
CUSTOMER_DELIVERY_OUT="${OUT_ABS}/customer-delivery"

mkdir -p "${GENERIC_INSTALLERS_OUT}" "${BROWSER_EXT_OUT}"

has_device_inputs() {
  [[ -n "${CONFIG_FILE}" ]] || [[ -n "${DEVICE_ID}" && -n "${ORG_NAME}" && -n "${TOKEN}" && -n "${BACKEND_URL}" && -n "${GATEWAY_URL}" ]]
}

has_customer_inputs() {
  [[ -n "${CHROME_EXTENSION_ID}" && -n "${FIREFOX_INSTALL_URL}" ]]
}

if [[ "${BUILD_WINDOWS}" -eq 1 ]]; then
  echo "==> Rebuilding generic Windows installer bundle..."
  bash "${ROOT_DIR}/scripts/build-windows-bundle.sh" "${VERSION}" "${GENERIC_INSTALLERS_OUT}"
fi

if [[ "${BUILD_MACOS}" -eq 1 ]]; then
  if command -v pkgbuild >/dev/null 2>&1; then
    echo "==> Rebuilding generic macOS installer package..."
    bash "${ROOT_DIR}/scripts/build-macos-installer.sh" "${VERSION}" "${GENERIC_INSTALLERS_OUT}"
  else
    echo "==> Skipping generic macOS package rebuild (pkgbuild not available on this host)."
  fi
fi

if [[ "${BUILD_BROWSER}" -eq 1 ]]; then
  echo "==> Rebuilding browser extension artifacts..."
  bash "${ROOT_DIR}/scripts/build-browser-extensions.sh" \
    --version "${VERSION}" \
    --out-dir "${BROWSER_EXT_OUT}"
fi

if [[ "${BUILD_DEVICE}" -eq 1 ]]; then
  if has_device_inputs; then
    echo "==> Rebuilding per-device installers..."
    device_cmd=(
      bash "${ROOT_DIR}/scripts/build-device-installers.sh"
      --version "${VERSION}"
      --out-dir "${DEVICE_INSTALLERS_OUT}"
    )
    if [[ -n "${CONFIG_FILE}" ]]; then
      device_cmd+=(--config-file "${CONFIG_FILE}")
      if [[ -n "${DEVICE_ID}" ]]; then
        device_cmd+=(--device-id "${DEVICE_ID}")
      fi
    else
      device_cmd+=(
        --device-id "${DEVICE_ID}"
        --org-name "${ORG_NAME}"
        --token "${TOKEN}"
        --backend-url "${BACKEND_URL}"
        --gateway-url "${GATEWAY_URL}"
      )
    fi
    "${device_cmd[@]}"
  else
    echo "==> Skipping per-device installers (no embedded enrollment config or inline enrollment fields were provided)."
  fi
fi

if [[ "${BUILD_CUSTOMER}" -eq 1 ]]; then
  if has_device_inputs && has_customer_inputs; then
    echo "==> Rebuilding customer delivery bundle..."
    customer_cmd=(
      bash "${ROOT_DIR}/scripts/build-customer-delivery-bundle.sh"
      --version "${VERSION}"
      --out-dir "${CUSTOMER_DELIVERY_OUT}"
      --chrome-extension-id "${CHROME_EXTENSION_ID}"
      --chrome-update-url "${CHROME_UPDATE_URL}"
      --firefox-extension-id "${FIREFOX_EXTENSION_ID}"
      --firefox-install-url "${FIREFOX_INSTALL_URL}"
    )
    if [[ -n "${CONFIG_FILE}" ]]; then
      customer_cmd+=(--config-file "${CONFIG_FILE}")
      if [[ -n "${DEVICE_ID}" ]]; then
        customer_cmd+=(--device-id "${DEVICE_ID}")
      fi
    else
      customer_cmd+=(
        --device-id "${DEVICE_ID}"
        --org-name "${ORG_NAME}"
        --token "${TOKEN}"
        --backend-url "${BACKEND_URL}"
        --gateway-url "${GATEWAY_URL}"
      )
    fi
    "${customer_cmd[@]}"
  else
    echo "==> Skipping customer delivery bundle (requires device installer inputs plus --chrome-extension-id and --firefox-install-url)."
  fi
fi

echo "==> Artifact rebuild complete."
echo "    Base output: ${OUT_ABS}"
