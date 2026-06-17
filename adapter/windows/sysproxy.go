//go:build windows

package windows

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/themisto/agent/core/adapter/iface"
	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/pkg/log"
)

const (
	inetSettingsPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	agentRegPath     = `SOFTWARE\Themisto\Agent`
	policiesPath     = `SOFTWARE\Policies\Microsoft\Windows\CurrentVersion\Internet Settings`
	proxyOwnerValue  = "themisto-agent"

	listenerProbeTimeout = 500 * time.Millisecond
)

// winSystemProxy implements iface.SystemProxy using the Windows registry for
// WinINET and netsh for WinHTTP.
type winSystemProxy struct {
	mu  sync.RWMutex
	log log.Logger

	regHost string
	regPort uint16
}

func newSystemProxy(logger log.Logger) *winSystemProxy {
	sp := &winSystemProxy{log: logger}
	if h, p, err := sp.readRegistered(); err == nil && h != "" {
		sp.regHost = h
		sp.regPort = p
	}
	return sp
}

// ---------------------------------------------------------------------------
// SystemProxy interface
// ---------------------------------------------------------------------------

func (s *winSystemProxy) Register(ctx context.Context, host string, port uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if gpHasProxy() {
		return fmt.Errorf("group policy proxy is active: %w", iface.ErrConflict)
	}

	if s.regHost == host && s.regPort == port && s.hasOwnerMarker() {
		return nil
	}

	curEnabled, curServer := readWinINETProxy(registry.CURRENT_USER)
	if curEnabled && curServer != "" && !s.hasOwnerMarker() {
		return fmt.Errorf("existing proxy %q: %w", curServer, iface.ErrConflict)
	}

	if err := s.backupOriginal(ctx); err != nil {
		return fmt.Errorf("backup original proxy state before registration: %w", err)
	}

	proxyServer := fmt.Sprintf("http=%s:%d;https=%s:%d", host, port, host, port)

	// Step 1: HKCU — if this fails, nothing was modified yet.
	if err := writeWinINETProxy(registry.CURRENT_USER, proxyServer); err != nil {
		return fmt.Errorf("write HKCU proxy: %w", err)
	}

	// Step 2: HKLM — if this fails, roll back HKCU.
	if err := writeWinINETProxy(registry.LOCAL_MACHINE, proxyServer); err != nil {
		s.log.Warn("write HKLM proxy failed, rolling back HKCU", "error", err)
		s.restoreHKCU()
		return fmt.Errorf("write HKLM proxy (HKCU rolled back): %w", err)
	}

	// Step 3: WinHTTP — if this fails, roll back HKCU + HKLM.
	proxyAddr := fmt.Sprintf("%s:%d", host, port)
	if err := runCmd(ctx, "netsh", "winhttp", "set", "proxy",
		"proxy-server="+proxyAddr, "bypass-list=<local>"); err != nil {
		s.log.Warn("netsh winhttp set proxy failed, rolling back HKCU+HKLM", "error", err)
		s.restoreHKCU()
		s.restoreHKLM()
		return fmt.Errorf("set winhttp proxy (HKCU+HKLM rolled back): %w", err)
	}

	notifyProxyChange()

	if err := s.writeOwnerMarker(host, port); err != nil {
		s.log.Warn("write owner marker failed", "error", err)
	}
	s.regHost = host
	s.regPort = port

	s.log.Info("system proxy registered", "host", host, "port", port)
	return nil
}

func (s *winSystemProxy) Unregister(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.hasOwnerMarker() {
		return nil
	}

	s.restoreOriginal(ctx)
	notifyProxyChange()
	s.removeOwnerMarker()
	s.regHost = ""
	s.regPort = 0

	s.log.Info("system proxy unregistered")
	return nil
}

func (s *winSystemProxy) State() (domain.ProxyState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	enabled, server := readWinINETProxy(registry.CURRENT_USER)
	host, port := parseProxyServer(server)

	return domain.ProxyState{
		Host:         host,
		Port:         port,
		Active:       enabled,
		OwnedByAgent: s.hasOwnerMarker() && enabled && host == s.regHost && port == s.regPort,
	}, nil
}

