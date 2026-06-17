//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const (
	inetSettingsPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	agentRegPath     = `SOFTWARE\Themisto\Agent`
	proxyOwnerValue  = "themisto-agent"
)

// ProxyRecoveryStatus is a backend-authored signal that tells the frontend
// whether proxy recovery should be offered and why. The frontend must not
// reconstruct proxy ownership logic itself.
type ProxyRecoveryStatus struct {
	ShouldOffer    bool   `json:"should_offer"`
	NeedsElevation bool   `json:"needs_elevation"` // true if HKLM/WinHTTP still need cleanup
	Reason         string `json:"reason"`
	ProxyHost      string `json:"proxy_host"`
	ProxyPort      uint16 `json:"proxy_port"`
}

// ResetProxyResult describes the outcome of a proxy recovery attempt.
type ResetProxyResult struct {
	Success bool   `json:"success"` // true if at least HKCU was cleaned
	Partial bool   `json:"partial"` // true if HKLM/WinHTTP not cleaned (UAC declined or binary missing)
	Pending bool   `json:"pending"` // true if elevated cleanup was launched but not confirmed complete
	Message string `json:"message"`
}

// GetProxyRecoveryStatus returns a backend-authored signal for whether proxy
// recovery should be offered in the UI. Recovery is offered only when
// Themisto-owned proxy state needs cleanup; a normal healthy Themisto proxy
// registration should not show a destructive recovery CTA.
func (a *App) GetProxyRecoveryStatus() ProxyRecoveryStatus {
	owned, regHost, regPort := isThemistoOwnedProxy()
	if !owned {
		return ProxyRecoveryStatus{ShouldOffer: false}
	}

	// Check each surface for stale Themisto proxy state.
	hkcuStale := isHKCUStale(regHost, regPort)
	hklmStale := isHKLMStale(regHost, regPort)

	if !hkcuStale && !hklmStale {
		// All surfaces are clean. No recovery needed.
		return ProxyRecoveryStatus{ShouldOffer: false}
	}

	status := a.GetAgentStatus()
	healthyRuntime := status.Running && status.ProxyState == "running"

	// Healthy, fully configured Themisto proxy state is the expected steady
	// state and should not offer a recovery button.
	if healthyRuntime && hkcuStale && hklmStale {
		return ProxyRecoveryStatus{ShouldOffer: false}
	}

	// At least one surface still needs cleanup. Agent health can help refine
	// the explanation, but partial machine cleanup should still be offered even
	// if the runtime status is otherwise healthy.

	var reason string
	switch {
	case hkcuStale && hklmStale:
		if !status.Running {
			reason = "System proxy is set to Themisto but the agent is not running. Internet access may be affected."
		} else if status.ProxyState != "running" {
			reason = "System proxy is set to Themisto but the proxy listener is not active. Internet access may be affected."
		} else {
			reason = "Themisto proxy settings appear inconsistent. Reset if you are experiencing connectivity issues."
		}
	case !hkcuStale && hklmStale:
		reason = "Browser proxy cleared. System-level proxy settings still need administrator approval."
	case hkcuStale && !hklmStale:
		if healthyRuntime {
			return ProxyRecoveryStatus{ShouldOffer: false}
		}
		reason = "Browser proxy still set to Themisto. Reset to restore your previous configuration."
	}

	return ProxyRecoveryStatus{
		ShouldOffer:    true,
		NeedsElevation: hklmStale,
		Reason:         reason,
		ProxyHost:      regHost,
		ProxyPort:      regPort,
	}
}

