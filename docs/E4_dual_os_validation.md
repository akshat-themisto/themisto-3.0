# E4 — Dual OS Enforcement Validation Plan

> Integration plan to validate macOS and Windows agents against the Ubuntu gateway.
> Covers mTLS enforcement, gateway downtime handling, integration tests, acceptance criteria, and logging requirements.
> No feature expansion.

---

## 1. Validation Matrix

All tests must pass for both OS platforms against the same Ubuntu gateway instance.

| ID | Category | macOS | Windows | Priority |
|----|----------|-------|---------|----------|
| T1.x | mTLS Handshake | Yes | Yes | P0 |
| T2.x | Proxy Registration | Yes | Yes | P0 |
| T3.x | Traffic Forwarding | Yes | Yes | P0 |
| T4.x | Policy Enforcement | Yes | Yes | P0 |
| T5.x | Gateway Downtime | Yes | Yes | P0 |
| T6.x | Telemetry Delivery | Yes | Yes | P1 |
| T7.x | Certificate Lifecycle | Yes | Yes | P1 |
| T8.x | Process Attribution | Yes | Yes | P1 |
| T9.x | Tamper Detection | Yes | Yes | P2 |
| T10.x | Uninstall Cleanup | Yes | Yes | P2 |

---

## 2. Test Environment

### Infrastructure

| Component | Specification |
|---|---|
| **Gateway** | Ubuntu 22.04 LTS, Go gateway binary, strict mTLS only (no plaintext fallback) |
| **macOS Agent** | macOS 14.x (Sonoma), Apple Silicon or Intel, clean VM/bare metal |
| **Windows Agent** | Windows 11 23H2, amd64, clean VM |
| **Network** | All three machines on the same LAN or VPN; no NAT between agent and gateway |
| **DNS** | Gateway reachable via hostname (e.g., `gateway.themisto.test`) with valid DNS or `/etc/hosts` entry |
| **Time sync** | NTP configured on all machines (mTLS cert validation is time-sensitive) |

### Certificate Infrastructure

| Certificate | Purpose | Lifetime |
|---|---|---|
| Gateway CA | Root CA that signs gateway server cert and agent client certs | 10 years (test CA) |
| Gateway server cert | Gateway's TLS server identity, signed by Gateway CA | 1 year |
| macOS agent client cert | mTLS client identity for macOS agent, signed by Gateway CA | 90 days |
| Windows agent client cert | mTLS client identity for Windows agent, signed by Gateway CA | 90 days |
| Rogue CA | Self-signed CA NOT in the trust chain, for negative tests | 1 year |
| Rogue client cert | Client cert signed by Rogue CA, for rejection tests | 90 days |

### Test Data

| Item | Description |
|---|---|
| Test policy | Policy payload with rules: allow `*.example.com`, block `*.blocked.test`, bypass `*.bypass.test` |
| Test URLs | `https://allowed.example.com/`, `https://app.blocked.test/`, `https://internal.bypass.test/` |
| Test binary (signed) | A minimal HTTP client binary, code-signed (macOS: Apple Developer, Windows: Authenticode) |
| Test binary (unsigned) | Same binary without code signature, for policy tests that check signing |

---

## 3. Integration Tests

### T1: mTLS Handshake Validation

#### T1.1 — Successful mTLS Connection

**Precondition:** Agent has valid client cert signed by Gateway CA. Gateway CA is installed in the agent's trust store.

**Steps:**
1. Start the agent.
2. Agent connects to gateway `/healthz` endpoint.
3. Observe TLS handshake.

**Expected:**
- TLS 1.3 negotiated.
- Gateway presents its server certificate; agent validates it against Gateway CA.
- Agent presents its client certificate; gateway validates it against Gateway CA.
- `/healthz` returns HTTP 200.

**Acceptance criteria:**
- Handshake completes in < 500ms on LAN.
- Agent logs: `"gateway health check passed"` at INFO.
- Gateway logs: client certificate subject, serial number, and validation result.

---

#### T1.2 — Agent with Expired Client Certificate

**Precondition:** Agent has a client cert with `NotAfter` in the past.

**Steps:**
1. Start the agent with the expired cert.
2. Agent attempts to connect to gateway.

**Expected:**
- TLS handshake fails.
- Gateway rejects the expired client cert.
- Agent receives a TLS error (e.g., `certificate has expired`).
- Agent logs: `"mTLS handshake failed: certificate expired"` at ERROR.
- Agent does NOT start the proxy listener.
- Agent emits `agent.startup_failed` telemetry event (best-effort, may fail since gateway is unreachable for telemetry too).

