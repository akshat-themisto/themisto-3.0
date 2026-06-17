package iface

import "errors"

// Sentinel errors for the adapter error-handling contract.
//
// Adapters must wrap platform-specific errors with these sentinels using
// fmt.Errorf("...: %w", ErrXxx) so the core can classify failures without
// importing platform packages.
//
// The core checks errors with errors.Is(err, iface.ErrXxx). Adapters may
// include additional context in the error message, but the sentinel must
// be in the error chain.
var (
	// ErrPermission indicates the adapter lacks OS-level privileges for the
	// requested operation (e.g., modifying proxy settings requires admin,
	// accessing the trust store requires elevated rights).
	ErrPermission = errors.New("adapter: permission denied")

	// ErrNotFound indicates the target resource does not exist — no process
	// with the given PID, no certificate with the given id, no connection
	// matching the given tuple, etc.
	ErrNotFound = errors.New("adapter: not found")

	// ErrConflict indicates a conflict with existing OS state that prevents
	// the operation from completing safely (e.g., a proxy is already
	// registered by another application, a service is still running when
	// Uninstall is called).
	ErrConflict = errors.New("adapter: conflict")

	// ErrUnsupported indicates the operation is not available on the current
	// platform or OS version. The adapter must return this rather than
	// panicking when a capability is absent.
	ErrUnsupported = errors.New("adapter: unsupported")

	// ErrNotInstalled indicates that a prerequisite registration is missing
	// (e.g., Start was called before Install).
	ErrNotInstalled = errors.New("adapter: not installed")

	// ErrInvalidCert indicates the provided certificate data is malformed,
	// not DER-encoded, or not a valid X.509 certificate.
	ErrInvalidCert = errors.New("adapter: invalid certificate")

	// ErrTimeout indicates the operation exceeded a platform-level timeout
	// unrelated to the Go context. When a context is available, adapters
	// should prefer returning ctx.Err() instead.
	ErrTimeout = errors.New("adapter: timeout")
)
