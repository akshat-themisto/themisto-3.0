# E1 — Agent Core Implementation Plan

> OS-agnostic core for the AI Traffic Governance Agent.
> Gateway runs on Ubuntu over strict mTLS.
> No OS-specific logic. No implementation code.

---

## 1. Core Package Structure

```
core/
├── agent.go                  # Top-level Agent struct; composes all modules; owns startup/shutdown
│
├── adapter/
│   └── iface/                # Frozen — OS adapter contracts (SystemProxy, CertStore, ProcessResolver, ServiceManager, NetworkInfo)
│       ├── adapter.go
│       ├── system_proxy.go
│       ├── cert_store.go
│       ├── process_resolver.go
│       ├── service.go
│       ├── network_info.go
│       └── errors.go
│
├── config/
│   ├── iface.go              # Frozen — Source, Provider interfaces
│   ├── manager.go            # ConfigManager: loads from Source, validates, exposes Provider, watches for changes
│   ├── defaults.go           # Default values for every AgentConfig field
│   └── validate.go           # Validation rules (gateway URL format, port range, TLS settings, etc.)
│
├── domain/
│   ├── types.go              # Frozen — AgentConfig, TLSConfig, PolicyPayload, RequestContext, Decision, etc.
│   └── protocol.go           # Frozen — ProtocolVersion = "2025.01"
│
├── identity/
│   ├── iface.go              # Frozen — Store interface
│   ├── store.go              # IdentityStore: manages mTLS credentials; delegates persistence to iface.CertStore
│   └── tls.go                # Builds *tls.Config from stored credentials for gateway connections
│
├── policy/
│   ├── iface.go              # Frozen — Fetcher, Engine interfaces
│   ├── fetcher.go            # PolicyFetcher: polls gateway /policy endpoint via GatewayClient.Do; returns PolicyPayload
│   ├── engine.go             # PolicyEngine: holds parsed rules in memory; evaluates RequestContext → Decision
│   └── sync.go               # PolicySyncLoop: background goroutine; calls Fetcher, feeds Engine.Update, emits telemetry
│
├── routing/
│   ├── iface.go              # Frozen — Router interface
│   └── router.go             # DefaultRouter: composes Config + PolicyEngine; resolves Decision for each request
│
├── transport/
│   ├── iface.go              # Frozen — LocalProxy, GatewayClient interfaces
│   ├── proxy.go              # HTTPProxy: net/http reverse proxy; handles CONNECT tunneling; calls Router per request
│   ├── gateway.go            # MtlsGatewayClient: persistent *http.Client with mTLS *tls.Config from Identity
│   └── protocol.go           # Wrapper protocol builder: wraps outbound requests with agent metadata headers
│
├── telemetry/
│   ├── iface.go              # Frozen — Metrics, Events interfaces
│   ├── collector.go          # TelemetryCollector: in-memory ring buffer; batches metrics + events for gateway push
│   └── emitter.go            # GatewayEmitter: periodically flushes buffered telemetry to gateway /telemetry endpoint
│
└── lifecycle.go              # Startup/shutdown orchestration helpers (phase runner, graceful drain)
```

### Dependency DAG (no cycles)

```
domain  →  (stdlib only)
iface   →  domain
config  →  domain
identity → domain, iface (CertStore)
telemetry → domain
policy  →  domain, transport (GatewayClient.Do), telemetry
routing →  domain, config, policy (Engine)
transport → domain, identity (TLS), routing (Router), telemetry
agent   →  all of the above + iface (OSAdapter)
```

No package under `core/` imports `adapter/darwin`, `adapter/windows`, `runtime.GOOS`, `syscall`, or any platform package.

---

## 2. Local HTTP Proxy Handler

### Responsibilities

The `transport/proxy.go` module implements `LocalProxy`. It is the ingress point for all local application traffic once the OS adapter registers the system proxy.

### Design

