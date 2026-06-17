# HTTPS DLP Interception + Real-App Validation

## 1) Start deterministic protocol targets

```bash
./scripts/run-protocol-targets.sh
```

Endpoints:
- HTTPS echo: `https://127.0.0.1:18443/echo`
- Browser harness: `https://127.0.0.1:18443/browser`
- WSS echo: `wss://127.0.0.1:18443/ws`
- gRPC health: `127.0.0.1:18444` (TLS)

Optional synthetic smoke traffic:

```bash
./scripts/protocol-targets-smoke.sh
```

## 2) Agent interception config

Set these keys in `agent.json`:

```json
{
  "https_intercept_enabled": true,
  "https_intercept_domains": [
    "api.openai.com",
    "chat.openai.com",
    "127.0.0.1"
  ],
  "https_intercept_fail_mode": "fail_open",
  "https_intercept_protocols": ["http", "websocket", "grpc"],
  "https_intercept_capture_mode": "encrypted_full_body"
}
```

## 3) Restart commands

### Local containers

```bash
docker compose build gateway backend
docker compose up -d gateway backend
docker compose logs --tail=200 gateway
docker compose logs --tail=200 backend
```

### macOS agent

```bash
make build-agent-darwin
./bin/agent-darwin-arm64 -config ./agent.json
```

### Windows agent

```powershell
go build -o .\bin\agent-windows-amd64.exe .\cmd\agent
.\bin\agent-windows-amd64.exe -config .\agent.json
```

## 4) Required real-app matrix (A+B+C superset)

Use the same scenarios for all clients below:
- Baseline success with interception enabled.
- DLP alert: sensitive outbound content, request succeeds, DLP event recorded.
- DLP block: sensitive outbound content, request blocked, action=block recorded.
- Non-allowlisted destination: no decryption, normal behavior.
- Fail-open simulation: force interception failure and confirm bypass with telemetry reason.

### A) Browsers
- Chrome
- Safari
- Firefox
- Edge

Browser workflow:
1. Open `https://127.0.0.1:18443/browser`.
2. Send HTTPS payload with SSN/API key text.
3. Send WSS payload.
4. Validate UI still functions while DLP events appear in dashboard.

### B) Desktop AI clients
- ChatGPT Desktop
- Cursor
- VS Code AI extension

Client workflow:
1. Configure system proxy to agent.
2. Submit prompts with known sensitive markers (SSN/API keys).
3. Validate alert/block behavior in-client and DLP event metadata in dashboard/API.

### C) Non-AI bypass validation
- Slack
- Notion
- Teams

Workflow:
1. Use normal app flows (messages/docs/calls) to non-allowlisted domains.
2. Confirm no interception metadata/DLP decrypt behavior for those flows.

## 5) Evidence collection checklist

Collect all of:
- Dashboard:
  - AI Usage
  - DLP Events
- Backend API:
  - `GET /api/v1/dlp/events`
  - `GET /api/v1/dlp/summary`
  - `GET /api/v1/ai-usage`
- Logs:
  - Agent logs
  - `docker compose logs --tail=200 gateway`
- Metrics:
  - intercept success/fail-open
  - skipped inspection counts
  - protocol scan counts
  - block/alert counts

Expected DLP event fields:
- `protocol`
- `intercepted_https`
- `inspection_quality`
- `inspection_skip_reason`
- `direction` (`outbound`)
- encrypted request body presence for matches
