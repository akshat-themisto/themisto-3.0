package iface

import (
	"context"

	"github.com/themisto/agent/core/domain"
)

// SystemProxy controls the OS-level HTTP/HTTPS proxy configuration and
// monitors its integrity against external tampering.
//
// # Ownership semantics
//
// The adapter must track which proxy settings it installed (e.g., by writing
// a marker to a platform-specific location or comparing against a remembered
// value). It must never modify or remove proxy settings it does not own.
//
// # Concurrency
//
// All methods must be safe for concurrent use. The core guarantees that it
// will not issue overlapping mutating calls (Register, Unregister) but may
// call State or VerifyIntegrity concurrently with a mutating call.
type SystemProxy interface {

	// Register configures the operating system to route HTTP and HTTPS
	// traffic through the proxy at host:port.
	//
	// The adapter must:
	//   - Set both HTTP and HTTPS proxy settings in the OS network configuration.
	//   - Record the configured values so that State and VerifyIntegrity can
	//     distinguish agent-owned settings from externally configured proxies.
	//   - Be idempotent: if the proxy is already registered with the same
	//     host and port, return nil without modifying anything.
	//
	// The adapter must NOT:
	//   - Overwrite proxy settings installed by a different application without
	//     returning ErrConflict.
	//   - Perform DNS resolution on the host; store and apply it verbatim.
	//   - Start any background goroutines.
	//
	// Errors:
	//   - ErrPermission: insufficient OS-level privileges (e.g., requires root
	//     or admin consent on macOS/Windows).
	//   - ErrConflict: a proxy is already configured by another application
	//     and cannot be safely replaced.
	//   - context.Canceled / context.DeadlineExceeded: the context was canceled
	//     before the operation completed.
	Register(ctx context.Context, host string, port uint16) error

	// Unregister removes the agent's proxy configuration from the operating
	// system, restoring the previous state (direct connection or the proxy
	// that was in place before Register was called).
	//
	// The adapter must:
	//   - Only remove settings it owns (see ownership semantics above).
	//   - Be idempotent: if no agent-owned proxy is set, return nil.
	//
	// The adapter must NOT:
	//   - Remove proxy settings installed by another application.
	//   - Leave the network stack in an inconsistent state (e.g., HTTP cleared
	//     but HTTPS still proxied).
	//
	// Errors:
	//   - ErrPermission: insufficient privileges.
	//   - context.Canceled / context.DeadlineExceeded.
	Unregister(ctx context.Context) error

	// State returns a snapshot of the current OS-level proxy configuration.
	// This is a read-only operation that must not modify any system setting.
	//
	// The returned ProxyState.OwnedByAgent field must be true only if the
	// adapter can confirm the current settings were installed by this agent.
	//
	// Errors: only returned when the OS query itself fails (e.g., permission
	// denied reading settings). A missing or inactive proxy is reported via
	// the ProxyState fields, not as an error.
	State() (domain.ProxyState, error)

	// VerifyIntegrity checks whether the proxy configuration installed by
	// Register is still intact. It compares the live OS settings against the
	// values the adapter recorded during the last successful Register call.
	//
	// Returns:
	//   - intact=true:  settings match what Register installed.
	//   - intact=false: settings were modified or removed by an external actor
	//                   (user, malware, MDM profile, another application).
	//   - err!=nil:     the check itself failed (e.g., cannot read OS settings);
	//                   the caller must not interpret intact in this case.
	//
	// If Register was never called (or Unregister was called since), the
	// adapter must return intact=false, err=nil.
	VerifyIntegrity() (intact bool, err error)
}