| Concern | Approach |
|---|---|
| HTTP requests | Standard reverse-proxy handler via `net/http`. Intercepts request, builds `domain.RequestContext`, calls Router, acts on Decision. |
| HTTPS CONNECT | Implements HTTP CONNECT method. On receiving CONNECT, establishes a TCP tunnel. Before tunneling, builds a `RequestContext` from the CONNECT target host:port for routing decisions. |
| Process attribution | On each incoming connection, extracts the client's local addr:port from `net.Conn`. Calls `iface.ProcessResolver.ResolveByConnection(clientAddr, clientPort, proxyAddr, proxyPort)` to get `ProcessInfo`. Attaches to `RequestContext`. |
| Decision: Forward | Wraps request using the protocol builder (Section 3) and relays via `GatewayClient.Relay`. Copies gateway response back to the original client. |
| Decision: Bypass | Dials the upstream directly (no gateway) using a plain `net/http` transport. Returns the response to the client. Core handles this without OS-specific code. |
| Decision: Block | Returns HTTP 403 with a configurable block page body from `AgentConfig`. For CONNECT, closes the connection immediately. |
| Listener lifecycle | Binds to `addr` from config (default `127.0.0.1:8080`). Uses `context.Context` for shutdown; `Stop()` calls `http.Server.Shutdown` with a drain timeout. |
| Connection limits | Configurable max concurrent connections via `AgentConfig`. Excess connections receive HTTP 503. |
| Timeouts | Per-request timeout from config. Idle connection timeout for keep-alive. Read/write header timeouts on the `http.Server`. |

### Error Handling

- If `ProcessResolver` returns `ErrUnsupported` or `ErrNotFound`, the request proceeds with a zero-value `ProcessInfo`. Routing and policy operate in "unknown process" mode.
- If `Router` returns an error, the proxy returns HTTP 502 and emits an error event via Telemetry.
- If `GatewayClient.Relay` fails, the proxy applies the retry strategy (Section 9) before returning 502.

---

## 3. Wrapper Protocol Builder

### Purpose

Every request the agent forwards to the gateway must carry agent metadata so the gateway can enforce policy, attribute traffic, and perform audit logging. The protocol builder constructs this metadata envelope.

### Metadata Headers

All custom headers use the `X-Themisto-` prefix. The protocol version header is always present.

| Header | Source | Example |
|---|---|---|
| `X-Themisto-Protocol-Version` | `domain.ProtocolVersion` | `2025.01` |
| `X-Themisto-Agent-ID` | `AgentConfig.AgentID` | `a3f8c1...` (UUID) |
| `X-Themisto-Request-ID` | Generated per request (UUID v4) | `d7e2a0...` |
| `X-Themisto-Timestamp` | `time.Now().UTC().Format(time.RFC3339Nano)` | `2025-06-15T12:34:56.789Z` |
| `X-Themisto-Process-Name` | `ProcessInfo.Name` | `curl` |
| `X-Themisto-Process-PID` | `ProcessInfo.PID` (string) | `12345` |
| `X-Themisto-Process-Signed` | `ProcessInfo.Signed` (bool string) | `true` |
| `X-Themisto-Process-Signer` | `ProcessInfo.SignerID` | `Apple Inc.` |
| `X-Themisto-Process-Bundle` | `ProcessInfo.BundleID` | `com.apple.Safari` |
| `X-Themisto-Policy-Version` | Last applied policy version string | `v42` |
| `X-Themisto-Decision` | Routing decision for audit trail | `forward` |
| `X-Themisto-Rule-ID` | The rule ID that produced the decision | `rule-17` |
| `X-Themisto-Network-Interface` | `NetworkInfo.PrimaryInterface()` | `en0` |
| `X-Themisto-VPN-Active` | `NetworkInfo.IsVPNActive()` (bool string) | `false` |

Executable paths and OS usernames are intentionally excluded because central telemetry
must not contain local file paths or infer a corporate identity from a local account.

### Construction Rules

1. Headers with empty or zero-value sources are omitted (gateway treats absence as "unknown").
2. The builder never reads or forwards `Authorization`, `Cookie`, `Set-Cookie`, or other sensitive client headers beyond what the original request already contains.
3. The builder operates on `domain.ProxyRequest`; it does not touch `net/http` types directly (transport converts between the two).
4. The builder is a pure function: `func BuildWrapperHeaders(cfg *AgentConfig, proc ProcessInfo, net NetworkInfo, policyVersion string, decision Decision, ruleID string) map[string]string`.

---

## 4. mTLS Transport Client

### Purpose

`transport/gateway.go` implements `GatewayClient`. It is the sole network egress point from the agent to the Ubuntu gateway. All communication uses mutual TLS.

### Design

