//go:build darwin

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadDarwinProxyMarkerMissingFile(t *testing.T) {
	m, err := readDarwinProxyMarker(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != nil {
		t.Fatalf("expected nil marker for missing file, got %+v", m)
	}
}

func TestReadDarwinProxyMarkerIgnoresEmptyMarker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "marker.json")
	if err := os.WriteFile(path, []byte(`{"host":"","port":0}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	m, err := readDarwinProxyMarker(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != nil {
		t.Fatalf("empty marker should be treated as missing, got %+v", m)
	}
}

func TestReadDarwinProxyMarkerParses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "marker.json")
	raw := darwinProxyMarker{
		Host:     "127.0.0.1",
		Port:     9090,
		Services: []string{"Wi-Fi", "Ethernet"},
		Previous: map[string]darwinProxyServiceInfo{
			"Wi-Fi": {
				WebHost: "10.0.0.2", WebPort: 3128, WebEnabled: true,
			},
		},
	}
	data, _ := json.Marshal(raw)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	m, err := readDarwinProxyMarker(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil marker")
	}
	if m.Host != "127.0.0.1" || m.Port != 9090 {
		t.Fatalf("wrong marker contents: %+v", m)
	}
	if len(m.Services) != 2 || m.Services[0] != "Wi-Fi" {
		t.Fatalf("services not parsed: %+v", m.Services)
	}
	if prev := m.Previous["Wi-Fi"]; prev.WebHost != "10.0.0.2" || prev.WebPort != 3128 || !prev.WebEnabled {
		t.Fatalf("previous state not parsed: %+v", prev)
	}
}

func fakeReader(state map[string][6]interface{}) darwinNetworksetupReader {
	return func(svc string) (string, uint16, bool, string, uint16, bool, error) {
		s, ok := state[svc]
		if !ok {
			return "", 0, false, "", 0, false, os.ErrNotExist
		}
		return s[0].(string), s[1].(uint16), s[2].(bool),
			s[3].(string), s[4].(uint16), s[5].(bool), nil
	}
}

func TestDarwinAnyServiceStaleWebMatch(t *testing.T) {
	m := &darwinProxyMarker{Host: "127.0.0.1", Port: 9090, Services: []string{"Wi-Fi"}}
	reader := fakeReader(map[string][6]interface{}{
		"Wi-Fi": {"127.0.0.1", uint16(9090), true, "", uint16(0), false},
	})
	if !darwinAnyServiceStale(m, reader) {
		t.Fatal("expected stale when web proxy matches marker")
	}
}

func TestDarwinAnyServiceStaleSecureMatch(t *testing.T) {
	m := &darwinProxyMarker{Host: "127.0.0.1", Port: 9090, Services: []string{"Ethernet"}}
	reader := fakeReader(map[string][6]interface{}{
		"Ethernet": {"", uint16(0), false, "127.0.0.1", uint16(9090), true},
	})
	if !darwinAnyServiceStale(m, reader) {
		t.Fatal("expected stale when secure proxy matches marker")
	}
}

func TestDarwinAnyServiceStaleFalseWhenDifferent(t *testing.T) {
	m := &darwinProxyMarker{Host: "127.0.0.1", Port: 9090, Services: []string{"Wi-Fi"}}
	reader := fakeReader(map[string][6]interface{}{
		"Wi-Fi": {"10.0.0.1", uint16(3128), true, "10.0.0.1", uint16(3128), true},
	})
	if darwinAnyServiceStale(m, reader) {
		t.Fatal("should not be stale when proxy points at a different host")
	}
}

func TestDarwinAnyServiceStaleProbeFailureIsNotStale(t *testing.T) {
	// Positive-evidence only: failed probes must not be treated as stale.
	m := &darwinProxyMarker{Host: "127.0.0.1", Port: 9090, Services: []string{"ghost-service"}}
	reader := fakeReader(map[string][6]interface{}{}) // probing returns ErrNotExist
	if darwinAnyServiceStale(m, reader) {
		t.Fatal("probe failure must not trigger staleness")
	}
}

func TestDeriveDarwinRecoveryStatusHealthySteadyStateDoesNotOffer(t *testing.T) {
	m := &darwinProxyMarker{Host: "127.0.0.1", Port: 9090, Services: []string{"Wi-Fi"}}
	st := deriveDarwinRecoveryStatus(m, true, true, true, "running")
	if st.ShouldOffer {
		t.Fatal("should not offer recovery when agent is healthy and proxy is expected")
	}
}

func TestDeriveDarwinRecoveryStatusAgentNotRunning(t *testing.T) {
	m := &darwinProxyMarker{Host: "127.0.0.1", Port: 9090, Services: []string{"Wi-Fi"}}
	st := deriveDarwinRecoveryStatus(m, true, false, false, "stopped")
	if !st.ShouldOffer {
		t.Fatal("should offer recovery when marker is stale and agent is not running")
	}
	if !st.NeedsElevation {
		t.Fatal("macOS recovery always needs elevation")
	}
	if st.ProxyHost != "127.0.0.1" || st.ProxyPort != 9090 {
		t.Fatalf("status should echo marker host:port, got %+v", st)
	}
	if !strings.Contains(st.Reason, "agent is not running") {
		t.Fatalf("reason should mention not running, got %q", st.Reason)
	}
}

func TestDeriveDarwinRecoveryStatusProxyListenerDown(t *testing.T) {
	m := &darwinProxyMarker{Host: "127.0.0.1", Port: 9090, Services: []string{"Wi-Fi"}}
	st := deriveDarwinRecoveryStatus(m, true, false, true, "stopped")
	if !st.ShouldOffer {
		t.Fatal("should offer recovery when listener is down")
	}
	if !strings.Contains(st.Reason, "listener is not active") {
		t.Fatalf("reason should mention listener not active, got %q", st.Reason)
	}
}

func TestDeriveDarwinRecoveryStatusNotStaleNeverOffers(t *testing.T) {
	m := &darwinProxyMarker{Host: "127.0.0.1", Port: 9090, Services: []string{"Wi-Fi"}}
	st := deriveDarwinRecoveryStatus(m, false, false, false, "stopped")
	if st.ShouldOffer {
		t.Fatal("should not offer recovery when nothing is stale")
	}
}

func TestBuildDarwinProxyResetScriptOffPath(t *testing.T) {
	m := &darwinProxyMarker{
		Host:     "127.0.0.1",
		Port:     9090,
		Services: []string{"Wi-Fi", "Ethernet"},
	}
	out := buildDarwinProxyResetScript(m)
	wants := []string{
		`/usr/sbin/networksetup -setwebproxystate 'Wi-Fi' off`,
		`/usr/sbin/networksetup -setsecurewebproxystate 'Wi-Fi' off`,
		`/usr/sbin/networksetup -setwebproxystate 'Ethernet' off`,
		`/usr/sbin/networksetup -setsecurewebproxystate 'Ethernet' off`,
		`/bin/rm -f '/Library/Application Support/Themisto/proxy_marker.json'`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Fatalf("reset script missing %q\n--- got ---\n%s", w, out)
		}
	}
	// Safety: no unescaped shell metacharacters inside quoted service names.
	if strings.Contains(out, "$(") || strings.Contains(out, "`") {
		t.Fatalf("reset script contains shell metacharacters: %s", out)
	}
}