// VerifyIntegrity checks that HKCU and HKLM proxy settings match what we
// registered. This is a registry-consistency check only — it does NOT probe
// listener liveness (that is done separately by the integrity monitor to avoid
// false tamper detection during normal startup timing windows).
func (s *winSystemProxy) VerifyIntegrity() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.regHost == "" {
		return false, nil
	}
	if !s.hasOwnerMarker() {
		return false, nil
	}

	expected := fmt.Sprintf("http=%s:%d;https=%s:%d", s.regHost, s.regPort, s.regHost, s.regPort)

	en, sv := readWinINETProxy(registry.CURRENT_USER)
	if !en || sv != expected {
		return false, nil
	}
	en, sv = readWinINETProxy(registry.LOCAL_MACHINE)
	if !en || sv != expected {
		return false, nil
	}
	return true, nil
}

// ---------------------------------------------------------------------------
// Listener liveness
// ---------------------------------------------------------------------------

// ProbeListener performs a TCP dial to host:port to check if the proxy
// listener is reachable. This is a practical liveness check (TCP connect
// succeeded), not a full proxy-functionality verification.
func ProbeListener(host string, port uint16) bool {
	target := net.JoinHostPort(host, strconv.Itoa(int(port)))
	conn, err := net.DialTimeout("tcp", target, listenerProbeTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// ---------------------------------------------------------------------------
// WinINET registry helpers
// ---------------------------------------------------------------------------

func readWinINETProxy(root registry.Key) (bool, string) {
	k, err := registry.OpenKey(root, inetSettingsPath, registry.QUERY_VALUE)
	if err != nil {
		return false, ""
	}
	defer k.Close()

	en, _, _ := k.GetIntegerValue("ProxyEnable")
	sv, _, _ := k.GetStringValue("ProxyServer")
	return en == 1, sv
}

func writeWinINETProxy(root registry.Key, proxyServer string) error {
	k, _, err := registry.CreateKey(root, inetSettingsPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyServer", proxyServer); err != nil {
		return err
	}
	return k.SetStringValue("ProxyOverride", "<local>")
}

func clearWinINETProxy(root registry.Key) {
	k, err := registry.OpenKey(root, inetSettingsPath, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.SetDWordValue("ProxyEnable", 0)
	_ = k.DeleteValue("ProxyServer")
	_ = k.DeleteValue("ProxyOverride")
}

// ---------------------------------------------------------------------------
// Agent registry hive
// ---------------------------------------------------------------------------

func (s *winSystemProxy) writeOwnerMarker(host string, port uint16) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, agentRegPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	_ = k.SetStringValue("ProxyOwner", proxyOwnerValue)
	_ = k.SetStringValue("RegisteredHost", host)
	_ = k.SetDWordValue("RegisteredPort", uint32(port))
	return nil
}

func (s *winSystemProxy) hasOwnerMarker() bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, agentRegPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue("ProxyOwner")
	return err == nil && v == proxyOwnerValue
}

func (s *winSystemProxy) removeOwnerMarker() {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, agentRegPath, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.DeleteValue("ProxyOwner")
	_ = k.DeleteValue("RegisteredHost")
	_ = k.DeleteValue("RegisteredPort")
}

func (s *winSystemProxy) readRegistered() (string, uint16, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, agentRegPath, registry.QUERY_VALUE)
	if err != nil {
		return "", 0, err
	}
	defer k.Close()
	host, _, err := k.GetStringValue("RegisteredHost")
	if err != nil {
		return "", 0, err
	}
	port, _, err := k.GetIntegerValue("RegisteredPort")
	if err != nil {
		return "", 0, err
	}
	return host, uint16(port), nil
}

// ---------------------------------------------------------------------------
// Backup / restore — all three proxy surfaces
// ---------------------------------------------------------------------------