| Concern | Approach |
|---|---|
| TLS configuration | Obtained from `identity.Store.GetGatewayTLSConfig()` which returns `domain.TLSConfig`. Converted to `*crypto/tls.Config` in `identity/tls.go`. Minimum TLS 1.3. |
| Client certificate | Loaded from `TLSConfig`. Rotated via `identity.Store.RotateCredentials`. Gateway validates client cert on every handshake. |
| CA trust | Gateway's CA cert installed by the core at startup via `iface.CertStore.InstallCA`. The `*tls.Config.RootCAs` pool includes only the gateway CA — no system roots. |
| HTTP client | A single `*http.Client` with the mTLS transport. Connection pooling enabled (default `MaxIdleConnsPerHost = 10`). |
| Relay method | `Relay(ctx, *ProxyRequest) → *ProxyResponse`. Streams request body to gateway, streams response body back. Supports chunked transfer. |
| Do method | `Do(ctx, *HTTPRequest) → *HTTPResponse`. For control-plane calls (policy sync, telemetry push, credential rotation). JSON request/response. |
| Connection reuse | HTTP/2 over mTLS for multiplexing. Falls back to HTTP/1.1 keep-alive if gateway does not support H2. |
| Health check | Periodic `Do` call to gateway `/healthz`. If unhealthy, marks client as degraded; proxy falls back to bypass-mode per config. |
| Certificate rotation | When `identity.Store` rotates credentials, the gateway client rebuilds its `*tls.Config` and replaces the HTTP transport. In-flight requests complete on the old transport; new requests use the new one. |
| Close | Drains in-flight requests (up to shutdown timeout), closes idle connections, releases the transport. |

### Gateway Endpoint Assumptions (Ubuntu)

| Endpoint | Method | Purpose |
|---|---|---|
| `/relay` | POST | Proxied traffic relay |
| `/policy` | GET | Policy fetch |
| `/telemetry` | POST | Metrics and event push |
| `/healthz` | GET | Gateway health check |
| `/identity/rotate` | POST | Credential rotation |

---

## 5. Policy Sync Client

### Purpose

`policy/fetcher.go` implements `Fetcher`. `policy/sync.go` runs the background sync loop that keeps the in-memory policy engine current with the gateway.

### Fetcher Design

- Calls `GatewayClient.Do` against the gateway `/policy` endpoint.
- Sends the current policy version as an `If-None-Match` / `X-Themisto-Policy-Version` header. Gateway returns 304 if unchanged (no body transfer).
- Deserializes the response into `domain.PolicyPayload`.
- Returns `(payload, version, error)`.
- `PollInterval()` reads the interval from `AgentConfig.PolicySyncInterval` (default: 60s). The gateway may override via a `Retry-After` or `X-Themisto-Poll-Interval` response header.

### Sync Loop Design

- Runs in a dedicated goroutine started by `Agent.Start`.
- Timer-based: fires every `PollInterval()`.
- On successful fetch: calls `Engine.Update(payload)`, emits `policy.updated` telemetry event.
- On fetch error: applies retry/backoff (Section 9), emits `policy.sync_failed` event with error detail.
- On 304 (no change): no-op, resets backoff.
- On shutdown: exits cleanly via context cancellation.
- On gateway `upgrade_required` response: emits `agent.upgrade_required` event, logs at WARN, continues operating with last-known policy.

### Engine Design

- `engine.go` implements `Engine`.
- `Update(payload)`: parses `PolicyPayload` into an internal rule set. Rules are ordered by priority. The update is atomic — either all rules replace the old set, or none do (parse error → keep old rules, return error).
- `Apply(ctx *RequestContext) → (Decision, ruleID, error)`: iterates rules in priority order, evaluates conditions against `RequestContext` fields (host, path, method, process info). Returns the first matching rule's decision. If no rule matches, returns a configurable default decision from `AgentConfig` (typically `DecisionForward`).

---

## 6. Telemetry Emitter

### Purpose

`telemetry/collector.go` buffers metrics and events in memory. `telemetry/emitter.go` flushes them to the gateway periodically.

### Collector Design

- Implements `Metrics` and `Events` interfaces.
- In-memory ring buffer with configurable capacity (default: 10,000 entries).
- `Counter`, `Gauge`, `Histogram`: append a timestamped metric entry to the buffer.
- `Emit`: appends a timestamped event entry to the buffer.
- Thread-safe via a mutex or lock-free ring.
- When the buffer is full, oldest entries are dropped (lossy under pressure, never blocks callers).

### Emitter Design

- Runs in a dedicated goroutine started by `Agent.Start`.
- Flush interval from `AgentConfig.TelemetryFlushInterval` (default: 30s).
- On flush: drains the buffer, serializes entries into a batch JSON payload, calls `GatewayClient.Do` to POST to `/telemetry`.
- On flush error: entries are re-queued (up to one retry cycle), then dropped if still failing.
- On shutdown: performs a final synchronous flush with a short timeout (5s) before returning.

