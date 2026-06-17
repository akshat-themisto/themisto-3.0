package iface

import "github.com/themisto/agent/core/domain"

// ProcessResolver extracts OS-level metadata for processes that originate
// outbound network connections. The core calls these methods to attribute
// proxy requests to their source application for policy evaluation,
// routing decisions, and telemetry tagging.
//
// # Metadata flow to core
//
// The core calls ResolveByConnection when a new connection arrives at the
// local proxy listener. The returned ProcessInfo is attached to the
// domain.RequestContext that flows through Routing and Policy. The adapter
// never writes to RequestContext directly; it only returns ProcessInfo and
// the core performs the attachment.
//
// # Best-effort population
//
// Not every field in ProcessInfo can be populated on every platform. The
// adapter must populate every field it can reliably determine and leave
// others at their zero value. The core treats zero-valued fields as
// "unknown" and must never make security-critical decisions based solely
// on a field that may be unavailable.
//
// # Concurrency
//
// Both methods must be safe for concurrent use from multiple goroutines.
// The core may call ResolveByConnection for every incoming proxy request
// in parallel. Implementations should avoid holding locks for longer than
// necessary and should not cache stale PID-to-path mappings across calls.
//
// # Performance
//
// ResolveByConnection is on the hot path for every proxied request.
// Implementations should target sub-millisecond latency. If the OS lookup
// is expensive, the adapter may use a short-lived cache (≤1 second TTL)
// keyed by the connection tuple, but must not serve stale data for longer.
type ProcessResolver interface {

	// ResolveByPID returns metadata for the process identified by pid.
	//
	// Use this when the PID is already known (e.g., from a previous
	// ResolveByConnection call or from an OS event). If the process has
	// exited or the PID is invalid, returns ErrNotFound.
	//
	// Errors:
	//   - ErrNotFound: no running process with the given PID.
	//   - ErrPermission: insufficient privileges to inspect the process.
	ResolveByPID(pid int) (domain.ProcessInfo, error)

	// ResolveByConnection identifies the process that owns the TCP
	// connection described by the four-tuple (localAddr, localPort,
	// remoteAddr, remotePort) and returns its metadata.
	//
	// The "local" side is from the connecting application's perspective:
	// localAddr:localPort is the application's ephemeral socket,
	// remoteAddr:remotePort is the agent's proxy listener.
	//
	// The adapter must:
	//   - Query the OS network table (e.g., /proc/net/tcp on Linux,
	//     libproc on macOS, GetExtendedTcpTable on Windows) to map the
	//     connection tuple to a PID, then resolve the PID to ProcessInfo.
	//   - Return the most accurate data available at call time.
	//
	// The adapter must NOT:
	//   - Perform DNS resolution on the addresses.
	//   - Cache results beyond a short TTL (see performance note above).
	//   - Return partial data in the error case; if the process cannot
	//     be identified, return a zero-value ProcessInfo with an error.
	//
	// Errors:
	//   - ErrNotFound: no process found for the given connection tuple.
	//   - ErrUnsupported: the OS does not support connection-to-process
	//     resolution.
	//   - ErrPermission: insufficient privileges.
	ResolveByConnection(
		localAddr string, localPort uint16,
		remoteAddr string, remotePort uint16,
	) (domain.ProcessInfo, error)
}