func (s *winSystemProxy) backupOriginal(ctx context.Context) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, agentRegPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	setDWord := func(name string, value uint32) error {
		if err := k.SetDWordValue(name, value); err != nil {
			return fmt.Errorf("set %s: %w", name, err)
		}
		return nil
	}
	setString := func(name, value string) error {
		if err := k.SetStringValue(name, value); err != nil {
			return fmt.Errorf("set %s: %w", name, err)
		}
		return nil
	}

	// HKCU WinINET
	en, sv := readWinINETProxy(registry.CURRENT_USER)
	d := uint32(0)
	if en {
		d = 1
	}
	if err := setDWord("OriginalProxyEnable", d); err != nil {
		return err
	}
	if err := setString("OriginalProxyServer", sv); err != nil {
		return err
	}

	ik, err := registry.OpenKey(registry.CURRENT_USER, inetSettingsPath, registry.QUERY_VALUE)
	if err == nil {
		ov, _, err := ik.GetStringValue("ProxyOverride")
		if err == nil {
			if err := setString("OriginalProxyOverride", ov); err != nil {
				ik.Close()
				return err
			}
		}
		ik.Close()
	}

	// HKLM WinINET
	hklmEn, hklmSv := readWinINETProxy(registry.LOCAL_MACHINE)
	hklmD := uint32(0)
	if hklmEn {
		hklmD = 1
	}
	if err := setDWord("OriginalHKLMProxyEnable", hklmD); err != nil {
		return err
	}
	if err := setString("OriginalHKLMProxyServer", hklmSv); err != nil {
		return err
	}

	lk, err := registry.OpenKey(registry.LOCAL_MACHINE, inetSettingsPath, registry.QUERY_VALUE)
	if err == nil {
		ov, _, err := lk.GetStringValue("ProxyOverride")
		if err == nil {
			if err := setString("OriginalHKLMProxyOverride", ov); err != nil {
				lk.Close()
				return err
			}
		}
		lk.Close()
	}

	// WinHTTP
	server, bypass, err := readWinHTTPProxy(ctx)
	if err != nil {
		return fmt.Errorf("read WinHTTP proxy state: %w", err)
	}
	if err := setString("OriginalWinHTTPProxyServer", server); err != nil {
		return err
	}
	if err := setString("OriginalWinHTTPBypassList", bypass); err != nil {
		return err
	}

	return nil
}

// restoreHKCU restores only the HKCU WinINET proxy from backup values.
// Used during transactional rollback.
func (s *winSystemProxy) restoreHKCU() {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, agentRegPath, registry.QUERY_VALUE)
	if err != nil {
		clearWinINETProxy(registry.CURRENT_USER)
		return
	}
	defer k.Close()

	en, _, err := k.GetIntegerValue("OriginalProxyEnable")
	if err != nil || en == 0 {
		clearWinINETProxy(registry.CURRENT_USER)
		return
	}

	sv, _, _ := k.GetStringValue("OriginalProxyServer")
	ov, _, _ := k.GetStringValue("OriginalProxyOverride")

	ik, _, err := registry.CreateKey(registry.CURRENT_USER, inetSettingsPath, registry.SET_VALUE)
	if err == nil {
		_ = ik.SetDWordValue("ProxyEnable", uint32(en))
		if sv != "" {
			_ = ik.SetStringValue("ProxyServer", sv)
		}
		if ov != "" {
			_ = ik.SetStringValue("ProxyOverride", ov)
		}
		ik.Close()
	}
}

// restoreHKLM restores only the HKLM WinINET proxy from backup values.
// Used during transactional rollback.
func (s *winSystemProxy) restoreHKLM() {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, agentRegPath, registry.QUERY_VALUE)
	if err != nil {
		clearWinINETProxy(registry.LOCAL_MACHINE)
		return
	}
	defer k.Close()

	en, _, err := k.GetIntegerValue("OriginalHKLMProxyEnable")
	if err != nil || en == 0 {
		clearWinINETProxy(registry.LOCAL_MACHINE)
		return
	}

	sv, _, _ := k.GetStringValue("OriginalHKLMProxyServer")
	ov, _, _ := k.GetStringValue("OriginalHKLMProxyOverride")

	lk, _, err := registry.CreateKey(registry.LOCAL_MACHINE, inetSettingsPath, registry.SET_VALUE)
	if err == nil {
		_ = lk.SetDWordValue("ProxyEnable", uint32(en))
		if sv != "" {
			_ = lk.SetStringValue("ProxyServer", sv)
		}
		if ov != "" {
			_ = lk.SetStringValue("ProxyOverride", ov)
		}
		lk.Close()
	}
}

