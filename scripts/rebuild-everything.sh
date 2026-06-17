#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

RUN_RUNTIME=1
RUN_ARTIFACTS=1

RUNTIME_ARGS=()
ARTIFACT_ARGS=()

usage() {
  cat <<'EOF'
Usage:
  scripts/rebuild-everything.sh [options]

Runs both:
  - scripts/rebuild-runtime.sh
  - scripts/rebuild-artifacts.sh

Control flags:
  --skip-runtime
  --skip-artifacts
  --up
  --health
  -h, --help

All artifact-related flags are passed through to scripts/rebuild-artifacts.sh,
including:
  --version, --out-dir, --config-file,
  --device-id, --org-name, --token, --backend-url, --gateway-url,
  --chrome-extension-id, --chrome-update-url,
  --firefox-extension-id, --firefox-install-url,
  --skip-windows, --skip-macos, --skip-browser,
  --skip-device-installers, --skip-customer-delivery
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-runtime)
      RUN_RUNTIME=0
      shift
      ;;
    --skip-artifacts)
      RUN_ARTIFACTS=0
      shift
      ;;
    --up|--health)
      RUNTIME_ARGS+=("$1")
      shift
      ;;
    --skip-windows|--skip-macos|--skip-browser|--skip-device-installers|--skip-customer-delivery)
      ARTIFACT_ARGS+=("$1")
      shift
      ;;
    --version|--out-dir|--config-file|--device-id|--org-name|--token|--backend-url|--gateway-url|--chrome-extension-id|--chrome-update-url|--firefox-extension-id|--firefox-install-url)
      ARTIFACT_ARGS+=("$1" "$2")
      shift 2
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

if [[ "${RUN_RUNTIME}" -eq 1 ]]; then
  bash "${ROOT_DIR}/scripts/rebuild-runtime.sh" "${RUNTIME_ARGS[@]}"
fi

if [[ "${RUN_ARTIFACTS}" -eq 1 ]]; then
  bash "${ROOT_DIR}/scripts/rebuild-artifacts.sh" "${ARTIFACT_ARGS[@]}"
fi

echo "==> Full rebuild complete."