// ResetSystemProxy clears Themisto-owned proxy settings. This is NOT admin-gated
// because any employee needs to be able to recover internet access.
//
// HKCU is cleared directly (no elevation needed). For HKLM + WinHTTP, the method
// launches the installed agent binary with -proxy-off via UAC. Since ShellExecuteW
// only confirms launch (not completion), the result reports pending for the
// elevated path rather than claiming success.
func (a *App) ResetSystemProxy() ResetProxyResult {
	recovery := a.GetProxyRecoveryStatus()
	// Verify Themisto ownership/problem state before touching anything.
	owned, _, _ := isThemistoOwnedProxy()
	if !owned {
		return ResetProxyResult{
			Success: false,
			Message: "No Themisto-owned proxy settings found. Nothing to reset.",
		}
	}
	if !recovery.ShouldOffer {
		return ResetProxyResult{
			Success: false,
			Message: "Themisto proxy settings do not currently need recovery.",
		}
	}

	// Step 1: Clear HKCU directly (same user hive, no elevation needed).
	hkcuErr := clearHKCUThemistoProxy()

	// If only HKCU cleanup was needed, don't trigger an unnecessary UAC flow.
	if !recovery.NeedsElevation {
		if hkcuErr != nil {
			return ResetProxyResult{
				Success: false,
				Message: fmt.Sprintf("Could not reset browser proxy settings: %v", hkcuErr),
			}
		}
		return ResetProxyResult{
			Success: true,
			Message: "Browser proxy cleared.",
		}
	}

	// Step 2: Attempt elevated cleanup only when system-level cleanup is still needed.
	elevatedErr := runElevatedProxyOff()

	// Determine result. ShellExecuteW only confirms launch, not completion,
	// so elevated success means "launched" not "finished".
	switch {
	case hkcuErr != nil && elevatedErr != nil:
		return ResetProxyResult{
			Success: false,
			Message: fmt.Sprintf("Could not reset proxy settings: %v", hkcuErr),
		}

	case hkcuErr != nil && elevatedErr == nil:
		// HKCU failed but elevated was launched, or HKCU wasn't stale and only
		// system-level cleanup was needed.
		return ResetProxyResult{
			Success: !recovery.NeedsElevation,
			Pending: recovery.NeedsElevation,
			Message: "System-level proxy cleanup was requested with administrator privileges but browser proxy could not be reset directly.",
		}

	case hkcuErr == nil && elevatedErr != nil:
		// HKCU cleared but elevated failed (UAC declined or binary missing).
		return ResetProxyResult{
			Success: true,
			Partial: true,
			Message: "Browser proxy cleared. Some system-level proxy settings need administrator approval to fully reset.",
		}

	default:
		// HKCU cleared and elevated was launched (but not confirmed complete).
		return ResetProxyResult{
			Success: true,
			Pending: true,
			Message: "Browser proxy cleared. System-level proxy cleanup was requested with administrator privileges and should complete shortly.",
		}
	}
}

// ---------------------------------------------------------------------------
// Ownership detection
// ---------------------------------------------------------------------------

// isThemistoOwnedProxy checks whether the current proxy state is owned by
// Themisto using strict evidence: the owner marker in the agent registry hive,
// or an exact match between the registered host:port and the HKCU proxy value.
// Loopback alone is NOT sufficient.
func isThemistoOwnedProxy() (owned bool, regHost string, regPort uint16) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, agentRegPath, registry.QUERY_VALUE)
	if err != nil {
		return false, "", 0
	}
	defer k.Close()

	owner, _, _ := k.GetStringValue("ProxyOwner")
	host, _, _ := k.GetStringValue("RegisteredHost")
	port, _, _ := k.GetIntegerValue("RegisteredPort")

	// Strong signal: explicit owner marker.
	if owner == proxyOwnerValue {
		return true, host, uint16(port)
	}

	// Fallback: registered host:port exists and matches HKCU proxy exactly.
	if host != "" && port > 0 {
		enabled, server := readHKCUProxy()
		if enabled {
			expected := fmt.Sprintf("http=%s:%d;https=%s:%d", host, port, host, port)
			if server == expected {
				return true, host, uint16(port)
			}
		}
	}

	return false, "", 0
}

// ---------------------------------------------------------------------------
// Surface-level stale checks
// ---------------------------------------------------------------------------

// isHKCUStale returns true if HKCU WinINET proxy is still set to the
// Themisto registered address.
func isHKCUStale(regHost string, regPort uint16) bool {
	if regHost == "" || regPort == 0 {
		return false
	}
	enabled, server := readHKCUProxy()
	if !enabled {
		return false
	}
	expected := fmt.Sprintf("http=%s:%d;https=%s:%d", regHost, regPort, regHost, regPort)
	return server == expected
}

// isHKLMStale returns true if HKLM WinINET proxy is still set to the
// Themisto registered address. This is a read-only check (no elevation needed).
func isHKLMStale(regHost string, regPort uint16) bool {
	if regHost == "" || regPort == 0 {
		return false
	}
	enabled, server := readHKLMProxy()
	if !enabled {
		return false
	}
	expected := fmt.Sprintf("http=%s:%d;https=%s:%d", regHost, regPort, regHost, regPort)
	return server == expected
}