**Acceptance criteria:**
- Agent exits with non-zero status or enters degraded mode (per config).
- No traffic is proxied without valid mTLS.

---

#### T1.3 — Agent with Wrong CA (Rogue CA)

**Precondition:** Agent has a client cert signed by Rogue CA (not the Gateway CA).

**Steps:**
1. Start the agent with the rogue client cert.
2. Agent attempts to connect to gateway.

**Expected:**
- Gateway rejects the unknown client cert.
- TLS handshake fails with `unknown certificate authority` or `bad certificate`.
- Agent logs the rejection.

**Acceptance criteria:**
- Zero bytes of application data exchanged.
- Gateway access log shows the rejected handshake with client cert details.

---

#### T1.4 — Agent Without Client Certificate

**Precondition:** Agent is configured with no client certificate.

**Steps:**
1. Start the agent without mTLS credentials.

**Expected:**
- Gateway requires client cert (strict mTLS).
- TLS handshake fails.
- Agent logs: `"mTLS handshake failed: no client certificate"` at ERROR.

**Acceptance criteria:**
- Agent refuses to start proxy without valid identity.

---

#### T1.5 — Gateway with Wrong Server Certificate

**Precondition:** Gateway presents a server cert signed by a CA not trusted by the agent.

**Steps:**
1. Reconfigure gateway with a server cert signed by Rogue CA.
2. Start the agent with valid client cert but Gateway CA that does NOT match.

**Expected:**
- Agent rejects the gateway's server certificate.
- TLS handshake fails with `certificate signed by unknown authority`.
- Agent logs the rejection.

**Acceptance criteria:**
- Agent does not send any application data to the untrusted server.

---

### T2: Proxy Registration

#### T2.1 — Successful System Proxy Registration

**Steps:**
1. Start the agent.
2. Agent registers system proxy.

**Expected (macOS):**
- `networksetup -getwebproxy Wi-Fi` shows agent's host:port.
- `networksetup -getsecurewebproxy Wi-Fi` shows agent's host:port.

**Expected (Windows):**
- `HKCU\...\Internet Settings\ProxyEnable` = 1.
- `HKCU\...\Internet Settings\ProxyServer` contains agent's host:port.
- `netsh winhttp show proxy` shows agent's host:port.

**Acceptance criteria:**
- Proxy is active within 2 seconds of agent startup.
- Agent logs proxy registration at INFO.
- `SystemProxy.State()` returns `Active=true, OwnedByAgent=true`.

---

#### T2.2 — Proxy Registration with Existing External Proxy

**Precondition:** A different proxy (e.g., `10.0.0.1:3128`) is already configured in the OS.

**Steps:**
1. Start the agent.

**Expected:**
- Agent detects the existing proxy.
- Agent returns `ErrConflict` from `Register`.
- Agent logs a warning: `"external proxy already configured, skipping registration"`.
- Agent starts but does NOT override the external proxy.

**Acceptance criteria:**
- Original proxy settings remain unchanged.
- Agent operates but traffic is not automatically redirected (manual configuration needed).

---

### T3: Traffic Forwarding

#### T3.1 — HTTP Request Forwarded to Gateway

**Steps:**
1. Agent is running with proxy registered.
2. Use `curl --proxy http://127.0.0.1:8080 http://allowed.example.com/`.

**Expected:**
- Request arrives at the gateway via mTLS.
- Gateway receives wrapper headers (`X-Themisto-Protocol-Version`, `X-Themisto-Agent-ID`, etc.).
- Gateway forwards the request to the upstream.
- Response is returned to `curl`.

**Acceptance criteria:**
- Response status matches upstream's response.
- Wrapper headers are present in gateway's access log.
- Round-trip latency < 200ms overhead above direct connection on LAN.

---

#### T3.2 — HTTPS Request Forwarded via CONNECT Tunnel

**Steps:**
1. Use `curl --proxy http://127.0.0.1:8080 https://allowed.example.com/`.

**Expected:**
- Agent receives CONNECT request for `allowed.example.com:443`.
- Agent builds `RequestContext` from the CONNECT target.
- Router decides `Forward`.
- Agent relays the CONNECT tunnel to the gateway.
- Gateway establishes the upstream TLS connection.
- Response is returned end-to-end.

**Acceptance criteria:**
- TLS to upstream is not terminated by the agent (transparent tunnel).
- Gateway log shows the CONNECT target.

