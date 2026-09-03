//go:build darwin

package darwin

import (
	"context"
	"testing"

	"github.com/themisto/agent/pkg/log"
)

type testLogger struct{}

func (l *testLogger) Debug(_ string, _ ...interface{}) {}
func (l *testLogger) Info(_ string, _ ...interface{})  {}
func (l *testLogger) Warn(_ string, _ ...interface{})  {}
func (l *testLogger) Error(_ string, _ ...interface{}) {}
func (l *testLogger) With(_ ...interface{}) log.Logger {
	return l
}

func TestNew_ReturnsOSAdapter(t *testing.T) {
	adapter := New(&testLogger{})
	if adapter == nil {
		t.Fatal("New returned nil")
	}
}

func TestSystemProxy_StateWithoutRegister(t *testing.T) {
	sp := newSystemProxy(&testLogger{})
	state, err := sp.State()
	if err != nil {
		t.Fatalf("State error: %v", err)
	}
	if state.OwnedByAgent {
		t.Error("should not be owned by agent before Register")
	}
}

func TestSystemProxy_VerifyIntegrityWithoutRegister(t *testing.T) {
	sp := newSystemProxy(&testLogger{})
	intact, err := sp.VerifyIntegrity()
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if intact {
		t.Error("should not be intact without prior Register")
	}
}

func TestNetworkInfo_PrimaryInterface(t *testing.T) {
	ni := newNetworkInfo(&testLogger{})
	iface := ni.PrimaryInterface()
	// On a real Mac, this returns e.g. "en0". We just verify no crash.
	t.Logf("primary interface: %q", iface)
}

func TestNetworkInfo_IsVPNActive(t *testing.T) {
	ni := newNetworkInfo(&testLogger{})
	vpn := ni.IsVPNActive()
	t.Logf("VPN active: %v", vpn)
}

func TestProcessResolver_ResolveByPID_Self(t *testing.T) {
	pr := newProcessResolver(&testLogger{})
	info, err := pr.ResolveByPID(1) // launchd on macOS
	if err != nil {
		t.Skipf("cannot resolve PID 1 (may need root): %v", err)
	}
	if info.PID != 1 {
		t.Errorf("PID = %d, want 1", info.PID)
	}
	if info.Path == "" {
		t.Error("expected non-empty path for PID 1")
	}
	t.Logf("PID 1: path=%q name=%q user=%q", info.Path, info.Name, info.User)
}

func TestCertStore_HasCA_Nonexistent(t *testing.T) {
	cs := newCertStore(&testLogger{})
	has, err := cs.HasCA("nonexistent-id")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if has {
		t.Error("should not have nonexistent CA")
	}
}

func TestCertStore_RemoveCA_Idempotent(t *testing.T) {
	cs := newCertStore(&testLogger{})
	err := cs.RemoveCA(context.Background(), "nonexistent-id")
	if err != nil {
		t.Errorf("remove nonexistent CA should not error: %v", err)
	}
}

func TestServiceManager_StatusWhenNotInstalled(t *testing.T) {
	sm := newServiceManager(&testLogger{})
	status, err := sm.Status()
	if err != nil {
		t.Logf("status error (expected if not root): %v", err)
	}
	t.Logf("service status: %d", status)
}

func TestProfileContainsManagedProxy(t *testing.T) {
	if !profileContainsManagedProxy("ProxyAutoConfigURLString = https://mdm.example/proxy.pac") {
		t.Fatal("managed PAC profile should be detected")
	}
	if profileContainsManagedProxy("PayloadType = com.apple.security.root") {
		t.Fatal("unrelated MDM profiles must not be treated as proxy conflicts")
	}
}
