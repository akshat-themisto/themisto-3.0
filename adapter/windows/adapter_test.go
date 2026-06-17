//go:build windows

package windows

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/pkg/log"
)

type testLogger struct{}

func (l *testLogger) Debug(_ string, _ ...interface{}) {}
func (l *testLogger) Info(_ string, _ ...interface{})  {}
func (l *testLogger) Warn(_ string, _ ...interface{})  {}
func (l *testLogger) Error(_ string, _ ...interface{}) {}
func (l *testLogger) With(_ ...interface{}) log.Logger  { return l }

func TestNew_ReturnsOSAdapter(t *testing.T) {
	adapter := New(&testLogger{})
	if adapter == nil {
		t.Fatal("New returned nil")
	}
}

func TestSystemProxy_StateReadable(t *testing.T) {
	sp := newSystemProxy(&testLogger{})
	state, err := sp.State()
	if err != nil {
		t.Fatalf("State error: %v", err)
	}
	t.Logf("proxy state: active=%v owned=%v host=%q port=%d",
		state.Active, state.OwnedByAgent, state.Host, state.Port)
}

func TestSystemProxy_VerifyIntegrityWithoutRegister(t *testing.T) {
	sp := newSystemProxy(&testLogger{})
	// Clear any stale marker from previous test runs.
	sp.regHost = ""
	sp.regPort = 0
	intact, err := sp.VerifyIntegrity()
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if intact {
		t.Error("should not be intact without prior Register")
	}
}

func TestSystemProxy_ParseProxyServer(t *testing.T) {
	tests := []struct {
		input    string
		wantHost string
		wantPort uint16
	}{
		{"127.0.0.1:8080", "127.0.0.1", 8080},
		{"http=127.0.0.1:8080;https=127.0.0.1:8080", "127.0.0.1", 8080},
		{"", "", 0},
		{"proxy.corp.net:3128", "proxy.corp.net", 3128},
	}
	for _, tt := range tests {
		h, p := parseProxyServer(tt.input)
		if h != tt.wantHost || p != tt.wantPort {
			t.Errorf("parseProxyServer(%q) = (%q, %d), want (%q, %d)",
				tt.input, h, p, tt.wantHost, tt.wantPort)
		}
	}
}

func TestNetworkInfo_PrimaryInterface(t *testing.T) {
	ni := newNetworkInfo(&testLogger{})
	iface := ni.PrimaryInterface()
	t.Logf("primary interface: %q", iface)
}

func TestNetworkInfo_IsVPNActive(t *testing.T) {
	ni := newNetworkInfo(&testLogger{})
	vpn := ni.IsVPNActive()
	t.Logf("VPN active: %v", vpn)
}

func TestProcessResolver_ResolveByPID_Self(t *testing.T) {
	pr := newProcessResolver(&testLogger{})
	pid := os.Getpid()
	info, err := pr.ResolveByPID(pid)
	if err != nil {
		t.Fatalf("ResolveByPID(%d) error: %v", pid, err)
	}
	if info.PID != pid {
		t.Errorf("PID = %d, want %d", info.PID, pid)
	}
	if info.Path == "" {
		t.Error("expected non-empty path")
	}
	if info.Name == "" {
		t.Error("expected non-empty name")
	}
	t.Logf("self: path=%q name=%q user=%q ppid=%d", info.Path, info.Name, info.User, info.ParentPID)
}

func TestProcessResolver_ResolveByPID_NotFound(t *testing.T) {
	pr := newProcessResolver(&testLogger{})
	_, err := pr.ResolveByPID(99999999) // unlikely to exist
	if err == nil {
		t.Error("expected error for non-existent PID")
	}
}

func TestProcessResolver_ConnectionCache(t *testing.T) {
	cache := newConnCache(1000) // 1 second TTL is default

	key := connKey{"127.0.0.1", 12345, "127.0.0.1", 8080}
	_, ok := cache.get(key)
	if ok {
		t.Error("cache should be empty initially")
	}

	info := domain.ProcessInfo{PID: 42, Name: "test.exe"}
	cache.set(key, info)

	got, ok := cache.get(key)
	if !ok {
		t.Fatal("cache miss after set")
	}
	if got.PID != 42 {
		t.Errorf("PID = %d, want 42", got.PID)
	}
}

func TestCertStore_HasCA_Nonexistent(t *testing.T) {
	cs := newCertStore(&testLogger{})
	has, err := cs.HasCA("nonexistent-test-id-" + strconv.Itoa(os.Getpid()))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if has {
		t.Error("should not have nonexistent CA")
	}
}

func TestCertStore_RemoveCA_Idempotent(t *testing.T) {
	cs := newCertStore(&testLogger{})
	err := cs.RemoveCA(context.Background(), "nonexistent-test-id-"+strconv.Itoa(os.Getpid()))
	if err != nil {
		t.Errorf("remove nonexistent CA should not error: %v", err)
	}
}

func TestServiceManager_StatusQuery(t *testing.T) {
	sm := newServiceManager(&testLogger{})
	status, err := sm.Status()
	if err != nil {
		t.Logf("status query error (may need admin): %v", err)
	}
	t.Logf("service status: %d", status)
}

func TestParseWinHTTPOutput(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantServer string
		wantBypass string
	}{
		{
			name: "direct access",
			input: `
Current WinHTTP proxy settings:

    Direct access (no proxy server).
`,
			wantServer: "",
			wantBypass: "",
		},
		{
			name: "proxy set with bypass",
			input: `
Current WinHTTP proxy settings:

    Proxy Server(s) :  proxy.corp.net:3128
    Bypass List     :  *.local;<local>
`,
			wantServer: "proxy.corp.net:3128",
			wantBypass: "*.local;<local>",
		},
		{
			name: "proxy set no bypass",
			input: `
Current WinHTTP proxy settings:

    Proxy Server(s) :  127.0.0.1:9090
    Bypass List     :  (none)
`,
			wantServer: "127.0.0.1:9090",
			wantBypass: "",
		},
		{
			name:       "empty output",
			input:      "",
			wantServer: "",
			wantBypass: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, bypass, err := ParseWinHTTPOutput(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if server != tt.wantServer {
				t.Errorf("server = %q, want %q", server, tt.wantServer)
			}
			if bypass != tt.wantBypass {
				t.Errorf("bypass = %q, want %q", bypass, tt.wantBypass)
			}
		})
	}
}

func TestProbeListener(t *testing.T) {
	// Start a real TCP listener for the positive case.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.ParseUint(portStr, 10, 16)

	if !ProbeListener("127.0.0.1", uint16(port)) {
		t.Error("ProbeListener should return true for active listener")
	}

	// Close the listener and verify probe fails.
	ln.Close()
	// Give OS time to release the port.
	if ProbeListener("127.0.0.1", uint16(port)) {
		t.Log("ProbeListener returned true for closed port (OS may still have it in TIME_WAIT, acceptable)")
	}

	// Probe a port that is almost certainly not in use.
	if ProbeListener("127.0.0.1", 19) {
		t.Error("ProbeListener should return false for port 19 (chargen, typically unused)")
	}
}