### Standard Metric Names

| Metric | Type | Description |
|---|---|---|
| `proxy.requests.total` | Counter | Total requests received by the local proxy |
| `proxy.requests.forwarded` | Counter | Requests forwarded to gateway |
| `proxy.requests.bypassed` | Counter | Requests bypassed directly |
| `proxy.requests.blocked` | Counter | Requests blocked by policy |
| `proxy.latency.ms` | Histogram | End-to-end proxy latency |
| `gateway.relay.latency.ms` | Histogram | Gateway relay round-trip time |
| `gateway.health` | Gauge | 1 = healthy, 0 = unhealthy |
| `policy.version` | Gauge | Numeric policy version (for change detection) |
| `policy.sync.errors` | Counter | Policy sync failures |

### Standard Event Names

| Event | When |
|---|---|
| `agent.started` | Agent startup complete |
| `agent.stopped` | Agent shutdown complete |
| `policy.updated` | New policy applied |
| `policy.sync_failed` | Policy fetch failed (after retries) |
| `identity.rotated` | mTLS credentials rotated |
| `proxy.tamper_detected` | System proxy settings changed externally |
| `agent.upgrade_required` | Gateway demands newer agent version |

---

## 7. Config Manager

### Purpose

`config/manager.go` implements `Provider`. It loads configuration from a `Source`, validates it, and exposes the effective config to all core modules.

### Lifecycle

1. **Load**: At startup, calls `Source.ConfigBytes()` to get raw bytes (JSON or YAML) and the origin description.
2. **Parse**: Deserializes bytes into `domain.AgentConfig`.
3. **Defaults**: Fills missing fields from `defaults.go`.
4. **Validate**: Runs validation rules from `validate.go`. Fails startup if critical fields are missing or invalid (gateway URL, listen address).
5. **Expose**: Stores the validated config in an `atomic.Value` for lock-free reads via `Get()`.
6. **Watch**: Accepts `OnChange` callbacks. If the source supports reload (e.g., file watcher in `cmd/`), the manager re-runs load → parse → defaults → validate and, on success, swaps the atomic value and notifies watchers. On validation failure, keeps the old config and logs a warning.

### Validation Rules

| Field | Rule |
|---|---|
| `GatewayURL` | Required. Must be a valid HTTPS URL. |
| `ListenAddr` | Required. Must be a valid `host:port`. Host must be loopback (`127.0.0.1` or `::1`). |
| `AgentID` | Required. Non-empty string (UUID format preferred). |
| `PolicySyncInterval` | Optional. If present, must be ≥ 10s and ≤ 1h. Default: 60s. |
| `TelemetryFlushInterval` | Optional. If present, must be ≥ 5s and ≤ 5m. Default: 30s. |
| `MaxConcurrentConns` | Optional. If present, must be ≥ 1 and ≤ 65535. Default: 1000. |
| `ShutdownTimeout` | Optional. Default: 30s. |
| `DefaultDecision` | Optional. Must be one of `forward`, `bypass`, `block`. Default: `forward`. |
| `BlockPageBody` | Optional. Default: minimal HTML "request blocked" page. |

### Thread Safety

- `Get()` is lock-free (`atomic.Value`).
- `Watch()` and config reloads are serialized by a mutex protecting the watcher list.
- Watchers are called synchronously in registration order. Watchers must not block.

---

## 8. Startup / Shutdown Flow

### Startup Sequence