// ---------------------------------------------------------------------------
// HKCU cleanup
// ---------------------------------------------------------------------------

// clearHKCUThemistoProxy clears the HKCU WinINET proxy only if it matches
// the exact Themisto registered address. Loopback alone is not sufficient.
func clearHKCUThemistoProxy() error {
	owned, regHost, regPort := isThemistoOwnedProxy()
	if !owned {
		return fmt.Errorf("no Themisto proxy ownership confirmed; refusing to clear HKCU")
	}

	if !isHKCUStale(regHost, regPort) {
		// HKCU is already clean or doesn't match Themisto address.
		return nil
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, inetSettingsPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open HKCU internet settings: %w", err)
	}
	defer k.Close()

	_ = k.SetDWordValue("ProxyEnable", 0)
	_ = k.DeleteValue("ProxyServer")
	_ = k.DeleteValue("ProxyOverride")

	notifyProxyChangeLocal()
	return nil
}

// ---------------------------------------------------------------------------
// Elevated cleanup
// ---------------------------------------------------------------------------

// runElevatedProxyOff attempts to run the installed agent binary with -proxy-off
// via UAC elevation. Returns nil if the elevated process was successfully
// launched. Note: nil does NOT mean the cleanup completed — ShellExecuteW only
// confirms launch.
func runElevatedProxyOff() error {
	// Check if the installed agent binary exists.
	if _, err := os.Stat(agentBinaryPath); err != nil {
		return fmt.Errorf("installed agent binary not found at %s: cannot perform elevated proxy cleanup", agentBinaryPath)
	}

	mod := syscall.NewLazyDLL("shell32.dll")
	proc := mod.NewProc("ShellExecuteW")

	verb, _ := syscall.UTF16PtrFromString("runas")
	exe, _ := syscall.UTF16PtrFromString(agentBinaryPath)
	params, _ := syscall.UTF16PtrFromString("-proxy-off")
	dir, _ := syscall.UTF16PtrFromString(`C:\Program Files\Themisto`)

	ret, _, _ := proc.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(exe)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(dir)),
		0, // SW_HIDE — run silently
	)
	if ret <= 32 {
		return fmt.Errorf("elevated proxy cleanup failed (ShellExecute returned %d); try running 'themisto-agent.exe -proxy-off' as administrator", ret)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Registry helpers
// ---------------------------------------------------------------------------

// readHKCUProxy reads the current HKCU WinINET proxy state.
func readHKCUProxy() (bool, string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetSettingsPath, registry.QUERY_VALUE)
	if err != nil {
		return false, ""
	}
	defer k.Close()

	en, _, _ := k.GetIntegerValue("ProxyEnable")
	sv, _, _ := k.GetStringValue("ProxyServer")
	return en == 1, sv
}

// readHKLMProxy reads the current HKLM WinINET proxy state. This is a
// read-only check and does not require elevation.
func readHKLMProxy() (bool, string) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, inetSettingsPath, registry.QUERY_VALUE)
	if err != nil {
		return false, ""
	}
	defer k.Close()

	en, _, _ := k.GetIntegerValue("ProxyEnable")
	sv, _, _ := k.GetStringValue("ProxyServer")
	return en == 1, sv
}

// parseProxyServerLocal extracts host:port from a proxy server string.
func parseProxyServerLocal(s string) (string, uint16) {
	if s == "" {
		return "", 0
	}
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if idx := strings.Index(part, "="); idx >= 0 {
			part = part[idx+1:]
		}
		if h, pStr, ok := strings.Cut(part, ":"); ok {
			var p uint16
			fmt.Sscanf(pStr, "%d", &p)
			return h, p
		}
	}
	return s, 0
}

// notifyProxyChangeLocal notifies the system that proxy settings have changed.
func notifyProxyChangeLocal() {
	mod := syscall.NewLazyDLL("wininet.dll")
	proc := mod.NewProc("InternetSetOptionW")
	if proc.Find() != nil {
		return
	}
	proc.Call(0, 39, 0, 0) // INTERNET_OPTION_SETTINGS_CHANGED
	proc.Call(0, 37, 0, 0) // INTERNET_OPTION_REFRESH
}
