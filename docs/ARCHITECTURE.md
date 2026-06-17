# AI Traffic Governance Platform — Agent Core Architecture

## 1. Architecture Diagram (Text)

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           AGENT REPOSITORY BOUNDARY                               │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                  │
│  ┌───────────────────────────────────────────────────────────────────────────┐  │
│  │                        OS ADAPTER LAYER (thin)                             │  │
│  │  ┌─────────────────────────────┐  ┌─────────────────────────────┐         │  │
│  │  │   adapter/darwin/            │  │   adapter/windows/          │         │  │
│  │  │   - sysproxy                 │  │   - sysproxy                │         │  │
│  │  │   - certstore                │  │   - certstore               │         │  │
│  │  │   - process                  │  │   - process                 │         │  │
│  │  │   - lifecycle                │  │   - lifecycle               │         │  │
│  │  └──────────────┬───────────────┘  └──────────────┬─────────────┘         │  │
│  │                 │                                  │                        │  │
│  │                 └──────────────┬───────────────────┘                        │  │
│  │                                │ implements                                  │  │
│  └────────────────────────────────┼───────────────────────────────────────────┘  │
│                                   ▼                                               │
│  ┌───────────────────────────────────────────────────────────────────────────┐  │
│  │                     CORE — ADAPTER INTERFACES                              │  │
│  │   core/adapter/iface/                                                      │  │
│  │   SystemProxy | CertStore | ProcessManager | Lifecycle | NetworkInfo       │  │
│  └───────────────────────────────────────────────────────────────────────────┘  │
│                                   │                                               │
│                                   ▼                                               │
│  ┌───────────────────────────────────────────────────────────────────────────┐  │
│  │                        CORE — BUSINESS LOGIC                               │  │
│  │                                                                             │  │
│  │   ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐       │  │
│  │   │   Config    │  │  Identity   │  │   Policy    │  │  Telemetry  │       │  │
│  │   │   (load,    │  │  (mTLS,     │  │   Sync      │  │  (metrics,   │       │  │
│  │   │   validate) │  │  certs)     │  │  (rules)    │  │  events)    │       │  │
│  │   └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘       │  │
│  │          │                │                │                │               │  │
│  │          └────────────────┼────────────────┼────────────────┘               │  │
│  │                           ▼                ▼                                │  │
│  │   ┌─────────────────────────────────────────────────────────────────────┐  │  │
│  │   │                         ROUTING                                      │  │  │
│  │   │   (match request → policy → upstream / bypass / block)               │  │  │
│  │   └─────────────────────────────────────────────────────────────────────┘  │  │
│  │                           │                                                 │  │
│  │                           ▼                                                 │  │
│  │   ┌─────────────────────────────────────────────────────────────────────┐  │  │
│  │   │                         TRANSPORT                                    │  │  │
│  │   │   (local proxy listener, mTLS to gateway, request/response relay)   │  │  │
│  │   └─────────────────────────────────────────────────────────────────────┘  │  │
│  │                                                                             │  │
│  └───────────────────────────────────────────────────────────────────────────┘  │
│                                   │                                               │
│                                   ▼                                               │
│  ┌───────────────────────────────────────────────────────────────────────────┐  │
│  │                     CORE — DOMAIN TYPES (no OS)                             │  │
│  │   core/domain/   Request, Response, Policy, Identity, Config, Telemetry     │  │
│  └───────────────────────────────────────────────────────────────────────────┘  │
│                                                                                  │
├─────────────────────────────────────────────────────────────────────────────────┤
│   EXTERNAL:  Local apps → Agent (HTTP/HTTPS proxy) → mTLS → Remote Gateway       │
└─────────────────────────────────────────────────────────────────────────────────┘
```

**Data flow (conceptual):**
- Local traffic hits agent’s HTTP/HTTPS proxy (Transport).
- Transport passes requests to Routing; Routing uses Config + Policy to decide forward / bypass / block.
- Transport uses Identity for mTLS client credentials and Transport for gateway connection.
- Policy sync and Telemetry talk to gateway (via Transport/Identity) and feed into Config and metrics.
- OS adapters are used only where the core calls “system” (proxy settings, cert store, process, lifecycle); they never contain routing, transport, or policy logic.

---

## 2. Folder Structure

```
agent/
├── cmd/
│   ├── agent/                    # Main entrypoint; wires core + OS adapter
│   │   └── main.go
│   └── agent-darwin/              # Darwin-specific build (if needed)
│   └── agent-windows/             # Windows-specific build (if needed)
│
├── core/                          # All business logic; zero OS-specific imports
│   ├── adapter/
│   │   └── iface/                 # Interfaces that OS adapters implement
│   │       ├── system_proxy.go
│   │       ├── cert_store.go
│   │       ├── process.go
│   │       ├── lifecycle.go
│   │       └── network_info.go
│   │
│   ├── config/                    # Configuration load, validation, defaults
│   │   └── iface.go
│   ├── domain/                    # Shared types (request, response, policy, identity, etc.)
│   │   ├── types.go
│   │   └── protocol.go            # Agent protocol version, message shapes
│   ├── identity/                  # mTLS identity, cert handling (via adapter)
│   │   └── iface.go
│   ├── policy/                    # Policy sync from gateway, rule application
│   │   └── iface.go
│   ├── routing/                   # Request classification, forward/bypass/block
│   │   └── iface.go
│   ├── telemetry/                 # Metrics, events, reporting
│   │   └── iface.go
│   ├── transport/                 # Local proxy server, mTLS client, relay
│   │   └── iface.go
│   └── agent.go                  # Top-level Agent type that composes modules
│
├── adapter/
│   ├── iface/                     # Re-export or type alias to core/adapter/iface (optional)
│   ├── darwin/
│   │   ├── sysproxy.go
│   │   ├── certstore.go
│   │   ├── process.go
│   │   ├── lifecycle.go
│   │   └── network.go
│   └── windows/
│       ├── sysproxy.go
│       ├── certstore.go
│       ├── process.go
│       ├── lifecycle.go
│       └── network.go
│
├── pkg/                           # Shared, non-core utilities (optional)
│   └── log/                       # Logging interface only; no OS-specific sinks here
│
├── docs/
│   └── ARCHITECTURE.md            # This document
│
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

