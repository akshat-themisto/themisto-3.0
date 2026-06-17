# G3 — Gateway Service Implementation Plan

Go gateway service for Ubuntu Docker deployment. Handles mTLS termination, wrapper parsing, policy evaluation, telemetry emission, upstream forwarding, and backpressure.

---

## 1. Module Breakdown

```
gateway/
├── cmd/
│   └── gateway/
│       └── main.go              # Entrypoint: config load, wiring, signal handling
│
├── internal/
│   ├── config/
│   │   ├── config.go            # Config struct, loading, validation
│   │   └── defaults.go          # Default values
│   │
│   ├── tls/
│   │   ├── listener.go          # mTLS listener setup
│   │   ├── client_info.go       # Extract device/org identity from client cert
│   │   └── cert_verifier.go     # Custom verification (revocation check callback)
│   │
│   ├── wrapper/
│   │   ├── parser.go            # Parse agent wrapper envelope
│   │   ├── types.go             # WrapperEnvelope, WrapperMetadata
│   │   └── validator.go         # Validate wrapper fields, protocol version
│   │
│   ├── policy/
│   │   ├── engine.go            # Policy rule evaluation
│   │   ├── loader.go            # Load policy from DB or cache
│   │   ├── types.go             # PolicyRule, PolicySet, Decision
│   │   └── cache.go             # In-memory policy cache with TTL
│   │
│   ├── telemetry/
│   │   ├── emitter.go           # Write telemetry events to store
│   │   ├── types.go             # TelemetryEvent, RequestLog
│   │   ├── buffer.go            # Async write buffer (batch inserts)
│   │   └── metrics.go           # Prometheus metrics (request count, latency, etc.)
│   │
│   ├── upstream/
│   │   ├── forwarder.go         # Forward request to upstream target
│   │   ├── transport.go         # HTTP client pool, connection reuse
│   │   └── response.go          # Response relay back to agent
│   │
│   ├── backpressure/
│   │   ├── limiter.go           # Concurrency limiter (semaphore)
│   │   ├── circuit.go           # Circuit breaker per upstream
│   │   └── shedder.go           # Load shedding under pressure
│   │
│   ├── handler/
│   │   ├── proxy.go             # Main proxy handler (orchestrates pipeline)
│   │   └── health.go            # /healthz endpoint (no mTLS)
│   │
│   └── store/
│       ├── postgres.go          # PostgreSQL connection, queries
│       └── queries.go           # SQL query constants
│
├── go.mod
├── go.sum
├── Dockerfile
└── Makefile
```

### Module Responsibilities

| Module | Responsibility | Dependencies |
|--------|---------------|--------------|
| `config` | Load YAML/env config, validate, provide to all modules | None |
| `tls` | Set up mTLS listener, extract client identity | `config`, backend cert-status API |
| `wrapper` | Parse and validate agent protocol envelope | `config` (protocol version) |
| `policy` | Evaluate routing/access policy for each request | `store` (policy data), `config` |
| `telemetry` | Buffer and emit request logs and metrics | `store`, `config` |
| `upstream` | Forward requests to destination, relay responses | `config`, `backpressure` |
| `backpressure` | Rate limiting, circuit breaking, load shedding | `config`, `telemetry` (metrics) |
| `handler` | Orchestrate the request pipeline | All above |
| `store` | PostgreSQL access layer | `config` |

---

## 2. Request Lifecycle

```
                                    GATEWAY SERVICE
┌─────────┐    ┌─────────────────────────────────────────────────────────────┐
│  Agent   │    │                                                             │
│          │    │  ┌──────┐   ┌────────┐   ┌────────┐   ┌──────────┐         │
│  mTLS ───┼───►│  │ TLS  │──►│Wrapper │──►│Policy  │──►│Backpress.│         │
│  client  │    │  │Listen│   │ Parse  │   │  Eval  │   │  Check   │         │
│          │    │  └──────┘   └────────┘   └────────┘   └──────────┘         │
│          │    │      │           │            │              │              │
│          │    │      │           │            │              ▼              │
│          │    │      │           │            │       ┌──────────┐          │
│          │    │      │           │            │       │ Upstream │──► Target│
│          │    │      │           │            │       │ Forward  │          │
│          │    │      │           │            │       └──────────┘          │
│          │    │      │           │            │              │              │
│          │    │      ▼           ▼            ▼              ▼              │
│          │    │  ┌──────────────────────────────────────────────┐           │
│  ◄───────┼────│  │            Telemetry Emitter                │           │
│ response │    │  │  (async buffer → batch insert → PostgreSQL) │           │
│          │    │  └──────────────────────────────────────────────┘           │
└─────────┘    └─────────────────────────────────────────────────────────────┘
```

### 2.1 Step-by-Step