---

#### T3.3 — Blocked Request

**Steps:**
1. Use `curl --proxy http://127.0.0.1:8080 https://app.blocked.test/`.

**Expected:**
- Router evaluates policy → `DecisionBlock`.
- Agent returns HTTP 403 with the block page body.
- No request reaches the gateway.

**Acceptance criteria:**
- Response body matches configured `BlockPageBody`.
- Agent telemetry: `proxy.requests.blocked` counter incremented.
- Agent log: blocked request at INFO with target host (no path or headers logged).

---

#### T3.4 — Bypassed Request

**Steps:**
1. Use `curl --proxy http://127.0.0.1:8080 https://internal.bypass.test/`.

**Expected:**
- Router evaluates policy → `DecisionBypass`.
- Agent connects directly to `internal.bypass.test` without going through the gateway.
- Response is returned to `curl`.

**Acceptance criteria:**
- Gateway access log does NOT contain an entry for this request.
- Agent telemetry: `proxy.requests.bypassed` counter incremented.

---

### T4: Policy Enforcement

#### T4.1 — Initial Policy Load

**Steps:**
1. Gateway has a policy with 3 rules (allow, block, bypass).
2. Start the agent.
3. Agent fetches policy during startup.

**Expected:**
- Agent calls gateway `/policy` endpoint.
- Receives `PolicyPayload` with version.
- `PolicyEngine.Update` succeeds.
- Agent logs: `"policy loaded, version=<v>, rules=3"` at INFO.

**Acceptance criteria:**
- Policy is active before the proxy starts accepting connections.

---

#### T4.2 — Policy Update During Runtime

**Steps:**
1. Agent is running with policy v1.
2. Update policy on gateway to v2 (add new block rule for `*.newblocked.test`).
3. Wait for next policy sync interval.

**Expected:**
- Agent fetches policy v2.
- `PolicyEngine.Update` replaces rules atomically.
- Agent logs: `"policy updated, version=v2"`.
- Subsequent requests to `*.newblocked.test` are blocked.

**Acceptance criteria:**
- Policy update is applied within `PolicySyncInterval + 5s`.
- No requests are dropped or errored during the policy swap.
- Telemetry event `policy.updated` is emitted.

---

#### T4.3 — Policy Fetch Failure (Gateway Temporarily Unavailable)

**Steps:**
1. Agent is running with policy v1.
2. Stop the gateway.
3. Wait for policy sync attempt.
4. Restart the gateway.
5. Wait for next successful sync.

**Expected:**
- Agent logs policy sync failure at WARN.
- Agent continues operating with policy v1 (last-known-good).
- After gateway restarts, next sync succeeds.
- No traffic disruption during the outage.

**Acceptance criteria:**
- Backoff between retry attempts is observable in logs.
- Telemetry event `policy.sync_failed` emitted on failure.
- Telemetry event `policy.updated` emitted on recovery.

---

### T5: Gateway Downtime Handling

#### T5.1 — Gateway Unreachable at Startup

**Steps:**
1. Ensure gateway is stopped.
2. Start the agent.

**Expected:**
- Agent attempts mTLS handshake → fails.
- Agent retries with exponential backoff.
- Agent starts in degraded mode:
  - Proxy listener is up.
  - All requests are handled per `DefaultDecision` (typically `forward`, which will fail, or `bypass`).
  - Or agent refuses to start (configurable).
- Agent logs: `"gateway unreachable, starting in degraded mode"` at WARN.

**Acceptance criteria:**
- Agent does not crash.
- Agent retries gateway connection in background.
- When gateway comes online, agent recovers automatically.

---

#### T5.2 — Gateway Goes Down During Operation

**Steps:**
1. Agent is running and forwarding traffic.
2. Stop the gateway.
3. Send a request through the proxy.

**Expected:**
- `GatewayClient.Relay` fails.
- Agent retries per the retry strategy (2 retries for relay).
- After retries exhausted, returns HTTP 502 to the client.
- Agent marks gateway as unhealthy (`gateway.health` gauge = 0).
- Agent logs the failure at ERROR.
- Subsequent requests continue to fail with 502 until gateway recovers.

**Acceptance criteria:**
- Client receives 502 within a bounded time (< 10s including retries).
- Agent does not crash or leak goroutines.
- Bypass and block decisions still function (only forward fails).

---

#### T5.3 — Gateway Recovery After Downtime