**Rules reflected in layout:**
- `core/` has no imports from `adapter/darwin` or `adapter/windows`.
- `adapter/*` import only `core/adapter/iface` and `core/domain` (and stdlib).
- `cmd/agent` imports `core` and exactly one adapter package via build tags.

---

## 3. Interfaces That OS Adapters Must Implement

All interfaces live under `core/adapter/iface/`. Implementations live under `adapter/darwin/` and `adapter/windows/`.

---

### 3.1 System Proxy

Used by core to set/clear system HTTP/HTTPS proxy (so client traffic goes to the agent).

```go
// Package iface defines interfaces implemented by OS-specific adapters.
package iface

// SystemProxy controls the OS-level HTTP/HTTPS proxy configuration.
// The agent uses this to direct traffic to its local proxy listener.
type SystemProxy interface {
	// SetProxy configures the system to use the given proxy address (e.g. "127.0.0.1:8080").
	SetProxy(host string, port uint16) error
	// ClearProxy removes the agent's proxy configuration from the system.
	ClearProxy() error
	// GetCurrent returns the current system proxy if set by this agent (empty if none or not ours).
	GetCurrent() (host string, port uint16, ok bool)
}
```

---

### 3.2 Certificate Store

Used for installing the gateway’s CA or agent client cert for mTLS (e.g. trust store, keychain).

```go
// CertStore manages OS-specific certificate storage (trust store, client certs).
type CertStore interface {
	// InstallCA installs a CA certificate for trusting the gateway. Id is a stable identifier for later removal.
	InstallCA(id string, der []byte) error
	// RemoveCA removes a previously installed CA by id.
	RemoveCA(id string) error
	// InstallClientCert installs a client certificate and private key for mTLS. Id is a stable identifier.
	InstallClientCert(id string, certDER []byte, keyDER []byte) error
	// RemoveClientCert removes a previously installed client cert by id.
	RemoveClientCert(id string) error
}
```

---

### 3.3 Process

Used for process identity, elevation, or OS-specific process checks (e.g. “is this process allowed to bypass?”). Kept minimal so core stays OS-agnostic.

```go
// Process provides OS-specific process information.
type Process interface {
	// CurrentProcessID returns the PID of the current process.
	CurrentProcessID() int
	// CurrentProcessPath returns the executable path of the current process (or empty if unavailable).
	CurrentProcessPath() string
}
```

---

### 3.4 Lifecycle

Used for OS-specific startup (e.g. launchd, services, tray) and graceful shutdown.

```go
// Lifecycle hooks for OS-specific startup and shutdown (e.g. service, tray, run-on-login).
type Lifecycle interface {
	// RunOnOSReady is called when the OS is ready (e.g. user session). Core starts proxy after this.
	RunOnOSReady(fn func() error) error
	// RequestShutdown signals the application to shut down gracefully.
	RequestShutdown()
}
```

