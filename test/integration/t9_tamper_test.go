package integration

import (
	"context"
	"testing"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/test/integration/testutil"
)

// T9.1 — Tamper detection: integrity returns false after external change.
func TestT9_1_TamperDetection(t *testing.T) {
	adapter := &tamperMockAdapter{}
	ctx := context.Background()

	// Register proxy.
	err := adapter.Register(ctx, "127.0.0.1", 8080)
	testutil.AssertNoError(t, err, "register")

	intact, err := adapter.VerifyIntegrity()
	testutil.AssertNoError(t, err, "verify before tamper")
	testutil.AssertTrue(t, intact, "intact before tamper")

	// Simulate external tamper.
	adapter.tamper("10.0.0.1", 3128)

	intact, err = adapter.VerifyIntegrity()
	testutil.AssertNoError(t, err, "verify after tamper")
	testutil.AssertFalse(t, intact, "not intact after tamper")
}

// T9 — Auto-reregister after tamper restores integrity.
func TestT9_AutoReregister(t *testing.T) {
	adapter := &tamperMockAdapter{}
	ctx := context.Background()

	adapter.Register(ctx, "127.0.0.1", 8080)
	adapter.tamper("10.0.0.1", 3128)

	intact, _ := adapter.VerifyIntegrity()
	testutil.AssertFalse(t, intact, "tampered")

	// Re-register restores.
	err := adapter.Register(ctx, "127.0.0.1", 8080)
	testutil.AssertNoError(t, err, "re-register")

	intact, _ = adapter.VerifyIntegrity()
	testutil.AssertTrue(t, intact, "restored after re-register")
}

// ---------------------------------------------------------------------------
// tamperMockAdapter simulates proxy tamper detection
// ---------------------------------------------------------------------------

type tamperMockAdapter struct {
	registeredHost string
	registeredPort uint16
	liveHost       string
	livePort       uint16
}

func (m *tamperMockAdapter) Register(_ context.Context, host string, port uint16) error {
	m.registeredHost = host
	m.registeredPort = port
	m.liveHost = host
	m.livePort = port
	return nil
}

func (m *tamperMockAdapter) Unregister(_ context.Context) error {
	m.registeredHost = ""
	m.registeredPort = 0
	m.liveHost = ""
	m.livePort = 0
	return nil
}

func (m *tamperMockAdapter) State() (domain.ProxyState, error) {
	return domain.ProxyState{
		Host:         m.liveHost,
		Port:         m.livePort,
		Active:       m.liveHost != "",
		OwnedByAgent: m.liveHost == m.registeredHost && m.livePort == m.registeredPort,
	}, nil
}

func (m *tamperMockAdapter) VerifyIntegrity() (bool, error) {
	if m.registeredHost == "" {
		return false, nil
	}
	return m.liveHost == m.registeredHost && m.livePort == m.registeredPort, nil
}

func (m *tamperMockAdapter) tamper(host string, port uint16) {
	m.liveHost = host
	m.livePort = port
}