**Steps:**
1. Gateway is down; agent is in degraded mode.
2. Restart the gateway.
3. Wait for health check to detect recovery.

**Expected:**
- Agent's health checker detects `/healthz` success.
- Circuit breaker transitions from Open → Half-Open → Closed.
- `gateway.health` gauge = 1.
- Policy sync resumes and fetches latest policy.
- Subsequent relay requests succeed.
- Agent logs: `"gateway recovered"` at INFO.

**Acceptance criteria:**
- Recovery detected within `CircuitBreakerCooldown + HealthCheckInterval` (< 150s worst case).
- No manual intervention required.
- Telemetry gap during downtime is acceptable (best-effort).

---

#### T5.4 — Extended Gateway Outage (> 1 hour)

**Steps:**
1. Stop gateway for > 1 hour.
2. Monitor agent behavior.

**Expected:**
- Agent continues running in degraded mode.
- Retry backoff is capped at `MaxBackoff` (60s).
- Circuit breaker remains open, periodic half-open probes every `CircuitBreakerCooldown`.
- No memory leaks, no goroutine leaks, no log file growth explosion.
- Agent continues handling bypass and block decisions.

**Acceptance criteria:**
- Memory usage remains stable (within 10% of pre-outage baseline after 1 hour).
- Goroutine count remains stable.
- Log volume is bounded (backoff reduces log spam).

---

### T6: Telemetry Delivery

#### T6.1 — Metrics Reach Gateway

**Steps:**
1. Agent is running and processing requests.
2. Wait for telemetry flush interval.

**Expected:**
- Agent POSTs a batch of metrics to gateway `/telemetry`.
- Payload contains: `proxy.requests.total`, `proxy.requests.forwarded`, `proxy.latency.ms`, etc.

**Acceptance criteria:**
- Gateway receives and logs the telemetry payload.
- Metric values are non-zero and consistent with actual request volume.

---

#### T6.2 — Events Reach Gateway

**Steps:**
1. Start the agent.
2. Trigger a policy update.

**Expected:**
- Gateway receives `agent.started` event.
- Gateway receives `policy.updated` event.

**Acceptance criteria:**
- Events contain correct timestamps.
- Events contain the agent ID.

---

#### T6.3 — Telemetry During Gateway Downtime

**Steps:**
1. Stop gateway.
2. Agent generates telemetry.
3. Restart gateway.

**Expected:**
- Telemetry is buffered in the ring buffer.
- After recovery, buffered entries are flushed.
- If buffer overflows, oldest entries are dropped (no crash).

**Acceptance criteria:**
- No data loss if buffer capacity is not exceeded.
- Lossy behavior is documented and expected under pressure.

---

### T7: Certificate Lifecycle

#### T7.1 — CA Certificate Installation

**Steps:**
1. Agent installs Gateway CA into OS trust store during startup.

**Expected (macOS):**
- Certificate appears in System Keychain with label `"Themisto-CA-<id>"`.
- `security find-certificate -c "Themisto" /Library/Keychains/System.keychain` returns the cert.

**Expected (Windows):**
- Certificate appears in `Cert:\LocalMachine\Root`.
- `Get-ChildItem Cert:\LocalMachine\Root | Where-Object { $_.FriendlyName -like "Themisto-*" }` returns the cert.

**Acceptance criteria:**
- Certificate is trusted system-wide.
- `HasCA(id)` returns `true`.

---

#### T7.2 — Credential Rotation

**Steps:**
1. Agent is running with client cert v1.
2. Gateway issues a rotation command with new client cert v2 and new CA.
3. Agent calls `IdentityStore.RotateCredentials`.

**Expected:**
- New credentials are stored.
- Gateway client rebuilds TLS transport with new cert.
- In-flight requests complete on old transport.
- New requests use new credentials.
- Agent logs: `"credentials rotated"` at INFO.
- Telemetry event `identity.rotated`.

**Acceptance criteria:**
- Zero dropped requests during rotation.
- Gateway validates new client cert successfully.
- Old client cert is no longer used for new connections.

---

### T8: Process Attribution

#### T8.1 — Process Info Attached to Request

**Steps:**
1. Use a known signed application to make a request through the proxy (e.g., `curl` on macOS, `Invoke-WebRequest` on Windows).

**Expected:**
- Gateway receives wrapper headers with process info:
  - `X-Themisto-Process-Path`: path to the binary.
  - `X-Themisto-Process-Name`: binary name.
  - `X-Themisto-Process-PID`: PID (numeric).
  - `X-Themisto-Process-User`: OS user.
  - `X-Themisto-Process-Signed`: `true` or `false`.

