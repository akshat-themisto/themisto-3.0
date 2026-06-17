package iface

import "context"

// CertStore manages trusted CA certificates in the operating system's trust
// store. It is used by the core to install the gateway's CA so that TLS
// interception is transparent to local applications.
//
// # Certificate format
//
// All certificate bytes (der parameter) must be DER-encoded X.509. The adapter
// must validate the encoding before passing it to the OS and return
// ErrInvalidCert if the data is malformed.
//
// # Identifiers
//
// Each certificate is associated with a stable string identifier (id) chosen
// by the core. The adapter must use this id to locate certificates for
// removal and existence checks. The mapping from id to OS-native handle
// (e.g., keychain label, certutil friendly name) is the adapter's
// responsibility.
//
// # Concurrency
//
// All methods must be safe for concurrent use. The core guarantees no
// overlapping mutating calls for the same id but may call HasCA concurrently
// with InstallCA or RemoveCA on a different id.
type CertStore interface {

	// InstallCA adds a CA certificate to the OS trust store, identified by id.
	//
	// The adapter must:
	//   - Store the certificate so that all TLS clients on the system trust it.
	//   - Be idempotent: if a certificate with the same id already exists and
	//     is byte-identical, return nil. If it exists but differs, replace it.
	//   - Validate that der is a well-formed DER-encoded X.509 certificate
	//     before touching the trust store.
	//
	// The adapter must NOT:
	//   - Install the certificate into per-user stores only when a system-wide
	//     store is available and the agent has the necessary privileges.
	//   - Modify or remove certificates not managed by this agent.
	//
	// Errors:
	//   - ErrPermission: insufficient privileges to modify the trust store.
	//   - ErrInvalidCert: the provided DER data is not a valid X.509 certificate.
	//   - context.Canceled / context.DeadlineExceeded.
	InstallCA(ctx context.Context, id string, der []byte) error

	// RemoveCA removes a previously installed CA certificate by its identifier.
	//
	// The adapter must:
	//   - Remove only the certificate matching the given id.
	//   - Be idempotent: if no certificate with the given id exists, return nil.
	//
	// The adapter must NOT:
	//   - Remove certificates not installed by this agent.
	//   - Leave stale references in the trust store (e.g., a trust policy
	//     entry without its backing certificate).
	//
	// Errors:
	//   - ErrPermission: insufficient privileges.
	//   - context.Canceled / context.DeadlineExceeded.
	RemoveCA(ctx context.Context, id string) error

	// HasCA reports whether a CA certificate with the given id is currently
	// present and trusted in the OS trust store. This is a read-only check.
	//
	// Returns false, nil if the certificate was never installed or was
	// removed. Returns an error only if the query itself fails.
	HasCA(id string) (bool, error)
}
