//go:build darwin

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderAgentPlistContainsRequiredKeys(t *testing.T) {
	out := renderAgentPlist("/usr/local/bin/themisto-agent", "/etc/themisto/agent.json", "/var/log/themisto")

	wants := []string{
		`<string>com.themisto.agent</string>`,
		`<string>/usr/local/bin/themisto-agent</string>`,
		`<string>-config</string>`,
		`<string>/etc/themisto/agent.json</string>`,
		`<key>RunAtLoad</key>`,
		`<key>KeepAlive</key>`,
		`<key>ThrottleInterval</key>`,
		`<integer>10</integer>`,
		`<string>/var/log/themisto/agent.stdout.log</string>`,
		`<string>/var/log/themisto/agent.stderr.log</string>`,
		`<key>UserName</key>`,
		`<string>root</string>`,
		`<key>GroupName</key>`,
		`<string>wheel</string>`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Fatalf("renderAgentPlist() missing %q\n--- output ---\n%s", w, out)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(out), `<?xml version="1.0"`) {
		t.Fatal("renderAgentPlist() must begin with the XML declaration")
	}
}

func TestMergeDarwinConfigDefaultsPreservesInstalledValues(t *testing.T) {
	installed := map[string]interface{}{
		"agent_id":    "already-enrolled",
		"gateway_url": "https://gateway.prod.example.com",
	}
	source := map[string]interface{}{
		"gateway_url":      "https://gateway.bundled.example.com", // must NOT clobber
		"backend_url":      "https://backend.example.com",
		"listen_addr":      "127.0.0.1:9090",
		"default_decision": "forward",
	}

	mergeDarwinConfigDefaults(installed, source)

	if got := getString(installed, "gateway_url"); got != "https://gateway.prod.example.com" {
		t.Fatalf("merge clobbered enrolled gateway_url: %q", got)
	}
	if got := getString(installed, "backend_url"); got != "https://backend.example.com" {
		t.Fatalf("merge did not fill empty backend_url: %q", got)
	}
	if got := getString(installed, "listen_addr"); got != "127.0.0.1:9090" {
		t.Fatalf("merge did not fill empty listen_addr: %q", got)
	}
	if got := getString(installed, "default_decision"); got != "forward" {
		t.Fatalf("merge did not fill empty default_decision: %q", got)
	}
	if got := getString(installed, "agent_id"); got != "already-enrolled" {
		t.Fatalf("merge should not touch agent_id: %q", got)
	}
}

func TestCanonicalizeDarwinCredentialPaths(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]interface{}
		want map[string]string
	}{
		{
			name: "empty config gets canonical paths",
			in:   map[string]interface{}{},
			want: map[string]string{
				"cert_path": filepath.Join(darwinCertsDir, "device.crt"),
				"key_path":  filepath.Join(darwinCertsDir, "device.key"),
				"ca_path":   filepath.Join(darwinCertsDir, "ca-chain.pem"),
			},
		},
		{
			name: "relative paths are rewritten",
			in: map[string]interface{}{
				"cert_path": "certs/device.crt",
				"key_path":  "./device.key",
				"ca_path":   "ca.pem",
			},
			want: map[string]string{
				"cert_path": filepath.Join(darwinCertsDir, "device.crt"),
				"key_path":  filepath.Join(darwinCertsDir, "device.key"),
				"ca_path":   filepath.Join(darwinCertsDir, "ca-chain.pem"),
			},
		},
		{
			name: "absolute paths are preserved",
			in: map[string]interface{}{
				"cert_path": "/some/other/place/device.crt",
				"key_path":  "/some/other/place/device.key",
				"ca_path":   "/some/other/place/ca.pem",
			},
			want: map[string]string{
				"cert_path": "/some/other/place/device.crt",
				"key_path":  "/some/other/place/device.key",
				"ca_path":   "/some/other/place/ca.pem",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			canonicalizeDarwinCredentialPaths(tc.in)
			for k, want := range tc.want {
				if got := getString(tc.in, k); got != want {
					t.Fatalf("%s: %s = %q, want %q", tc.name, k, got, want)
				}
			}
		})
	}
}

func TestIsReadyToRunDarwinRejectsIncomplete(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]interface{}
		want bool
	}{
		{
			name: "missing agent_id",
			cfg:  map[string]interface{}{"gateway_url": "https://g"},
			want: false,
		},
		{
			name: "missing gateway_url",
			cfg:  map[string]interface{}{"agent_id": "a"},
			want: false,
		},
		{
			name: "cert paths pointing at /nonexistent",
			cfg: map[string]interface{}{
				"agent_id":    "a",
				"gateway_url": "https://g",
				"cert_path":   "/nonexistent/cert",
				"key_path":    "/nonexistent/key",
				"ca_path":     "/nonexistent/ca",
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isReadyToRunDarwin(tc.cfg); got != tc.want {
				t.Fatalf("%s: got %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestExtractLaunchctlField(t *testing.T) {
	out := `com.themisto.agent = {
	active count = 1
	path = /Library/LaunchDaemons/com.themisto.agent.plist
	type = LaunchDaemon
	state = running
	program = /usr/local/bin/themisto-agent
	pid = 7421
	last exit code = 0
}`
	if got := extractLaunchctlField(out, "pid = "); got != "7421" {
		t.Fatalf("pid field = %q, want %q", got, "7421")
	}
	if got := extractLaunchctlField(out, "last exit code = "); got != "0" {
		t.Fatalf("last exit code = %q, want %q", got, "0")
	}
	if got := extractLaunchctlField(out, "missing = "); got != "" {
		t.Fatalf("missing field should return empty, got %q", got)
	}
}