**Acceptance criteria (macOS):**
- `Process-Path` matches output of `which curl`.
- `Process-Signed` is `true` for Apple-signed binaries.
- `Process-Bundle` is populated for `.app` bundles.

**Acceptance criteria (Windows):**
- `Process-Path` matches the full executable path.
- `Process-Signed` is `true` for Authenticode-signed binaries.

---

#### T8.2 — Unknown Process Handling

**Steps:**
1. If `ResolveByConnection` cannot find the process (e.g., race condition, process exited).

**Expected:**
- Request proceeds with zero-value `ProcessInfo`.
- Wrapper headers for process fields are omitted.
- No error returned to the client.

**Acceptance criteria:**
- Request is not blocked due to unknown process (unless policy explicitly requires known process).

---

### T9: Tamper Detection

#### T9.1 — Manual Proxy Change Detected

**Steps:**
1. Agent is running with proxy registered.
2. Manually change proxy settings via OS UI or CLI.
3. Wait for next integrity check (30s).

**Expected:**
- `VerifyIntegrity()` returns `intact=false`.
- Agent emits `proxy.tamper_detected` telemetry event.
- Agent logs: `"proxy settings tampered"` at WARN.
- If `AutoReregister` is enabled, agent re-registers the proxy.

**Acceptance criteria:**
- Detection within 60s of tamper.
- If auto-reregister succeeds, proxy is restored.

---

### T10: Uninstall Cleanup

#### T10.1 — Clean Uninstall Leaves No Artifacts

**Steps:**
1. Run full uninstall procedure.
2. Verify all artifacts removed.

**Expected (macOS):**
- No launchd plist.
- No binary at `/usr/local/bin/themisto-agent`.
- No config at `/etc/themisto/`.
- No logs at `/var/log/themisto/`.
- No Themisto certificates in System Keychain.
- Proxy settings restored to pre-install state.