```
cmd/agent/main.go
  │
  ├── 1. Parse flags / env → select OS adapter via build tags
  ├── 2. Construct Logger implementation
  ├── 3. Construct OSAdapter (darwin or windows)
  ├── 4. Construct config.Source (file path from flags)
  │
  └── 5. Call core.NewAgent(OSAdapter, Source, Logger) → Agent
         │
         ├── 5a. ConfigManager.Load(Source) → validate → fail-fast if invalid
         ├── 5b. IdentityStore.Init(CertStore) → load existing creds or bootstrap
         │
         └── 6. Call Agent.Start(ctx) — main run loop
                │
                ├── Phase 1: IDENTITY
                │   └── IdentityStore.GetGatewayTLSConfig() → verify creds valid
                │       If no creds: attempt bootstrap via gateway enrollment
                │       On failure: fatal — agent cannot operate without mTLS
                │
                ├── Phase 2: GATEWAY
                │   └── GatewayClient.Init(tlsConfig) → connect + /healthz check
                │       On failure: retry with backoff (Section 9)
                │       After N failures: start in degraded mode (bypass-all)
                │
                ├── Phase 3: POLICY
                │   └── PolicyFetcher.Fetch() → initial policy load
                │   └── PolicyEngine.Update(payload) → activate rules
                │       On failure: start with empty policy (default decision applies)
                │   └── PolicySyncLoop.Start(ctx) → background goroutine
                │
                ├── Phase 4: TELEMETRY
                │   └── TelemetryEmitter.Start(ctx) → background goroutine
                │   └── Emit "agent.started" event
                │
                ├── Phase 5: PROXY
                │   └── LocalProxy.Listen(ctx, listenAddr) → start accepting connections
                │       On bind failure: fatal
                │
                ├── Phase 6: SYSTEM PROXY
                │   └── OSAdapter.SystemProxy.Register(ctx, host, port) → direct OS traffic to proxy
                │       On ErrPermission: log error, continue (user may have manual proxy)
                │       On ErrConflict: log warning, continue
                │
                └── Phase 7: INTEGRITY MONITOR
                    └── Start background goroutine: every 30s call VerifyIntegrity()
                        On tamper: emit "proxy.tamper_detected", attempt re-register
```

### Shutdown Sequence

Triggered by: context cancellation, OS signal (via ServiceManager), or explicit Stop call.

```
Agent.Stop(ctx)
  │
  ├── 1. OSAdapter.SystemProxy.Unregister(ctx)
  │      Restore original proxy settings.
  │      On error: log, continue (best-effort cleanup).
  │
  ├── 2. LocalProxy.Stop()
  │      Stop accepting new connections.
  │      Drain in-flight requests (up to ShutdownTimeout).
  │
  ├── 3. PolicySyncLoop.Stop()
  │      Cancel sync goroutine, wait for exit.
  │
  ├── 4. TelemetryEmitter.Flush() then Stop()
  │      Final synchronous flush to gateway.
  │      Cancel emitter goroutine, wait for exit.
  │      Emit "agent.stopped" event (best-effort).
  │
  ├── 5. GatewayClient.Close()
  │      Drain in-flight relay requests.
  │      Close idle connections.
  │
  └── 6. Log "agent shutdown complete"
```

### Failure Modes During Startup

| Failure | Behavior |
|---|---|
| Config invalid | Fatal. Log error and exit immediately. |
| No mTLS credentials | Fatal. Cannot communicate with gateway. |
| Gateway unreachable | Degraded start. Retry in background. Proxy starts in default-decision mode. |
| Initial policy fetch fails | Warning. Start with empty policy. Default decision applies to all requests. |
| Proxy bind fails | Fatal. Core function unavailable. |
| System proxy register fails | Warning. Agent runs but traffic is not redirected automatically. |

---

## 9. Retry / Backoff Strategy

### Algorithm

Exponential backoff with jitter, capped maximum, and circuit breaker.

### Parameters (from AgentConfig, with defaults)

| Parameter | Default | Description |
|---|---|---|
| `InitialBackoff` | 1s | First retry delay |
| `MaxBackoff` | 60s | Maximum retry delay |
| `BackoffMultiplier` | 2.0 | Exponential growth factor |
| `JitterFraction` | 0.2 | Random jitter ± 20% of computed delay |
| `MaxRetries` | 5 | Per-operation retry limit (0 = infinite for sync loops) |
| `CircuitBreakerThreshold` | 10 | Consecutive failures before circuit opens |
| `CircuitBreakerCooldown` | 120s | How long the circuit stays open before half-open probe |

### Backoff Computation

```
delay = min(InitialBackoff * (BackoffMultiplier ^ attempt), MaxBackoff)
jitter = delay * JitterFraction * (random float in [-1, 1])
actual_delay = delay + jitter
```

### Application Points

| Operation | Max Retries | Circuit Breaker | Notes |
|---|---|---|---|
| `GatewayClient.Relay` (per request) | 2 | Yes | Fast fail to avoid client timeout |
| `GatewayClient.Do` (control plane) | 5 | Yes | Policy sync, telemetry push |
| `PolicyFetcher.Fetch` (sync loop) | unlimited | Yes | Loop continues; circuit breaker prevents hammering |
| `TelemetryEmitter.Flush` | 3 | No | Drop data after retries (telemetry is best-effort) |
| `IdentityStore.RotateCredentials` | 5 | No | Critical path; log at ERROR on final failure |
| Gateway health check | unlimited | Yes | Periodic probe; circuit breaker gates reconnection |

