//go:build darwin

package darwin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/themisto/agent/core/adapter/iface"
	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/pkg/log"
)

const (
	markerDir  = "/Library/Application Support/Themisto"
	markerFile = "proxy_marker.json"
)

// proxyMarker is the on-disk ownership record written during Register.
type proxyMarker struct {
	Host     string                      `json:"host"`
	Port     uint16                      `json:"port"`
	Services []string                    `json:"services"`
	Previous map[string]proxyServiceInfo `json:"previous,omitempty"`
}

type proxyServiceInfo struct {
	WebHost       string `json:"web_host,omitempty"`
	WebPort       uint16 `json:"web_port,omitempty"`
	WebEnabled    bool   `json:"web_enabled"`
	SecureHost    string `json:"secure_host,omitempty"`
	SecurePort    uint16 `json:"secure_port,omitempty"`
	SecureEnabled bool   `json:"secure_enabled"`
}

// darwinSystemProxy implements iface.SystemProxy using the networksetup CLI.
type darwinSystemProxy struct {
	mu         sync.RWMutex
	registered *proxyMarker // nil when no proxy is registered
	log        log.Logger
}

func newSystemProxy(logger log.Logger) *darwinSystemProxy {
	sp := &darwinSystemProxy{log: logger}
	// Restore state from on-disk marker if the daemon restarted.
	if m, err := readMarker(); err == nil {
		sp.registered = m
	}
	return sp
}

// ---------------------------------------------------------------------------
// SystemProxy interface
// ---------------------------------------------------------------------------

func (s *darwinSystemProxy) Register(ctx context.Context, host string, port uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if skipSystemProxyRegistration() {
		s.registered = &proxyMarker{Host: host, Port: port}
		s.log.Info("system proxy registration skipped", "env", "THEMISTO_SKIP_SYSTEM_PROXY")
		return nil
	}
	if s.registered == nil && managedProxyConfigurationPresent(ctx) {
		return fmt.Errorf("an MDM-managed proxy configuration is present: %w", iface.ErrConflict)
	}

	// Idempotent only if live settings still match.
	if s.registered != nil && s.registered.Host == host && s.registered.Port == port {
		intact, err := s.verifyRegisteredLocked(ctx)
		if err == nil && intact {
			return nil
		}
		s.log.Warn("registered proxy state drifted; re-applying system proxy", "error", err)
	}

	services, err := activeNetworkServices(ctx)
	if err != nil {
		return fmt.Errorf("enumerate network services: %w", err)
	}
	if len(services) == 0 {
		return fmt.Errorf("no active network services found")
	}

	previous := make(map[string]proxyServiceInfo, len(services))

	// Check for existing proxy not owned by us.
	for _, svc := range services {
		webHost, webPort, webEnabled, _ := getWebProxy(ctx, svc)
		secureHost, securePort, secureEnabled, _ := getSecureWebProxy(ctx, svc)
		previous[svc] = proxyServiceInfo{
			WebHost:       webHost,
			WebPort:       webPort,
			WebEnabled:    webEnabled,
			SecureHost:    secureHost,
			SecurePort:    securePort,
			SecureEnabled: secureEnabled,
		}
		webConflict := webEnabled && (webHost != host || webPort != port)
		secureConflict := secureEnabled && (secureHost != host || securePort != port)
		if webConflict || secureConflict {
			if !s.isOurs() {
				return fmt.Errorf("service %q already proxied and not owned by agent: %w", svc, iface.ErrConflict)
			}
		}
	}

	portStr := strconv.FormatUint(uint64(port), 10)
	for _, svc := range services {
		if err := runCmd(ctx, "networksetup", "-setwebproxy", svc, host, portStr); err != nil {
			return fmt.Errorf("set web proxy on %s: %w", svc, err)
		}
		if err := runCmd(ctx, "networksetup", "-setsecurewebproxy", svc, host, portStr); err != nil {
			return fmt.Errorf("set secure web proxy on %s: %w", svc, err)
		}
	}

	marker := &proxyMarker{Host: host, Port: port, Services: services, Previous: previous}
	if err := writeMarker(marker); err != nil {
		s.log.Warn("failed to write proxy marker", "error", err)
	}
	s.registered = marker

	s.log.Info("system proxy registered", "host", host, "port", port, "services", strings.Join(services, ", "))
	return nil
}

func (s *darwinSystemProxy) Unregister(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if skipSystemProxyRegistration() {
		s.registered = nil
		return nil
	}

	if s.registered == nil {
		return nil
	}

	for _, svc := range s.registered.Services {
		if prev, ok := s.registered.Previous[svc]; ok {
			_ = restoreProxyForService(ctx, svc, prev)
			continue
		}
		_ = disableProxyForService(ctx, svc)
	}

	_ = removeMarker()
	s.log.Info("system proxy unregistered", "services", strings.Join(s.registered.Services, ", "))
	s.registered = nil
	return nil
}

func (s *darwinSystemProxy) State() (domain.ProxyState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctx := context.Background()
	services, err := activeNetworkServices(ctx)
	if err != nil || len(services) == 0 {
		return domain.ProxyState{}, err
	}

	h, p, enabled, err := getWebProxy(ctx, services[0])
	if err != nil {
		return domain.ProxyState{}, err
	}

	return domain.ProxyState{
		Host:         h,
		Port:         p,
		Active:       enabled,
		OwnedByAgent: enabled && s.registered != nil && h == s.registered.Host && p == s.registered.Port,
	}, nil
}

