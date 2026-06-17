package iface

// NetworkInfo provides read-only network context from the operating system.
// The core uses this for routing hints and telemetry labels.
//
// # Read-only contract
//
// All methods are pure queries. They must never modify system state, open
// sockets, or perform network I/O.
//
// # Best-effort semantics
//
// Network conditions change rapidly. Values returned are a snapshot and may
// be stale by the time the caller acts on them. The core must never use
// these values as a security gate; they are informational hints only.
//
// # Concurrency
//
// All methods must be safe for concurrent use from multiple goroutines.
type NetworkInfo interface {

	// PrimaryInterface returns a stable name or identifier for the primary
	// outbound network interface (e.g., "en0" on macOS, "Ethernet" on
	// Windows, "eth0" on Linux).
	//
	// Returns an empty string if the primary interface cannot be determined.
	// The core uses this for telemetry labels and optional per-interface
	// policy matching; it must function correctly when this returns "".
	PrimaryInterface() string

	// IsVPNActive reports whether the OS indicates an active VPN connection
	// (e.g., utun interface on macOS, WireGuard/OpenVPN adapter on Windows).
	//
	// This is best-effort: false negatives are acceptable for uncommon VPN
	// implementations. The core uses this as a routing hint and telemetry
	// signal, never as a security-critical decision point.
	IsVPNActive() bool
}