**Step 1: TLS Handshake (tls/listener.go)**
- Agent opens TCP connection to gateway port 443.
- Go `tls.Listener` performs TLS 1.3 handshake.
- Client certificate is required (`tls.RequireAndVerifyClientCert`).
- Custom `VerifyPeerCertificate` callback checks cert serial against backend revocation API (with cache).
- On success: extract `CN` (device_id) and `O` (org_id) from client cert into request context.
- On failure: connection is terminated at TLS level; no HTTP response.

**Step 2: HTTP Request Reception (handler/proxy.go)**
- HTTP/1.1 or HTTP/2 request received over the mTLS connection.
- Request context carries: `device_id`, `org_id`, `client_cert_serial`, `connection_time`.

**Step 3: Wrapper Parsing (wrapper/parser.go)**
- Agent wraps the original request in an envelope (custom header or body wrapper).
- Parser extracts:
  - `protocol_version`: Agent protocol version.
  - `original_request`: Method, URL, headers, body of the original request.
  - `metadata`: Source app (if available), agent version, OS, timestamps.
- Validation rejects:
  - Unknown protocol versions.
  - Missing required fields.
  - Body size exceeding configured maximum.

**Step 4: Policy Evaluation (policy/engine.go)**
- Input: `device_id`, `org_id`, `original_request` (host, path, method), `metadata`.
- Engine loads the active policy set for the org (from cache or DB).
- Evaluates rules in priority order:
  - **Allow**: Forward to upstream.
  - **Block**: Return 403 to agent with reason.
  - **Log-only**: Forward to upstream, flag for enhanced logging.
- Returns: `Decision` (allow/block/log), `matched_rule_id`, `policy_version`.

**Step 5: Backpressure Check (backpressure/limiter.go)**
- Before forwarding, check:
  1. **Concurrency semaphore**: Are we below max in-flight requests? If not → 503 with `Retry-After`.
  2. **Circuit breaker**: Is the target upstream circuit open? If open → 503.
  3. **Load shedder**: Is system load (CPU, goroutines, memory) above threshold? If yes → shed lowest-priority requests first.

**Step 6: Upstream Forwarding (upstream/forwarder.go)**
- Reconstruct the original HTTP request from the parsed wrapper.
- Forward to the target upstream using a pooled `http.Client` (with connection reuse).
- Timeouts: connect 5s, response header 30s, total 120s (configurable).
- Capture response: status code, headers, body.