// restoreOriginal restores all three proxy surfaces from backup values and
// cleans up the backup entries.
func (s *winSystemProxy) restoreOriginal(ctx context.Context) {
	s.restoreHKCU()
	s.restoreHKLM()
	s.restoreWinHTTP(ctx)
	s.cleanupBackupValues()
}

// restoreWinHTTP restores WinHTTP proxy from backup values.
func (s *winSystemProxy) restoreWinHTTP(ctx context.Context) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, agentRegPath, registry.QUERY_VALUE)
	if err != nil {
		_ = runCmd(ctx, "netsh", "winhttp", "reset", "proxy")
		return
	}
	defer k.Close()

	server, _, _ := k.GetStringValue("OriginalWinHTTPProxyServer")
	bypass, _, _ := k.GetStringValue("OriginalWinHTTPBypassList")

	if server == "" {
		_ = runCmd(ctx, "netsh", "winhttp", "reset", "proxy")
		return
	}

	args := []string{"winhttp", "set", "proxy", "proxy-server=" + server}
	if bypass != "" {
		args = append(args, "bypass-list="+bypass)
	}
	_ = runCmd(ctx, "netsh", args...)
}

// cleanupBackupValues removes all Original* entries from the agent registry hive.
func (s *winSystemProxy) cleanupBackupValues() {
	bk, err := registry.OpenKey(registry.LOCAL_MACHINE, agentRegPath, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer bk.Close()
	for _, name := range []string{
		"OriginalProxyEnable", "OriginalProxyServer", "OriginalProxyOverride",
		"OriginalHKLMProxyEnable", "OriginalHKLMProxyServer", "OriginalHKLMProxyOverride",
		"OriginalWinHTTPProxyServer", "OriginalWinHTTPBypassList",
	} {
		_ = bk.DeleteValue(name)
	}
}

// ---------------------------------------------------------------------------
// WinHTTP helpers
// ---------------------------------------------------------------------------

// readWinHTTPProxy reads the current WinHTTP proxy configuration by parsing
// the output of "netsh winhttp show proxy".
func readWinHTTPProxy(ctx context.Context) (server, bypass string, err error) {
	out, err := exec.CommandContext(ctx, "netsh", "winhttp", "show", "proxy").Output()
	if err != nil {
		return "", "", err
	}
	return ParseWinHTTPOutput(string(out))
}

// ParseWinHTTPOutput parses the output of "netsh winhttp show proxy" and
// returns the proxy server and bypass list. If direct access (no proxy),
// server and bypass are both empty.
func ParseWinHTTPOutput(output string) (server, bypass string, err error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)

		if strings.Contains(lower, "direct access") {
			return "", "", nil
		}

		if strings.Contains(lower, "proxy server") {
			if idx := strings.Index(line, ":"); idx >= 0 {
				server = strings.TrimSpace(line[idx+1:])
				// Handle "Proxy Server(s) :  value" format
				server = strings.TrimSpace(strings.TrimPrefix(server, " "))
			}
		}
		if strings.Contains(lower, "bypass list") {
			if idx := strings.Index(line, ":"); idx >= 0 {
				bypass = strings.TrimSpace(line[idx+1:])
				bypass = strings.TrimSpace(strings.TrimPrefix(bypass, " "))
				if strings.ToLower(bypass) == "(none)" {
					bypass = ""
				}
			}
		}
	}
	return server, bypass, nil
}

