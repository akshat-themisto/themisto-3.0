//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// This file is the macOS counterpart to proxy_recovery_windows.go. It is
// deliberately shaped like its Windows sibling so the frontend can stay
// OS-agnostic: both expose the same GetProxyRecoveryStatus/ResetSystemProxy
// signature, the same ProxyRecoveryStatus fields, and the same
// ResetProxyResult fields.
//
// Ownership signal on macOS: the darwin adapter writes a marker JSON file
// under /Library/Application Support/Themisto when it registers a system
// proxy. If that marker exists, we know Themisto tried to set a proxy on
// this machine. "Stale" means any of the services listed in the marker is
// still pointed at the marker's host:port even though the agent is not
// healthy. We refuse to touch proxy state without both ownership and
// staleness — this is the fail-safe contract the user asked for.

// darwinProxyMarkerPath is the on-disk marker written by the darwin adapter
// in adapter/darwin/sysproxy.go. We read it read-only here.
const darwinProxyMarkerPath = "/Library/Application Support/Themisto/proxy_marker.json"

// ProxyRecoveryStatus mirrors the Windows struct exactly.
type ProxyRecoveryStatus struct {
	ShouldOffer    bool   `json:"should_offer"`
	NeedsElevation bool   `json:"needs_elevation"`
	Reason         string `json:"reason"`
	ProxyHost      string `json:"proxy_host"`
	ProxyPort      uint16 `json:"proxy_port"`
}

// ResetProxyResult mirrors the Windows struct exactly.
type ResetProxyResult struct {
	Success bool   `json:"success"`
	Partial bool   `json:"partial"`
	Pending bool   `json:"pending"`
	Message string `json:"message"`
}

// ---------------------------------------------------------------------------
// Marker types (must match adapter/darwin/sysproxy.go on the wire)
// ---------------------------------------------------------------------------

type darwinProxyMarker struct {
	Host     string                            `json:"host"`
	Port     uint16                            `json:"port"`
	Services []string                          `json:"services"`
	Previous map[string]darwinProxyServiceInfo `json:"previous,omitempty"`
}

type darwinProxyServiceInfo struct {
	WebHost       string `json:"web_host,omitempty"`
	WebPort       uint16 `json:"web_port,omitempty"`
	WebEnabled    bool   `json:"web_enabled"`
	SecureHost    string `json:"secure_host,omitempty"`
	SecurePort    uint16 `json:"secure_port,omitempty"`
	SecureEnabled bool   `json:"secure_enabled"`
}

// ---------------------------------------------------------------------------
// Frontend-facing methods
// ---------------------------------------------------------------------------

// GetProxyRecoveryStatus is the backend-authored signal the frontend polls to
// decide whether to show the proxy-reset button on macOS. It never touches
// proxy state.
func (a *App) GetProxyRecoveryStatus() ProxyRecoveryStatus {
	marker, err := readDarwinProxyMarker(darwinProxyMarkerPath)
	if err != nil || marker == nil {
		return ProxyRecoveryStatus{ShouldOffer: false}
	}

	stale := darwinAnyServiceStale(marker, liveDarwinNetworksetupReader)
	if !stale {
		return ProxyRecoveryStatus{ShouldOffer: false}
	}

	status := a.GetAgentStatus()
	healthyRuntime := status.Running && status.ProxyState == "running"

	return deriveDarwinRecoveryStatus(marker, true, healthyRuntime, status.Running, status.ProxyState)
}

// ResetSystemProxy clears Themisto-owned proxy state on every active service
// listed in the marker. Requires administrator privileges — macOS enforces
// this at the networksetup layer. Uses the existing osascript helper so the
// user sees the native "Themisto wants to make changes" prompt.
func (a *App) ResetSystemProxy() ResetProxyResult {
	marker, err := readDarwinProxyMarker(darwinProxyMarkerPath)
	if err != nil || marker == nil {
		return ResetProxyResult{
			Success: false,
			Message: "No Themisto-owned proxy settings found. Nothing to reset.",
		}
	}
	if !darwinAnyServiceStale(marker, liveDarwinNetworksetupReader) {
		return ResetProxyResult{
			Success: false,
			Message: "Themisto proxy settings do not currently need recovery.",
		}
	}

	script := buildDarwinProxyResetScript(marker)
	if strings.TrimSpace(script) == "" {
		// Should not happen: staleness implies at least one service to touch.
		return ResetProxyResult{
			Success: false,
			Message: "Could not build proxy reset script.",
		}
	}

	if err := runAdminShellScript(script); err != nil {
		// Fail safely: we tell the user we tried and leave state alone.
		return ResetProxyResult{
			Success: false,
			Message: fmt.Sprintf("Could not reset proxy settings: %v", err),
		}
	}

	// networksetup is synchronous, so once the admin script returns we can
	// report success. Frontend will re-poll GetProxyRecoveryStatus to confirm.
	return ResetProxyResult{
		Success: true,
		Message: "System proxy cleared on macOS. If your browser still shows a proxy, restart the browser to pick up the new settings.",
	}
}

// ---------------------------------------------------------------------------
// Pure helpers (kept free of shell calls so they can be unit-tested)
// ---------------------------------------------------------------------------

// darwinNetworksetupReader reads the current web and secure-web proxy state
// for a given service. Tests inject a fake reader; production uses
// liveDarwinNetworksetupReader.
type darwinNetworksetupReader func(service string) (webHost string, webPort uint16, webOn bool, secureHost string, securePort uint16, secureOn bool, err error)