---

### 3.5 Network Info

Used when the core needs local network context (e.g. “current primary interface”, “is VPN up”) for routing or telemetry. Optional; core must work when not implemented or when info is unavailable.

```go
// NetworkInfo provides read-only network context from the OS.
type NetworkInfo interface {
	// PrimaryInterface returns a stable name or identifier for the primary outbound interface (e.g. "en0").
	// Returns empty string if unknown or not applicable.
	PrimaryInterface() string
	// IsVPNActive returns true if the OS reports an active VPN (best-effort).
	IsVPNActive() bool
}
```

---

## 4. Internal Modules Inside Core (Interfaces Only)

Each module is defined by interfaces in its package. Implementations live inside `core/` and depend only on other core packages and adapter interfaces.

---

### 4.1 Config

Responsible for loading, validating, and exposing agent configuration (file, env, flags). No OS-specific paths; paths are provided by the caller or a thin config loader in `cmd/`.

```go
// Package config defines configuration contract for the agent.
package config

// Source provides raw configuration bytes and origin (e.g. file path). Implemented in cmd or a small loader.
type Source interface {
	ConfigBytes() ([]byte, string, error) // bytes, origin description, error
}

// Provider exposes validated configuration to the rest of the core.
type Provider interface {
	// Get returns the current effective configuration. Callers must not mutate.
	Get() *domain.AgentConfig
	// Watch registers for changes; the implementation may call OnChange when config is reloaded.
	Watch(OnChange func(*domain.AgentConfig))
	// Validate checks the current config and returns any validation errors.
	Validate() error
}
```

---

### 4.2 Identity

Responsible for mTLS identity: credentials, TLS config for gateway connection. Uses `iface.CertStore` for storage; no direct OS calls.

```go
// Package identity defines how the agent obtains and uses mTLS identity.
package identity

import "core/domain"

// Store is implemented by core; it uses iface.CertStore for persistence.
type Store interface {
	// GetGatewayTLSConfig returns a TLS config for outbound mTLS to the gateway.
	GetGatewayTLSConfig() (*domain.TLSConfig, error)
	// RotateCredentials updates stored credentials (e.g. after policy sync). Ids identify certs in CertStore.
	RotateCredentials(caID string, clientID string, certDER, keyDER, caDER []byte) error
	// Clear removes all credentials from the store (e.g. on logout).
	Clear() error
}
```

---

### 4.3 Policy Sync

Responsible for fetching and applying policy (routing rules, allowlists) from the gateway. Depends on Transport for HTTP and Identity for mTLS.

```go
// Package policy defines policy sync and rule application.
package policy

import "core/domain"

// Fetcher retrieves policy from the gateway. Implemented in core; uses transport and identity.
type Fetcher interface {
	// Fetch retrieves the latest policy for the agent. Returns policy payload and optional version.
	Fetch() (*domain.PolicyPayload, string, error)
	// PollInterval returns the recommended duration between fetches.
	PollInterval() time.Duration
}

// Engine applies policy to requests (used by Routing). Holds rules derived from PolicyPayload.
type Engine interface {
	// Apply returns the decision for the given request context (forward, bypass, block) and optional rule id.
	Apply(ctx *domain.RequestContext) (domain.Decision, string, error)
	// Update replaces the active policy with the given payload. Called after successful Fetch.
	Update(payload *domain.PolicyPayload) error
}
```

---

### 4.4 Routing

Request classification and decision: forward to gateway, bypass (direct), or block. Uses Config and Policy only; no I/O.

```go
// Package routing defines request classification and forwarding decisions.
package routing

import "core/domain"

// Router decides how to handle each request (forward via gateway, bypass, or block).
type Router interface {
	// Route returns the decision and optional rule id for the given request context.
	Route(ctx *domain.RequestContext) (domain.Decision, string, error)
}
```

---

### 4.5 Transport

Local proxy server and mTLS relay to gateway. Depends on Identity for TLS and Routing for decisions.

```go
// Package transport defines the local proxy and gateway relay.
package transport

import (
	"context"
	"core/domain"
)

// LocalProxy is the HTTP/HTTPS proxy server that receives traffic from the OS.
type LocalProxy interface {
	// Listen starts the proxy on the given address. Blocks until Stop or error.
	Listen(ctx context.Context, addr string) error
	// Stop gracefully stops the proxy and closes connections.
	Stop() error
}

// GatewayClient performs mTLS requests to the remote gateway (used for relaying and policy fetch).
type GatewayClient interface {
	// Relay forwards a request to the gateway and returns the response. Used for proxied traffic.
	Relay(ctx context.Context, req *domain.ProxyRequest) (*domain.ProxyResponse, error)
	// Do performs an arbitrary HTTP request to the gateway (e.g. policy sync endpoint).
	Do(ctx context.Context, req *domain.HTTPRequest) (*domain.HTTPResponse, error)
	// Close closes the client and releases connections.
	Close() error
}
```

