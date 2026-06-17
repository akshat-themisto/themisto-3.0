# Themisto Production Customer Onboarding (VPS Hosted)

This runbook covers customer delivery for the current release gate:
- Control plane hosted by provider on VPS.
- Browser GA scope: **Chrome + Firefox**.
- Safari is a follow-up managed milestone.
- Endpoint agent scope: **macOS + Windows**.

## 1. Provider Control Plane Prerequisites

1. Deploy `postgres`, `backend`, `gateway`, and `dashboard` on VPS.
2. Use public TLS certificates for external URLs.
3. Set these environment variables during deployment:
   - `PUBLIC_BACKEND_URL=https://control.yourdomain.com` (or `http://<ip>:8443` if no TLS on backend endpoint)
   - `PUBLIC_GATEWAY_URL=https://gateway.yourdomain.com`
4. Confirm health:
   - `GET <gateway-health-url>/healthz`
   - `GET <backend-health-url>/healthz`

Example deployment command:

```bash
sudo PUBLIC_BACKEND_URL=https://control.yourdomain.com \
     PUBLIC_GATEWAY_URL=https://gateway.yourdomain.com \
     bash scripts/deploy-vps.sh
```

## 2. Customer Requirements (What They Must Have)

1. Managed browser administration:
   - Chrome enterprise policy channel (Google Admin / MDM).
   - Firefox enterprise policy channel (`policies.json`, GPO, or MDM).
2. Endpoint privileges:
   - Local admin rights to install/start agent service on macOS and Windows.
3. Network:
   - Endpoint egress to public backend and gateway URLs over HTTPS.
   - Loopback allowed to `127.0.0.1:17175` (browser extension to local prompt-eval API).

## 3. Build Per-Device Endpoint Installers

### Dashboard-driven flow (recommended)

1. Sign in to dashboard.
2. Open **Devices**.
3. Click **New Enrollment** and create package.
4. Download per-device `agent.json`.
5. Build installers with embedded config:

```bash
make package-device-config \
  CONFIG_FILE=/path/to/agent-<device-id>.json \
  VERSION=<version>
```

Output:
- `dist/installers/device-<device-id>/ThemistoAgent-macos-<version>.pkg`
- `dist/installers/device-<device-id>/ThemistoAgent-windows-<version>.zip`
- `dist/installers/device-<device-id>/agent.json`

### API/token flow (alternative)

```bash
make package-device \
  DEVICE_ID=<device-id> \
  ORG_NAME="<org-name>" \
  ENROLLMENT_TOKEN=<one-time-token> \
  BACKEND_URL=<public-backend-url> \
  GATEWAY_URL=<public-gateway-url> \
  VERSION=<version>
```

## 4. Build Browser Artifacts + Managed Policies

Build extension artifacts:

```bash
make package-browser-extensions VERSION=<version>
```

Outputs:
- `dist/browser/extensions/themisto-prompt-capture-chromium-<version>.zip`
- `dist/browser/extensions/themisto-prompt-capture-firefox-<version>.xpi`

Render managed policy files:

```bash
make render-browser-policies \
  CHROME_EXTENSION_ID=<chrome-extension-id> \
  FIREFOX_INSTALL_URL=https://downloads.example.com/themisto/themisto-prompt-capture-firefox-<version>-signed.xpi
```

Outputs:
- `dist/browser/policies/chrome-enterprise-force-install.json`
- `dist/browser/policies/firefox-enterprise-policies.json`

Notes:
- Chrome production rollout assumes private CWS listing and force-install by extension ID.
- Firefox production rollout requires a **signed** XPI before force-install.

## 5. One-Shot Delivery Bundle (Recommended)

Build endpoint installers + browser artifacts + policies in one command:

```bash
bash scripts/build-customer-delivery-bundle.sh \
  --version <version> \
  --config-file /path/to/agent-<device-id>.json \
  --chrome-extension-id <chrome-extension-id> \
  --firefox-install-url https://downloads.example.com/themisto/themisto-prompt-capture-firefox-<version>-signed.xpi
```

Bundle output:
- `dist/customer-delivery/installers/device-<device-id>/...`
- `dist/customer-delivery/browser/extensions/...`
- `dist/customer-delivery/browser/policies/...`
- `dist/customer-delivery/DELIVERY_README.md`

## 6. Customer Endpoint Install Flow

### macOS

1. Open `ThemistoAgent-macos-<version>.pkg`.
2. Complete installer wizard with admin credentials.
3. Verify device appears in dashboard.

### Windows

1. Unzip `ThemistoAgent-windows-<version>.zip`.
2. Run `install.bat` and accept UAC prompt.
3. Verify device appears in dashboard.

## 7. Validation Checklist

1. VPS readiness:
   - Backend and gateway `/healthz` return success.
2. Browser validation on pilot devices (Chrome + Firefox):
   - Extension force-installed via enterprise policy.
   - Prompt interception works on supported AI surfaces.
   - Telemetry/audit entries visible in dashboard.
3. Endpoint validation:
   - Device enrolled and active after install.
   - Reissue token + reinstall path works.

## 8. Safari Milestone (Not in Current GA Gate)

Safari rollout remains a separate managed milestone:
1. Build signed/notarized Safari Web Extension app container.
2. Deliver MDM deployment profile/runbook for forced enablement.
3. Promote Safari to GA only after parity validation (interception + telemetry).
