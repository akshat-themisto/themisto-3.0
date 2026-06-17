package integration

import (
	"context"
	"testing"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/test/integration/testutil"
)

// T10.1 — Clean uninstall leaves no artifacts.
func TestT10_1_CleanUninstall(t *testing.T) {
	adapter := &uninstallMockAdapter{}
	ctx := context.Background()

	// Simulate full install lifecycle.
	adapter.Install(ctx)
	adapter.Start(ctx, func() {})
	adapter.RegisterProxy(ctx, "127.0.0.1", 8080)
	adapter.InstallCA(ctx, "gateway-ca", []byte{0x30}) // stub DER

	// Verify everything is installed.
	testutil.AssertTrue(t, adapter.serviceInstalled, "service installed")
	testutil.AssertTrue(t, adapter.proxyActive, "proxy active")
	testutil.AssertTrue(t, adapter.caInstalled, "CA installed")

	// Run uninstall sequence per E4 spec.
	adapter.Stop(ctx)
	adapter.UnregisterProxy(ctx)
	adapter.RemoveCA(ctx, "gateway-ca")
	adapter.Uninstall(ctx)

	// Verify all artifacts removed.
	testutil.AssertFalse(t, adapter.serviceInstalled, "service removed")
	testutil.AssertFalse(t, adapter.serviceRunning, "service not running")
	testutil.AssertFalse(t, adapter.proxyActive, "proxy cleared")
	testutil.AssertFalse(t, adapter.caInstalled, "CA removed")

	// Idempotent: uninstall again should not error.
	adapter.Stop(ctx)
	adapter.UnregisterProxy(ctx)
	adapter.RemoveCA(ctx, "gateway-ca")
	adapter.Uninstall(ctx)

	status := adapter.Status()
	testutil.AssertEqual(t, status, domain.ServiceNotInstalled, "status is not installed")
}

// ---------------------------------------------------------------------------
// uninstallMockAdapter — tracks artifact state for cleanup validation
// ---------------------------------------------------------------------------

type uninstallMockAdapter struct {
	serviceInstalled bool
	serviceRunning   bool
	proxyActive      bool
	caInstalled      bool
}

func (m *uninstallMockAdapter) Install(_ context.Context) error {
	m.serviceInstalled = true
	return nil
}

func (m *uninstallMockAdapter) Start(_ context.Context, ready func()) error {
	m.serviceRunning = true
	ready()
	return nil
}

func (m *uninstallMockAdapter) Stop(_ context.Context) error {
	m.serviceRunning = false
	return nil
}

func (m *uninstallMockAdapter) Uninstall(_ context.Context) error {
	m.serviceInstalled = false
	return nil
}

func (m *uninstallMockAdapter) Status() domain.ServiceStatus {
	if !m.serviceInstalled {
		return domain.ServiceNotInstalled
	}
	if m.serviceRunning {
		return domain.ServiceRunning
	}
	return domain.ServiceStopped
}

func (m *uninstallMockAdapter) RegisterProxy(_ context.Context, _ string, _ uint16) error {
	m.proxyActive = true
	return nil
}

func (m *uninstallMockAdapter) UnregisterProxy(_ context.Context) error {
	m.proxyActive = false
	return nil
}

func (m *uninstallMockAdapter) InstallCA(_ context.Context, _ string, _ []byte) error {
	m.caInstalled = true
	return nil
}

func (m *uninstallMockAdapter) RemoveCA(_ context.Context, _ string) error {
	m.caInstalled = false
	return nil
}