**Step 7: Response Relay (upstream/response.go)**
- Wrap upstream response in gateway response envelope (if agent protocol requires it).
- Relay response back to agent over the mTLS connection.
- Stream large responses (don't buffer entire body in memory).

**Step 8: Telemetry Emission (telemetry/emitter.go)**
- After response is sent (or on error), emit a telemetry event:
  ```
  TelemetryEvent {
    timestamp, device_id, org_id,
    request_host, request_method, request_path,
    response_status, latency_ms,
    policy_decision, matched_rule_id, policy_version,
    bytes_sent, bytes_received,
    agent_version, protocol_version
  }
  ```
- Event is pushed to an async buffer (channel-based).
- Buffer flushes to PostgreSQL via batch INSERT every 1s or 500 events (whichever first).
- Prometheus metrics updated synchronously (counters, histograms).

---

## 3. Module Details

### 3.1 TLS Module

**Listener Setup:**
- Bind to `0.0.0.0:8443` (mapped to host 443 via Docker).
- Load server cert + key from mounted volume.
- Load CA chain for client verification.
- Set `MinVersion: tls.VersionTLS13`.
- Set `ClientAuth: tls.RequireAndVerifyClientCert`.

**Revocation Check:**
- `VerifyPeerCertificate` calls backend `/internal/cert-status/{serial}`.
- Response cached in-memory (map with mutex, 60s TTL).
- Cache eviction: LRU with max 10,000 entries.
- On backend unreachable: configurable fail-open (allow) or fail-closed (reject). Default: fail-closed.

**Client Identity Extraction:**
- From verified client cert: `CN` → device_id, `O` → org_id.
- Stored in `context.Context` via typed keys.
- Available to all downstream handlers.

### 3.2 Wrapper Module

**Envelope Format:**
```
POST /proxy HTTP/1.1
X-Themisto-Protocol: 2025.01
Content-Type: application/octet-stream

┌─────────────────────────────────┐
│ 4 bytes: metadata length (BE)  │
│ N bytes: JSON metadata         │
│ remaining: original HTTP req   │
└─────────────────────────────────┘
```

**Metadata JSON:**
```json
{
  "protocol_version": "2025.01",
  "agent_version": "0.1.0",
  "os": "darwin",
  "source_app": "com.example.browser",
  "request_id": "uuid"
}
```

**Parser outputs:** `WrapperEnvelope` containing metadata struct + `*http.Request` reconstructed from the raw request bytes.

### 3.3 Policy Module

**Policy Cache:**
- Keyed by `org_id`.
- Loaded on first request for an org, refreshed every 5 minutes.
- Stores `PolicySet`: ordered list of `PolicyRule`.

**Rule Evaluation:**
- Rules evaluated top-to-bottom (priority order).
- First match wins.
- Default rule (implicit): allow all (configurable to block all).

**Rule Fields:**
```
PolicyRule {
  id, priority, org_id,
  match_host (glob), match_path (prefix), match_method,
  decision (allow | block | log_only),
  enabled
}
```

### 3.4 Backpressure Module

**Concurrency Limiter:**
- Buffered channel as semaphore.
- Configurable max concurrency (default: 1000 in-flight requests).
- `TryAcquire(timeout)` → returns false if can't acquire within timeout.

**Circuit Breaker:**
- Per upstream host.
- States: closed (healthy) → open (failing) → half-open (probing).
- Open after 5 consecutive failures or >50% error rate in 30s window.
- Half-open: allow 1 request every 10s to probe.
- Close after 3 consecutive successes in half-open.

**Load Shedder:**
- Monitors `runtime.NumGoroutine()` and memory usage.
- When goroutines > threshold (e.g., 50,000) or memory > 80% of limit:
  - Reject new requests with 503.
  - Emit `gateway.load_shedding` metric.

### 3.5 Upstream Module

**HTTP Client Pool:**
- Single `http.Transport` with:
  - `MaxIdleConns: 200`
  - `MaxIdleConnsPerHost: 20`
  - `IdleConnTimeout: 90s`
  - `TLSHandshakeTimeout: 5s`
- Connection reuse across requests to the same upstream.

**Request Forwarding:**
- Strip Themisto-specific headers before forwarding.
- Preserve original `Host` header.
- Add `X-Forwarded-For` with agent's device identity (not IP, since mTLS).
- Stream request body (don't buffer).

**Response Handling:**
- Stream response body back through the mTLS connection.
- Capture status code and content-length for telemetry.
- Timeout: total request timeout enforced via `context.WithTimeout`.

### 3.6 Telemetry Module

**Async Buffer:**
- Buffered channel of `TelemetryEvent` (capacity: 10,000).
- Dedicated goroutine reads from channel.
- Flushes batch INSERT to PostgreSQL every 1s or when buffer reaches 500 events.
- If channel is full: drop events (increment `telemetry.events_dropped` counter).

**Prometheus Metrics:**
- `gateway_requests_total{org, decision, status}` — counter.
- `gateway_request_duration_seconds{org}` — histogram.
- `gateway_upstream_duration_seconds{upstream_host}` — histogram.
- `gateway_active_connections` — gauge.
- `gateway_telemetry_buffer_size` — gauge.
- `gateway_telemetry_events_dropped_total` — counter.

Metrics exposed on a separate HTTP listener (`:9090/metrics`, no mTLS, localhost only).

---

## 4. Configuration

```yaml
# config.yaml
server:
  listen_addr: ":8443"
  read_timeout: 30s
  write_timeout: 120s
  idle_timeout: 300s

tls:
  cert_path: /etc/themisto/server/server.crt
  key_path: /etc/themisto/server/server.key
  ca_chain_path: /etc/themisto/ca/ca-chain.pem
  min_version: "1.3"
  client_auth: require

revocation:
  backend_url: "http://backend:8443"
  cache_ttl: 60s
  cache_max_entries: 10000
  fail_mode: closed  # closed | open

policy:
  refresh_interval: 5m
  default_decision: allow

upstream:
  max_idle_conns: 200
  idle_timeout: 90s
  request_timeout: 120s

backpressure:
  max_concurrency: 1000
  circuit_breaker_threshold: 5
  circuit_breaker_window: 30s
  goroutine_limit: 50000

telemetry:
  buffer_size: 10000
  flush_interval: 1s
  flush_batch_size: 500
  metrics_addr: ":9090"

database:
  dsn: "postgres://themisto:pass@postgres:5432/themisto?sslmode=disable"
  max_open_conns: 25
  max_idle_conns: 5
  conn_max_lifetime: 5m

logging:
  level: info
  format: json
```

---

## 5. Graceful Shutdown

1. Receive `SIGTERM` or `SIGINT`.
2. Stop accepting new connections (close TLS listener).
3. Wait for in-flight requests to complete (up to 30s grace period).
4. Flush telemetry buffer (remaining events).
5. Close database connections.
6. Close upstream HTTP client pool.
7. Exit 0.

If grace period expires, force-close remaining connections and exit 1.

---

## 6. Dockerfile

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /gateway ./cmd/gateway

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /gateway /usr/local/bin/gateway
USER nobody:nobody
ENTRYPOINT ["gateway"]
```

---

## 7. Health Endpoint

`GET /healthz` — served on the same port but **without** mTLS (handled before client cert verification via a custom handler that skips the main proxy pipeline).

Response:
```json
{
  "status": "ok",
  "uptime_seconds": 3600,
  "db_connected": true,
  "active_connections": 42
}
```

Returns 200 if healthy, 503 if database is unreachable or service is shutting down.