// darwinAnyServiceStale returns true if ANY service in the marker still
// advertises a web or secure-web proxy matching marker.Host:Port. If the
// reader cannot probe a service, that service is skipped (staleness must be
// positive evidence, not inferred from failure).
func darwinAnyServiceStale(m *darwinProxyMarker, read darwinNetworksetupReader) bool {
	if m == nil {
		return false
	}
	for _, svc := range m.Services {
		wh, wp, won, sh, sp, son, err := read(svc)
		if err != nil {
			continue
		}
		if won && wh == m.Host && wp == m.Port {
			return true
		}
		if son && sh == m.Host && sp == m.Port {
			return true
		}
	}
	return false
}

// deriveDarwinRecoveryStatus is the pure status-derivation step. Given a
// marker that's already known to be stale, decide how to describe the
// situation to the user. Agent health refines the wording.
func deriveDarwinRecoveryStatus(m *darwinProxyMarker, stale, healthyRuntime, running bool, proxyState string) ProxyRecoveryStatus {
	if m == nil || !stale {
		return ProxyRecoveryStatus{ShouldOffer: false}
	}
	if healthyRuntime {
		// Steady-state Themisto — every service matching marker is expected.
		// Offering a destructive reset here would be user-hostile.
		return ProxyRecoveryStatus{ShouldOffer: false}
	}

	var reason string
	switch {
	case !running:
		reason = "System proxy is set to Themisto but the agent is not running. Internet access may be affected."
	case proxyState != "running":
		reason = "System proxy is set to Themisto but the proxy listener is not active. Internet access may be affected."
	default:
		reason = "Themisto proxy settings appear inconsistent. Reset if you are experiencing connectivity issues."
	}

	return ProxyRecoveryStatus{
		ShouldOffer:    true,
		NeedsElevation: true, // networksetup always needs root on macOS
		Reason:         reason,
		ProxyHost:      m.Host,
		ProxyPort:      m.Port,
	}
}

// buildDarwinProxyResetScript assembles the shell script we pass to osascript.
// For every service in the marker it either restores the Previous state or
// falls back to turning the proxy off. Kept pure so tests can diff the output.
func buildDarwinProxyResetScript(m *darwinProxyMarker) string {
	if m == nil || len(m.Services) == 0 {
		return ""
	}
	var parts []string
	for _, svc := range m.Services {
		q := shellQuote(svc)
		prev, hasPrev := m.Previous[svc]

		// Web proxy
		if hasPrev && prev.WebEnabled && prev.WebHost != "" && prev.WebPort > 0 {
			parts = append(parts, fmt.Sprintf("/usr/sbin/networksetup -setwebproxy %s %s %d",
				q, shellQuote(prev.WebHost), prev.WebPort))
		} else {
			parts = append(parts, fmt.Sprintf("/usr/sbin/networksetup -setwebproxystate %s off", q))
		}

		// Secure (HTTPS) proxy
		if hasPrev && prev.SecureEnabled && prev.SecureHost != "" && prev.SecurePort > 0 {
			parts = append(parts, fmt.Sprintf("/usr/sbin/networksetup -setsecurewebproxy %s %s %d",
				q, shellQuote(prev.SecureHost), prev.SecurePort))
		} else {
			parts = append(parts, fmt.Sprintf("/usr/sbin/networksetup -setsecurewebproxystate %s off", q))
		}
	}
	// Best-effort marker cleanup so the next status read returns clean state.
	parts = append(parts, fmt.Sprintf("/bin/rm -f %s || true", shellQuote(darwinProxyMarkerPath)))
	return strings.Join(parts, " && ")
}

// readDarwinProxyMarker reads and parses the marker at path. Returns nil
// when the file does not exist (not an error from the caller's perspective).
func readDarwinProxyMarker(path string) (*darwinProxyMarker, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m darwinProxyMarker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if strings.TrimSpace(m.Host) == "" || m.Port == 0 {
		return nil, nil
	}
	return &m, nil
}

// ---------------------------------------------------------------------------
// Live networksetup probe (used by production code paths)
// ---------------------------------------------------------------------------

func liveDarwinNetworksetupReader(service string) (string, uint16, bool, string, uint16, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wh, wp, won, err := runNetworksetupGet(ctx, "-getwebproxy", service)
	if err != nil {
		return "", 0, false, "", 0, false, err
	}
	sh, sp, son, err := runNetworksetupGet(ctx, "-getsecurewebproxy", service)
	if err != nil {
		return "", 0, false, "", 0, false, err
	}
	return wh, wp, won, sh, sp, son, nil
}

func runNetworksetupGet(ctx context.Context, flag, service string) (string, uint16, bool, error) {
	out, err := exec.CommandContext(ctx, "/usr/sbin/networksetup", flag, service).Output()
	if err != nil {
		return "", 0, false, err
	}
	return parseNetworksetupProxyOutput(string(out))
}

// parseNetworksetupProxyOutput parses the multi-line output of
// `networksetup -getwebproxy <service>`. Exposed for tests.
func parseNetworksetupProxyOutput(out string) (string, uint16, bool, error) {
	var host string
	var port uint16
	var enabled bool
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Enabled:"):
			enabled = strings.TrimSpace(strings.TrimPrefix(line, "Enabled:")) == "Yes"
		case strings.HasPrefix(line, "Server:"):
			host = strings.TrimSpace(strings.TrimPrefix(line, "Server:"))
		case strings.HasPrefix(line, "Port:"):
			p, _ := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "Port:")), 10, 16)
			port = uint16(p)
		}
	}
	return host, port, enabled, nil
}