func (s *darwinSystemProxy) VerifyIntegrity() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if skipSystemProxyRegistration() {
		return true, nil
	}

	if s.registered == nil {
		return false, nil
	}

	ctx := context.Background()
	for _, svc := range s.registered.Services {
		h, p, enabled, err := getWebProxy(ctx, svc)
		if err != nil {
			return false, err
		}
		if !enabled || h != s.registered.Host || p != s.registered.Port {
			return false, nil
		}
		h, p, enabled, err = getSecureWebProxy(ctx, svc)
		if err != nil {
			return false, err
		}
		if !enabled || h != s.registered.Host || p != s.registered.Port {
			return false, nil
		}
	}

	return true, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func (s *darwinSystemProxy) isOurs() bool {
	_, err := readMarker()
	return err == nil
}

func (s *darwinSystemProxy) verifyRegisteredLocked(ctx context.Context) (bool, error) {
	if s.registered == nil {
		return false, nil
	}
	for _, svc := range s.registered.Services {
		h, p, enabled, err := getWebProxy(ctx, svc)
		if err != nil {
			return false, err
		}
		if !enabled || h != s.registered.Host || p != s.registered.Port {
			return false, nil
		}
		h, p, enabled, err = getSecureWebProxy(ctx, svc)
		if err != nil {
			return false, err
		}
		if !enabled || h != s.registered.Host || p != s.registered.Port {
			return false, nil
		}
	}
	return true, nil
}

func activeNetworkServices(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "networksetup", "-listallnetworkservices").Output()
	if err != nil {
		return nil, err
	}
	var services []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "An asterisk") {
			continue
		}
		if strings.HasPrefix(line, "*") {
			continue // disabled service
		}
		services = append(services, line)
	}
	return services, nil
}

func managedProxyConfigurationPresent(ctx context.Context) bool {
	out, err := exec.CommandContext(ctx, "profiles", "show", "-type", "configuration").CombinedOutput()
	if err != nil {
		return false
	}
	return profileContainsManagedProxy(string(out))
}

func profileContainsManagedProxy(output string) bool {
	lower := strings.ToLower(output)
	for _, marker := range []string{
		"proxyautoconfigurlstring",
		"proxyserver",
		"com.apple.systemconfiguration",
		"com.apple.proxy.http.global",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func getWebProxy(ctx context.Context, service string) (host string, port uint16, enabled bool, err error) {
	return parseProxyOutput(ctx, "-getwebproxy", service)
}

func getSecureWebProxy(ctx context.Context, service string) (host string, port uint16, enabled bool, err error) {
	return parseProxyOutput(ctx, "-getsecurewebproxy", service)
}

func disableProxyForService(ctx context.Context, service string) error {
	var firstErr error
	if err := runCmd(ctx, "networksetup", "-setwebproxystate", service, "off"); err != nil {
		firstErr = err
	}
	if err := runCmd(ctx, "networksetup", "-setsecurewebproxystate", service, "off"); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func restoreProxyForService(ctx context.Context, service string, state proxyServiceInfo) error {
	var firstErr error
	if state.WebEnabled && state.WebHost != "" && state.WebPort > 0 {
		if err := runCmd(ctx, "networksetup", "-setwebproxy", service, state.WebHost, strconv.FormatUint(uint64(state.WebPort), 10)); err != nil {
			firstErr = err
		}
	} else if err := runCmd(ctx, "networksetup", "-setwebproxystate", service, "off"); err != nil {
		firstErr = err
	}

	if state.SecureEnabled && state.SecureHost != "" && state.SecurePort > 0 {
		if err := runCmd(ctx, "networksetup", "-setsecurewebproxy", service, state.SecureHost, strconv.FormatUint(uint64(state.SecurePort), 10)); err != nil && firstErr == nil {
			firstErr = err
		}
	} else if err := runCmd(ctx, "networksetup", "-setsecurewebproxystate", service, "off"); err != nil && firstErr == nil {
		firstErr = err
	}

	return firstErr
}

func parseProxyOutput(ctx context.Context, flag, service string) (string, uint16, bool, error) {
	out, err := exec.CommandContext(ctx, "networksetup", flag, service).Output()
	if err != nil {
		return "", 0, false, err
	}

	var host string
	var port uint16
	var enabled bool

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Enabled:") {
			enabled = strings.TrimSpace(strings.TrimPrefix(line, "Enabled:")) == "Yes"
		} else if strings.HasPrefix(line, "Server:") {
			host = strings.TrimSpace(strings.TrimPrefix(line, "Server:"))
		} else if strings.HasPrefix(line, "Port:") {
			p, _ := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "Port:")), 10, 16)
			port = uint16(p)
		}
	}
	return host, port, enabled, nil
}

// ---------------------------------------------------------------------------
// marker file I/O
// ---------------------------------------------------------------------------

func markerPath() string {
	return filepath.Join(markerDir, markerFile)
}

func writeMarker(m *proxyMarker) error {
	if err := os.MkdirAll(markerDir, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(markerPath(), data, 0600)
}

func readMarker() (*proxyMarker, error) {
	data, err := os.ReadFile(markerPath())
	if err != nil {
		return nil, err
	}
	var m proxyMarker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func removeMarker() error {
	err := os.Remove(markerPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func runCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return nil
}

func skipSystemProxyRegistration() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("THEMISTO_SKIP_SYSTEM_PROXY")))
	return v == "1" || v == "true" || v == "yes"
}