---

### 4.6 Telemetry

Metrics and events for observability. No OS-specific details; adapters may provide labels (e.g. OS name) via core-defined labels.

```go
// Package telemetry defines metrics and event reporting.
package telemetry

import "core/domain"

// Metrics records counters, gauges, and histograms. Implementation may push to gateway or log.
type Metrics interface {
	// Counter increments a counter by name and labels.
	Counter(name string, value int64, labels map[string]string)
	// Gauge sets a gauge by name and labels.
	Gauge(name string, value float64, labels map[string]string)
	// Histogram records an observation (e.g. latency).
	Histogram(name string, value float64, labels map[string]string)
}

// Events emits discrete events (e.g. agent started, policy updated, error).
type Events interface {
	// Emit sends an event with optional payload. Event names are defined in domain.
	Emit(event string, payload *domain.EventPayload) error
}
```

---

### 4.7 Logging (Core Contract)

Core does not write to OS-specific sinks; it uses an interface. The implementation is provided from `cmd/` and may use OS-specific log backends.

```go
// Package log defines the logging contract used by core. Implementations live in cmd or pkg/log.
package log

// Logger is the minimal interface required by core. No OS-specific methods.
type Logger interface {
	Debug(msg string, keyvals ...interface{})
	Info(msg string, keyvals ...interface{})
	Warn(msg string, keyvals ...interface{})
	Error(msg string, keyvals ...interface{})
	With(keyvals ...interface{}) Logger
}
```

---

## 5. Dependency Direction Rules

Enforced by layout and (optionally) `go/build` constraints or linters.

| Layer              | May import                                                                 | Must not import                          |
|--------------------|----------------------------------------------------------------------------|------------------------------------------|
| `cmd/*`            | `core/*`, `adapter/darwin` or `adapter/windows` (via tags), `pkg/log`      | Adapter internals beyond iface           |
| `core/*`           | `core/domain`, `core/adapter/iface`, `core/config`, `core/identity`, etc.  | `adapter/darwin`, `adapter/windows`, `os`, `syscall`, platform-specific packages |
| `core/domain`      | Standard library only                                                     | Any other core package, any adapter      |
| `core/adapter/iface` | `core/domain` only (if types needed)                                    | Any other core package, adapter impls    |
| `adapter/darwin`   | `core/adapter/iface`, `core/domain`, stdlib, `golang.org/x/sys/unix` etc.  | `core/routing`, `core/transport`, `core/policy` |
| `adapter/windows`  | `core/adapter/iface`, `core/domain`, stdlib, `golang.org/x/sys/windows`   | Same as darwin                           |
| `pkg/log`          | Standard library only                                                     | Core, adapters                           |

**Additional rules:**
- No circular imports between core packages. Suggested DAG: `domain` → (nothing in core); `iface` → `domain`; `config`, `identity`, `policy`, `telemetry` → `domain`; `routing` → `domain`, `config`, `policy`; `transport` → `domain`, `identity`, `routing`; top-level `agent` → all core modules and iface.
- All OS-specific imports (`runtime.GOOS`, `unix.*`, `windows.*`, `syscall`, etc.) are only in `adapter/*` or `cmd/*`. Core must be buildable with a no-op adapter that only implements interfaces.

---

## 6. Logging Architecture

- **Core**: Uses only the `log.Logger` interface (Debug, Info, Warn, Error, With). No log level logic, no file paths, no syslog/WinEvt in core.
- **Implementation**: Provided at startup from `cmd/agent`. Can be stdlog, structured logger (e.g. zerolog/slog), or an adapter that forwards to OS sinks (e.g. macOS Unified Logging, Windows ETW). Passed into each core component via constructor or option.
- **Context**: `With(keyvals...)` returns a child logger so that request IDs, module name, or policy version can be attached without core knowing the backend.
- **Sensitive data**: Core must never log request bodies, headers (e.g. Authorization), or raw certs. Logging of URLs or domains is allowed only according to policy or config (e.g. redacted by default).
- **Adapters**: May log their own OS operations using the same Logger interface passed from cmd, or a separate logger; they must not introduce new log interfaces that core depends on.