**Expected (Windows):**
- No Windows Service `ThemistoAgent`.
- No files at `C:\Program Files\Themisto\`.
- No data at `C:\ProgramData\Themisto\`.
- No registry keys at `HKLM\SOFTWARE\Themisto\`.
- No Themisto certificates in `Cert:\LocalMachine\Root`.
- Proxy settings restored to pre-install state.

**Acceptance criteria:**
- Verification script returns zero artifacts found.

---

## 4. Logging Requirements

### Log Levels for Validation

All log messages relevant to validation must be emitted at the specified levels:

| Event | Level | Required Fields |
|---|---|---|
| Successful mTLS handshake | INFO | gateway_host, tls_version, client_cert_serial |
| Failed mTLS handshake | ERROR | gateway_host, error_detail, client_cert_subject (if available) |
| Proxy registered | INFO | host, port, network_services (macOS) / registry_keys (Windows) |
| Proxy registration conflict | WARN | existing_proxy_host, existing_proxy_port |
| Request forwarded | DEBUG | request_id, target_host, decision, rule_id, process_name, latency_ms |
| Request blocked | INFO | request_id, target_host, rule_id, process_name |
| Request bypassed | DEBUG | request_id, target_host, rule_id |
| Policy loaded/updated | INFO | policy_version, rule_count |
| Policy sync failed | WARN | error_detail, retry_attempt, next_retry_in |
| Gateway health check passed | DEBUG | latency_ms |
| Gateway health check failed | WARN | error_detail, consecutive_failures |
| Gateway recovered | INFO | downtime_duration_s |
| Telemetry flush success | DEBUG | batch_size, latency_ms |
| Telemetry flush failed | WARN | error_detail, entries_dropped |
| Credentials rotated | INFO | new_cert_serial, old_cert_serial |
| Proxy tamper detected | WARN | expected_host, expected_port, actual_host, actual_port |
| Agent started | INFO | agent_id, protocol_version, os, arch, agent_version |
| Agent stopped | INFO | uptime_s, total_requests |
| Circuit breaker opened | WARN | target (gateway), consecutive_failures |
| Circuit breaker closed | INFO | target (gateway) |

### Structured Logging Format

All logs must be structured (key-value pairs) for machine parsing. Example:

```
level=INFO msg="proxy registered" host=127.0.0.1 port=8080 service=Wi-Fi
level=WARN msg="policy sync failed" error="connection refused" attempt=3 next_retry_in=16s
level=INFO msg="request blocked" request_id=d7e2a0 target=app.blocked.test rule_id=rule-42 process=curl
```

### Log Retention for Tests

- During validation runs, log level must be set to DEBUG.
- Logs must be collected from both agent and gateway for correlation.
- Logs must be retained for the duration of the validation cycle (minimum 7 days).
- Agent logs must include a run ID (set via config or env var) so that logs from different test runs can be distinguished.

### Sensitive Data Rules

- NEVER log: request bodies, response bodies, `Authorization` headers, `Cookie` headers, private keys, raw certificate bytes.
- MAY log: target hostnames, IP addresses, port numbers, process paths, certificate serial numbers, certificate subjects (without private key material).
- Configurable: URL paths can be logged or redacted based on `AgentConfig.LogURLPaths` (default: redacted).

---

## 5. Acceptance Criteria Summary

### P0 (Must pass before any release)

| Test ID | Description | macOS | Windows |
|---------|-------------|-------|---------|
| T1.1 | Successful mTLS handshake | Pass | Pass |
| T1.2 | Expired cert rejected | Pass | Pass |
| T1.3 | Wrong CA rejected | Pass | Pass |
| T1.4 | No client cert rejected | Pass | Pass |
| T1.5 | Wrong server cert rejected | Pass | Pass |
| T2.1 | Proxy registration works | Pass | Pass |
| T3.1 | HTTP forwarding works | Pass | Pass |
| T3.2 | HTTPS CONNECT works | Pass | Pass |
| T3.3 | Block works | Pass | Pass |
| T3.4 | Bypass works | Pass | Pass |
| T4.1 | Initial policy load | Pass | Pass |
| T4.2 | Runtime policy update | Pass | Pass |
| T5.1 | Startup without gateway (degraded) | Pass | Pass |
| T5.2 | Gateway down during operation | Pass | Pass |
| T5.3 | Gateway recovery | Pass | Pass |

### P1 (Must pass before GA)

| Test ID | Description | macOS | Windows |
|---------|-------------|-------|---------|
| T4.3 | Policy fetch failure + recovery | Pass | Pass |
| T5.4 | Extended gateway outage stability | Pass | Pass |
| T6.1 | Metrics delivery | Pass | Pass |
| T6.2 | Events delivery | Pass | Pass |
| T6.3 | Telemetry during downtime | Pass | Pass |
| T7.1 | CA installation | Pass | Pass |
| T7.2 | Credential rotation | Pass | Pass |
| T8.1 | Process attribution | Pass | Pass |
| T8.2 | Unknown process handling | Pass | Pass |

### P2 (Should pass before GA)

| Test ID | Description | macOS | Windows |
|---------|-------------|-------|---------|
| T2.2 | Proxy conflict detection | Pass | Pass |
| T9.1 | Tamper detection | Pass | Pass |
| T10.1 | Clean uninstall | Pass | Pass |

---

## 6. Test Execution Procedure

### Pre-flight Checklist

1. Clean VMs provisioned (macOS, Windows, Ubuntu gateway).
2. All certificates generated and distributed.
3. Gateway binary deployed and configured for strict mTLS.
4. Test policy deployed on gateway.
5. NTP synchronized across all machines.
6. Agent binaries built for macOS (universal) and Windows (amd64).
7. Log collection configured (agent + gateway).
8. Run ID assigned for this validation cycle.

### Execution Order

1. **Gateway setup:** Deploy and verify gateway is running (`/healthz` responds 200 with a valid client cert via `curl --cert`).
2. **macOS pass:** Run T1 → T10 on macOS VM. Record results.
3. **Windows pass:** Run T1 → T10 on Windows VM. Record results.
4. **Cross-validation:** Run T3.1 from macOS and Windows simultaneously to verify gateway handles concurrent multi-platform clients.
5. **Downtime simulation:** Run T5.1–T5.4 with coordinated gateway stop/start.
6. **Cleanup pass:** Run T10 on both platforms.
7. **Report generation:** Collect all logs, compile pass/fail matrix, note any deviations.

### Failure Handling

- P0 failure: blocks release. Must fix and re-run the failed test.
- P1 failure: blocks GA but not beta/preview releases. Must fix before GA.
- P2 failure: tracked as known issue. Fix in subsequent release.
- Flaky test (passes on re-run): investigate root cause. If timing-dependent, increase timeouts and re-run 3x. If 3/3 pass, mark as flaky with a tracking issue.