### Circuit Breaker States

1. **Closed** (normal): requests flow through. Failure counter increments on each failure, resets on success.
2. **Open** (tripped): all requests fail immediately with a circuit-open error. Timer starts for cooldown.
3. **Half-Open** (probing): after cooldown, one request is allowed through. If it succeeds → Closed. If it fails → Open (reset cooldown timer).

### Context Awareness

- All retries respect `ctx.Done()`. If the context is canceled during a backoff sleep, the retry loop exits immediately.
- The `Relay` path uses the per-request context. The sync loops use the agent-lifetime context.

---

## 10. Concurrency Model

### Goroutine Inventory

| Goroutine | Owner | Lifetime | Purpose |
|---|---|---|---|
| Main | `Agent.Start` | Agent lifetime | Blocks on signal/context; orchestrates phases |
| HTTP listener | `LocalProxy.Listen` | Agent lifetime | Accepts incoming connections |
| Per-request handler | `net/http.Server` | Per request | Handles a single proxy request (managed by `http.Server`) |
| Policy sync loop | `PolicySyncLoop` | Agent lifetime | Periodic `Fetch` + `Engine.Update` |
| Telemetry emitter | `TelemetryEmitter` | Agent lifetime | Periodic flush to gateway |
| Integrity monitor | `Agent` | Agent lifetime | Periodic `VerifyIntegrity` check |
| Health checker | `GatewayClient` | Agent lifetime | Periodic `/healthz` probe |

### Synchronization Primitives

| Shared State | Protection | Access Pattern |
|---|---|---|
| `AgentConfig` (current) | `atomic.Value` | Write: ConfigManager on reload. Read: all modules, lock-free. |
| Policy rule set | `sync.RWMutex` | Write: `Engine.Update` (infrequent). Read: `Engine.Apply` on every request (hot path). |
| Telemetry ring buffer | Mutex or lock-free ring | Write: every request handler + sync loops. Read: emitter goroutine on flush. |
| mTLS `*tls.Config` | `atomic.Value` | Write: `IdentityStore` on rotation. Read: `GatewayClient` on every request. |
| Circuit breaker state | `sync.Mutex` | Write/Read: retry logic (low contention). |
| Gateway health flag | `atomic.Bool` | Write: health checker goroutine. Read: proxy handler (fast-path check). |

### Cancellation and Shutdown

- Every long-lived goroutine receives the agent-lifetime `context.Context`.
- On `Agent.Stop()`, the context is canceled. All goroutines observe `ctx.Done()` and exit.
- `sync.WaitGroup` tracks all background goroutines. `Stop()` waits on the WaitGroup with a timeout equal to `ShutdownTimeout`.
- Per-request goroutines are managed by `http.Server.Shutdown(ctx)` which drains gracefully.

### Hot Path Optimization

The per-request path (`connection accepted → ProcessResolver → Router → Relay/Bypass/Block → response`) must minimize allocations and lock contention:

1. `ProcessResolver.ResolveByConnection` — adapter-owned, expected sub-ms.
2. `Router.Route` → `Engine.Apply` — acquires `RWMutex` read lock. Since policy updates are rare (~1/min) and request volume is high, read locks are essentially uncontested.
3. `BuildWrapperHeaders` — pure function, no locks, allocates a `map[string]string`.
4. `GatewayClient.Relay` — HTTP/2 multiplexing avoids head-of-line blocking.
5. Config reads — `atomic.Value`, zero contention.
6. Telemetry writes — append to ring buffer under a short-held lock or lock-free CAS.

### No OS-Specific Concurrency

The core does not use platform-specific synchronization (`syscall.Mutex`, `windows.Event`, `darwin.dispatch_semaphore`). All concurrency is via Go's standard library: `context`, `sync`, `atomic`, channels.

---

## Appendix: Module Dependency Matrix

| Module | Depends On | Depended On By |
|---|---|---|
| `domain` | stdlib | everything |
| `adapter/iface` | domain | agent, transport, identity, routing |
| `config` | domain | agent, routing, transport, policy, telemetry |
| `identity` | domain, iface.CertStore | agent, transport |
| `telemetry` | domain | agent, policy, transport |
| `policy` | domain, transport.GatewayClient, telemetry | agent, routing |
| `routing` | domain, config, policy.Engine | agent, transport |
| `transport` | domain, identity, routing, telemetry | agent, policy |
| `agent` | all core modules, iface.OSAdapter | cmd |