// ---------------------------------------------------------------------------
// Emergency proxy recovery (exported, shared by installer + desktop app)
// ---------------------------------------------------------------------------

// EmergencyRestoreProxy restores all three proxy surfaces from backup values
// stored in the Themisto agent registry hive. If no backup exists, it falls
// back to clearing Themisto-owned proxy state. This function is narrowly
// scoped: it checks the owner marker and only modifies state that Themisto
// owns. Requires elevation for HKLM and WinHTTP surfaces.
func EmergencyRestoreProxy(logger log.Logger) error {
	ctx := context.Background()

	// Check if we own the proxy state.
	if !hasOwnerMarkerStatic() {
		// Even without owner marker, clear if the proxy matches Themisto pattern.
		host, port, err := readRegisteredStatic()
		if err != nil || host == "" {
			return fmt.Errorf("no Themisto proxy ownership found; nothing to restore")
		}
		logger.Info("no owner marker but registered host found, clearing", "host", host, "port", port)
	}

	// Try to restore from backup.
	sp := &winSystemProxy{log: logger}
	sp.restoreOriginal(ctx)
	notifyProxyChange()
	sp.removeOwnerMarker()

	logger.Info("emergency proxy restore complete")
	return nil
}

// ClearHKCUProxy clears only the HKCU WinINET proxy. This does NOT require
// elevation and can be called by any process running as the current user.
// Only clears if the proxy appears to be Themisto-owned.
func ClearHKCUProxy() error {
	enabled, server := readWinINETProxy(registry.CURRENT_USER)
	if !enabled {
		return nil
	}
	// Only clear if it looks like a Themisto proxy (http=host:port;https=host:port pattern
	// pointing to loopback).
	host, _ := parseProxyServer(server)
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return fmt.Errorf("HKCU proxy %q does not appear to be Themisto-owned", server)
	}
	clearWinINETProxy(registry.CURRENT_USER)
	notifyProxyChange()
	return nil
}

// hasOwnerMarkerStatic checks for the Themisto owner marker without needing
// a winSystemProxy instance.
func hasOwnerMarkerStatic() bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, agentRegPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue("ProxyOwner")
	return err == nil && v == proxyOwnerValue
}

// readRegisteredStatic reads the registered host:port without a winSystemProxy instance.
func readRegisteredStatic() (string, uint16, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, agentRegPath, registry.QUERY_VALUE)
	if err != nil {
		return "", 0, err
	}
	defer k.Close()
	host, _, err := k.GetStringValue("RegisteredHost")
	if err != nil {
		return "", 0, err
	}
	port, _, err := k.GetIntegerValue("RegisteredPort")
	if err != nil {
		return "", 0, err
	}
	return host, uint16(port), nil
}

// ---------------------------------------------------------------------------
// Group Policy
// ---------------------------------------------------------------------------

func gpHasProxy() bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, policiesPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	en, _, err := k.GetIntegerValue("ProxyEnable")
	return err == nil && en == 1
}

// ---------------------------------------------------------------------------
// InternetSetOption — wininet.dll
// ---------------------------------------------------------------------------

var (
	modWinInet            = syscall.NewLazyDLL("wininet.dll")
	procInternetSetOption = modWinInet.NewProc("InternetSetOptionW")
)

const (
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
)

func notifyProxyChange() {
	if procInternetSetOption.Find() != nil {
		return
	}
	procInternetSetOption.Call(0, internetOptionSettingsChanged, 0, 0)
	procInternetSetOption.Call(0, internetOptionRefresh, 0, 0)
}

// ---------------------------------------------------------------------------
// utility
// ---------------------------------------------------------------------------

func parseProxyServer(s string) (string, uint16) {
	if s == "" {
		return "", 0
	}
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if idx := strings.Index(part, "="); idx >= 0 {
			part = part[idx+1:]
		}
		if h, pStr, ok := strings.Cut(part, ":"); ok {
			p, _ := strconv.ParseUint(pStr, 10, 16)
			return h, uint16(p)
		}
	}
	return s, 0
}
