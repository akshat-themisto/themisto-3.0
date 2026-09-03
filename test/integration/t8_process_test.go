package integration

import (
	"testing"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/core/transport"
	"github.com/themisto/agent/test/integration/testutil"
)

// T8.1 — Privacy-safe process info is included in wrapper headers.
func TestT8_1_ProcessInfoInHeaders(t *testing.T) {
	proc := domain.ProcessInfo{
		PID:      42,
		Path:     "/usr/bin/curl",
		Name:     "curl",
		User:     "testuser",
		BundleID: "com.apple.curl",
		Signed:   true,
		SignerID: "Apple Inc.",
	}

	headers := transport.BuildWrapperHeaders(
		"agent-001", proc, "en0", false, "v1",
		domain.DecisionForward, "", "", "rule-1", "req-1",
	)

	testutil.AssertEqual(t, headers["X-Themisto-Process-PID"], "42", "PID header")
	testutil.AssertEqual(t, headers["X-Themisto-Process-Name"], "curl", "name header")
	testutil.AssertEqual(t, headers["X-Themisto-Process-Bundle"], "com.apple.curl", "bundle header")
	testutil.AssertEqual(t, headers["X-Themisto-Process-Signed"], "true", "signed header")
	testutil.AssertEqual(t, headers["X-Themisto-Process-Signer"], "Apple Inc.", "signer header")
	for _, forbidden := range []string{"X-Themisto-Process-Path", "X-Themisto-Process-User"} {
		if _, ok := headers[forbidden]; ok {
			t.Errorf("privacy-bearing header %s must be absent", forbidden)
		}
	}
}

// T8.2 — Unknown process: zero-value ProcessInfo omits headers.
func TestT8_2_UnknownProcessOmitsHeaders(t *testing.T) {
	proc := domain.ProcessInfo{} // all zero values

	headers := transport.BuildWrapperHeaders(
		"agent-001", proc, "", false, "v1",
		domain.DecisionForward, "", "", "", "req-2",
	)

	// Zero PID should be absent.
	if _, ok := headers["X-Themisto-Process-PID"]; ok {
		t.Error("PID header should be absent for zero PID")
	}
	if _, ok := headers["X-Themisto-Process-Path"]; ok {
		t.Error("path header should be absent for empty path")
	}
	if _, ok := headers["X-Themisto-Process-Name"]; ok {
		t.Error("name header should be absent for empty name")
	}
	if _, ok := headers["X-Themisto-Process-Signed"]; ok {
		t.Error("signed header should be absent when not signed")
	}

	// Required headers are still present.
	testutil.AssertEqual(t, headers["X-Themisto-Protocol-Version"], domain.ProtocolVersion, "protocol version always present")
	testutil.AssertEqual(t, headers["X-Themisto-Agent-ID"], "agent-001", "agent ID always present")
}