---

## 7. Versioning Strategy for Agent Protocol

- **Protocol version**: Carried in all agent–gateway communication (e.g. header `X-Agent-Protocol-Version: 2025.01` or equivalent in a control channel). Format: `YYYY.MM` or semantic (e.g. `v1`, `v2`) with a compatibility policy.
- **Negotiation**: Agent sends its supported protocol version on first handshake or first request. Gateway responds with the chosen version or rejects with a clear error (e.g. “minimum version 2025.02”).
- **Backward compatibility**: Gateway supports at least N and N-1 agent protocol versions. Agents older than N-1 receive a clear “upgrade required” response and should surface that to the user.
- **Forward compatibility**: Agent should ignore unknown fields in gateway responses and not fail on minor additions. Breaking changes (e.g. new required fields, changed semantics) require a new protocol version.
- **Version in binary**: Agent exposes its protocol version via a constant in `core/domain/protocol.go` (e.g. `ProtocolVersion = "2025.01"`). Build or release version (e.g. git tag) is separate from protocol version.
- **Policy and API**: Policy payload and sync API versions can be tied to protocol version (e.g. same number) or sub-versioned (e.g. `policy_version` inside payload). Document in protocol spec.

---

## 8. Strict Rules Preventing OS Logic from Leaking into Core

1. **Import rule**: No file under `core/` may import `adapter/darwin`, `adapter/windows`, `runtime` (for GOOS), `syscall`, `golang.org/x/sys/unix`, `golang.org/x/sys/windows`, or any package that itself imports these for OS behavior. Only `core/adapter/iface` and pure domain/types are allowed for “system” abstraction.
2. **Build tag test**: A build with a dummy adapter (e.g. `adapter/dummy` implementing all iface interfaces with no-op) and no darwin/windows files must succeed: `go build -tags=dummy ./cmd/agent`.
3. **No OS in domain types**: `core/domain` types must not contain OS-specific enums, file paths (e.g. `/Library/` or `C:\`), or platform-specific constants. Paths and platform hints are passed in from cmd or config, not inferred inside core.
4. **Single adapter at runtime**: Only one adapter implementation is linked per build (build tags: `darwin` vs `windows`). Core receives interfaces; it must never type-assert to a concrete OS type.
5. **Code review checklist**: Every core PR must confirm: no new imports of adapter impls or OS packages; no `runtime.GOOS`; no OS-specific error handling or messages; no direct file or network I/O that assumes a particular OS (use interfaces or injectors instead).
6. **Linting**: Use a custom linter or `go list` rules to forbid imports from `adapter/darwin` and `adapter/windows` in any package under `core/`.

---

## 9. Domain Types (Stubs for Interface Completeness)

These are referenced by the interfaces above; only type names and responsibilities are defined. No implementation.

- **domain.AgentConfig**: Agent config (gateway URL, listen address, proxy port, feature flags, log level, etc.).
- **domain.TLSConfig**: TLS client config (e.g. min version, cipher suites, client cert); core builds this from Identity.
- **domain.PolicyPayload**: Opaque or structured policy (rules, allowlists, version).
- **domain.RequestContext**: Request metadata used by Routing (e.g. host, path, method, source app if available from adapter).
- **domain.Decision**: Enum: Forward, Bypass, Block.
- **domain.ProxyRequest / domain.ProxyResponse**: Request/response for Relay (method, URL, headers, body, status).
- **domain.HTTPRequest / domain.HTTPResponse**: Generic HTTP request/response for GatewayClient.Do.
- **domain.EventPayload**: Key-value or structured payload for Telemetry.Emit.

All of these live in `core/domain/` and contain only standard types and core-defined enums/structs with no OS-specific fields.

---

## 10. Summary

- **Core** holds all business logic (routing, transport, identity, policy, config, telemetry) and depends only on `core/domain` and `core/adapter/iface`.
- **OS adapters** are thin implementations of `SystemProxy`, `CertStore`, `Process`, `Lifecycle`, and `NetworkInfo` in `adapter/darwin` and `adapter/windows`.
- **Dependency direction** is strictly inward: adapters and cmd depend on core; core never depends on adapter implementations or OS packages.
- **Logging** is interface-based; implementation is injected from cmd.
- **Protocol versioning** is explicit in agent–gateway communication with a clear compatibility policy.
- **Governance** is enforced by import rules, build tags, and linting so that OS logic cannot leak into core.

This document is the single source of truth for the agent repository layout, interfaces, and boundaries until revised by the architecture owner.
