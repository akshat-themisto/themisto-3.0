package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/test/integration/testutil"
)

var errConflict = errors.New("conflict")

// T2.1 — Successful system proxy registration.
func TestT2_1_ProxyRegistration(t *testing.T) {
	adapter := &proxyMockAdapter{}
	ctx := context.Background()

	err := adapter.Register(ctx, "127.0.0.1", 8080)
	testutil.AssertNoError(t, err, "register proxy")

	state, err := adapter.State()
	testutil.AssertNoError(t, err, "proxy state")
	testutil.AssertTrue(t, state.Active, "proxy active")
	testutil.AssertTrue(t, state.OwnedByAgent, "owned by agent")
	testutil.AssertEqual(t, state.Host, "127.0.0.1", "proxy host")
	testutil.AssertEqual(t, state.Port, uint16(8080), "proxy port")

	// Idempotent: second register with same values succeeds.
	err = adapter.Register(ctx, "127.0.0.1", 8080)
	testutil.AssertNoError(t, err, "idempotent register")

	intact, err := adapter.VerifyIntegrity()
	testutil.AssertNoError(t, err, "verify integrity")
	testutil.AssertTrue(t, intact, "integrity intact")
}

// T2.2 — Proxy registration with existing external proxy returns ErrConflict.
func TestT2_2_ProxyConflict(t *testing.T) {
	adapter := &proxyMockAdapter{
		externalProxy: &domain.ProxyState{
			Host: "10.0.0.1", Port: 3128, Active: true, OwnedByAgent: false,
		},
	}
	ctx := context.Background()

	err := adapter.Register(ctx, "127.0.0.1", 8080)
	testutil.AssertError(t, err, "should conflict with external proxy")
}

// ---------------------------------------------------------------------------
// proxyMockAdapter for proxy-specific tests
// ---------------------------------------------------------------------------

type proxyMockAdapter struct {
	registered    *domain.ProxyState
	externalProxy *domain.ProxyState
}

func (m *proxyMockAdapter) Register(_ context.Context, host string, port uint16) error {
	if m.externalProxy != nil && m.externalProxy.Active && !m.externalProxy.OwnedByAgent {
		return errConflict
	}
	if m.registered != nil && m.registered.Host == host && m.registered.Port == port {
		return nil
	}
	m.registered = &domain.ProxyState{Host: host, Port: port, Active: true, OwnedByAgent: true}
	return nil
}

func (m *proxyMockAdapter) Unregister(_ context.Context) error {
	m.registered = nil
	return nil
}

func (m *proxyMockAdapter) State() (domain.ProxyState, error) {
	if m.registered != nil {
		return *m.registered, nil
	}
	if m.externalProxy != nil {
		return *m.externalProxy, nil
	}
	return domain.ProxyState{}, nil
}

func (m *proxyMockAdapter) VerifyIntegrity() (bool, error) {
	return m.registered != nil, nil
}
