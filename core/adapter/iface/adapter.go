// Package iface defines the contract between the OS-agnostic core and
// platform-specific adapters (macOS, Windows, Linux). Every adapter
// implementation must satisfy the [OSAdapter] interface, which composes five
// sub-interfaces covering all OS interactions the core requires.
//
// # Architecture
//
// The core never imports platform packages. It depends solely on this
// package and core/domain. OS adapters live in adapter/darwin/,
// adapter/windows/, etc. and import this package to implement it.
// Exactly one adapter is linked per build via build tags.
//
//	core/adapter/iface  ←  core/*  (depends on)
//	core/adapter/iface  ←  adapter/darwin  (implements)
//	core/adapter/iface  ←  adapter/windows (implements)
//
// # Error handling contract
//
// All errors returned by adapter methods must wrap the sentinel errors
// defined in errors.go (ErrPermission, ErrNotFound, ErrConflict, etc.)
// using fmt.Errorf("...: %w", iface.ErrXxx). The core classifies failures
// with errors.Is and never inspects error strings. When a context.Context
// is available and the operation is canceled or times out, the adapter must
// return ctx.Err() (context.Canceled or context.DeadlineExceeded).
//
// # Idempotency
//
// Every mutating method (Register, Unregister, InstallCA, RemoveCA,
// Install, Start, Stop, Uninstall) must be idempotent. Calling a method
// when the system is already in the desired state must succeed with a nil
// error. This simplifies retry logic in the core and prevents double-apply
// bugs on restarts.
//
// # Concurrency
//
// All methods must be safe for concurrent use from multiple goroutines.
// The core provides the following guarantees to simplify adapter
// implementations:
//
//   - No overlapping mutating calls on the same sub-interface (e.g., the
//     core will not call Register and Unregister simultaneously).
//   - Read-only methods (State, HasCA, Status, ResolveByPID,
//     ResolveByConnection, PrimaryInterface, IsVPNActive) may be called
//     concurrently with each other and with mutating methods.
//   - VerifyIntegrity may be called at any time, including during Register
//     or Unregister (it must tolerate transient inconsistency).
//
// Adapters must synchronize their own internal state (e.g., the remembered
// proxy values used by VerifyIntegrity).
//
// # What the adapter must NOT do
//
//   - Must not perform routing, policy evaluation, or transport logic.
//   - Must not access the network beyond local OS APIs (no HTTP calls, no
//     DNS resolution, no socket connections to external hosts).
//   - Must not start persistent background goroutines unless explicitly
//     documented (only ServiceManager.Start is allowed to block).
//   - Must not read or write core configuration; all parameters arrive
//     through method arguments.
//   - Must not log sensitive data (certificates, private keys, request
//     bodies). Logging operational events (e.g., "proxy registered on
//     :8080") via an injected Logger is acceptable.
//   - Must not type-assert the core or other interfaces; interaction is
//     strictly through the defined method signatures.
//   - Must not cache domain objects or hold references to core-owned memory
//     beyond the scope of a single method call.
//
// # How the adapter communicates metadata back to core
//
// The adapter returns structured data via domain types defined in
// core/domain:
//
//   - [domain.ProcessInfo]:   returned by ProcessResolver methods; the core
//     attaches it to domain.RequestContext before routing.
//   - [domain.ProxyState]:    returned by SystemProxy.State; the core reads
//     it for health checks and telemetry.
//   - [domain.ServiceStatus]: returned by ServiceManager.Status; the core
//     reads it for liveness reporting.
//
// The adapter never writes to domain.RequestContext or any other core-owned
// structure directly. All communication is through return values.
package iface

// OSAdapter is the aggregate interface that every platform-specific adapter
// must satisfy. The core receives a single OSAdapter at startup via
// dependency injection and holds it for the agent's lifetime.
//
// The interface is deliberately not split into optional and required parts:
// every platform must provide a concrete implementation for every method.
// Platforms that cannot support a particular operation (e.g., connection-to-
// process resolution) must return ErrUnsupported from the affected methods
// rather than omitting them.
type OSAdapter interface {
	SystemProxy
	CertStore
	ProcessResolver
	ServiceManager
	NetworkInfo
	UserNotifier
}