func TestBuildDarwinProxyResetScriptRestoresPrevious(t *testing.T) {
	m := &darwinProxyMarker{
		Host:     "127.0.0.1",
		Port:     9090,
		Services: []string{"Wi-Fi"},
		Previous: map[string]darwinProxyServiceInfo{
			"Wi-Fi": {
				WebHost: "10.0.0.2", WebPort: 3128, WebEnabled: true,
				SecureHost: "10.0.0.2", SecurePort: 3128, SecureEnabled: true,
			},
		},
	}
	out := buildDarwinProxyResetScript(m)
	if !strings.Contains(out, `-setwebproxy 'Wi-Fi' '10.0.0.2' 3128`) {
		t.Fatalf("expected restored web proxy command, got:\n%s", out)
	}
	if !strings.Contains(out, `-setsecurewebproxy 'Wi-Fi' '10.0.0.2' 3128`) {
		t.Fatalf("expected restored secure proxy command, got:\n%s", out)
	}
	if strings.Contains(out, `-setwebproxystate 'Wi-Fi' off`) {
		t.Fatalf("should not fall back to off when previous state is present:\n%s", out)
	}
}

func TestBuildDarwinProxyResetScriptEmptyMarker(t *testing.T) {
	if got := buildDarwinProxyResetScript(nil); got != "" {
		t.Fatalf("nil marker should return empty script, got %q", got)
	}
	if got := buildDarwinProxyResetScript(&darwinProxyMarker{}); got != "" {
		t.Fatalf("marker with no services should return empty script, got %q", got)
	}
}

func TestParseNetworksetupProxyOutput(t *testing.T) {
	out := `Enabled: Yes
Server: 127.0.0.1
Port: 9090
Authenticated Proxy Enabled: 0`
	host, port, enabled, err := parseNetworksetupProxyOutput(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("host = %q", host)
	}
	if port != 9090 {
		t.Fatalf("port = %d", port)
	}
	if !enabled {
		t.Fatal("expected enabled=true")
	}
}

func TestParseNetworksetupProxyOutputDisabled(t *testing.T) {
	out := `Enabled: No
Server:
Port: 0
Authenticated Proxy Enabled: 0`
	_, _, enabled, err := parseNetworksetupProxyOutput(out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled {
		t.Fatal("expected enabled=false")
	}
}
