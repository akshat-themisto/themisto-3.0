package iface

import (
	"context"

	"github.com/themisto/agent/core/domain"
)

// ServiceManager controls the agent's lifecycle as an OS-level background
// service (launchd on macOS, Windows Service on Windows, systemd on Linux).
//
// # Lifecycle ordering
//
// The expected call sequence is:
//
//	Install → Start → (running) → Stop → Uninstall
//
// Install and Start may be called in the same session or across reboots
// (Install persists the registration; Start activates it). Stop and
// Uninstall may similarly be separated.
//
// # Blocking behavior
//
// Start blocks until Stop is called or the context is canceled. All other
// methods return after the OS has acknowledged the operation.
//
// # Concurrency
//
// Methods must be safe for concurrent use, but the core guarantees it will
// not issue overlapping mutating calls (e.g., Start and Stop at the same
// time). Status may be called concurrently with any other method.
//
// # What the adapter must NOT do
//
//   - Must not embed business logic (routing, policy, transport) inside the
//     service run loop. The adapter only manages the OS service scaffold;
//     the core's run function is invoked via the ready callback in Start.
//   - Must not fork or exec a separate process. The service must run in-process.
//   - Must not swallow OS signals. Signals (SIGTERM, etc.) must propagate to
//     the context or be translated into a Stop call.
type ServiceManager interface {

	// Install registers the agent as a persistent background service with the
	// OS service manager. This writes the service definition (e.g., launchd
	// plist, systemd unit file, Windows service registry entry) but does not
	// start it.
	//
	// Idempotent: if the service is already registered with identical
	// configuration, returns nil. If registered with different configuration,
	// the adapter must update it.
	//
	// Errors:
	//   - ErrPermission: insufficient privileges to register a service.
	//   - ErrConflict: a service with a conflicting identifier is registered
	//     by a different application.
	//   - context.Canceled / context.DeadlineExceeded.
	Install(ctx context.Context) error

	// Start begins running the agent as a background service.
	//
	// The adapter must:
	//   - Notify the OS service manager that the service is starting.
	//   - Call ready() exactly once when the service scaffold is initialized
	//     and the core may begin its work (e.g., bind the proxy listener).
	//     The core blocks on ready(); failing to call it will hang startup.
	//   - Block until Stop is called or ctx is canceled.
	//   - On return, ensure the OS service manager is notified of termination.
	//
	// The adapter must NOT:
	//   - Call ready() more than once.
	//   - Perform any core business logic inside the service run loop.
	//
	// Errors:
	//   - ErrNotInstalled: the service has not been registered via Install.
	//   - ErrPermission: insufficient privileges.
	//   - context.Canceled / context.DeadlineExceeded: shutdown requested.
	Start(ctx context.Context, ready func()) error

	// Stop requests graceful shutdown of the running background service.
	//
	// The adapter must:
	//   - Signal the OS service manager to stop the service.
	//   - Wait for the service to acknowledge termination, up to the context
	//     deadline.
	//   - Cause the blocking Start call to return.
	//
	// Idempotent: calling Stop when the service is already stopped returns nil.
	//
	// Errors:
	//   - ErrPermission: insufficient privileges.
	//   - context.Canceled / context.DeadlineExceeded: timed out waiting for
	//     the service to stop.
	Stop(ctx context.Context) error

	// Uninstall removes the agent's service registration from the OS. The
	// service must be stopped before calling Uninstall; calling Uninstall on
	// a running service returns ErrConflict.
	//
	// Idempotent: if the service is not registered, returns nil.
	//
	// Errors:
	//   - ErrPermission: insufficient privileges.
	//   - ErrConflict: the service is still running.
	//   - context.Canceled / context.DeadlineExceeded.
	Uninstall(ctx context.Context) error

	// Status returns the current state of the background service as known
	// to the OS service manager. This is a read-only query.
	//
	// Errors: only returned when the status query itself fails (e.g.,
	// cannot communicate with the service manager). An uninstalled service
	// is reported as ServiceNotInstalled, not as an error.
	Status() (domain.ServiceStatus, error)
}
