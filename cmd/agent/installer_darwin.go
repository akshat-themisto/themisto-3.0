//go:build darwin

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Canonical macOS install layout. These match adapter/darwin/lifecycle.go and
// desktop/install_paths.go so the agent, desktop app, and launchd plist all
// agree on where binaries, configs, certs, and logs live.
const (
	darwinInstallBin       = "/usr/local/bin/themisto-agent"
	darwinConfigDir        = "/etc/themisto"
	darwinCertsDir         = "/etc/themisto/identity"
	darwinConfigPath       = "/etc/themisto/agent.json"
	darwinLogDir           = "/var/log/themisto"
	darwinLaunchDaemonPath = "/Library/LaunchDaemons/com.themisto.agent.plist"
	darwinLaunchLabel      = "com.themisto.agent"
)

// ---------------------------------------------------------------------------
// defaults / mode detection
// ---------------------------------------------------------------------------

// defaultConfigPath returns the canonical installed config path. Mirrors
// Windows (which returns C:\ProgramData\Themisto\agent.json). Dev workflows
// should pass -config explicitly.
func defaultConfigPath() string { return darwinConfigPath }

// isRunningAsService reports whether the process is under launchd. macOS does
// not provide a direct equivalent of svc.IsWindowsService, so we rely on the
// fact that our launchd plist always runs as UID 0 and the PPID is launchd
// (PID 1). The agent process just runs to completion either way; this only
// affects whether main() takes the service branch, which on darwin is a no-op.
func isRunningAsService() bool { return false }

// runAsService is intentionally a no-op on darwin. launchd supervises the
// process directly, so main() falls through to the normal run path.
func runAsService(_ string) error { return nil }

// ---------------------------------------------------------------------------
// Install
// ---------------------------------------------------------------------------

func runInstall(sourceDir string, logger *stdLogger) error {
	if sourceDir == "" {
		sourceDir = selfDir()
	}

	if !isRoot() {
		return fmt.Errorf("root privileges required: re-run with 'sudo %s -install'", self())
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("   Themisto Agent - macOS Installer")
	fmt.Println("========================================")
	fmt.Println()

	// 1. Create canonical directories.
	if err := ensureDarwinDirs(); err != nil {
		return err
	}
	fmt.Println("[OK] Created install directories")

	// 2. Stop any existing launchd service before overwriting the binary.
	fmt.Println("[..] Stopping existing agent (if running)...")
	_ = bootoutLaunchd()
	// launchd bootout can take a moment to release the process.
	time.Sleep(500 * time.Millisecond)

	// 3. Copy self to /usr/local/bin/themisto-agent.
	if err := copySelfToDarwinInstall(); err != nil {
		return fmt.Errorf("copy binary: %w", err)
	}
	fmt.Println("[OK] Installed binary to", darwinInstallBin)

	// 4. Load or create config, merging non-destructively from source.
	cfg, err := loadOrCreateDarwinConfig(sourceDir)
	if err != nil {
		return err
	}

	// 5. Copy certs from the source directory if present.
	certsFound := detectAndCopyDarwinCerts(sourceDir, cfg)
	if certsFound {
		fmt.Println("[OK] Certificates installed")
	}
	canonicalizeDarwinCredentialPaths(cfg)

	// 6. Persist config.
	if err := writeConfigMap(darwinConfigPath, cfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Chmod(darwinConfigPath, 0600); err != nil {
		fmt.Printf("[WARN] Could not tighten config permissions: %v\n", err)
	}
	fmt.Println("[OK] Configuration written to", darwinConfigPath)

	// 7. Run enrollment in-install if a token is present and no certs exist yet.
	if !certsFound && getString(cfg, "enrollment_token") != "" {
		fmt.Println("[..] Running device enrollment...")
		if err := doDarwinEnrollmentDuringInstall(cfg, logger); err != nil {
			fmt.Printf("[WARN] Enrollment failed: %v\n", err)
			fmt.Println("       You can enroll later with: themisto-agent -activate ...")
		} else {
			fmt.Println("[OK] Device enrolled and certificates received")
			if updated, err := loadConfigMap(darwinConfigPath); err == nil {
				cfg = updated
			}
		}
	}

	// 8. Write the launchd plist and load it.
	if err := writeLaunchDaemonPlist(); err != nil {
		return fmt.Errorf("install launchd plist: %w", err)
	}
	fmt.Println("[OK] LaunchDaemon installed at", darwinLaunchDaemonPath)

	ready := isReadyToRunDarwin(cfg)

	serviceVerified := false
	if ready {
		if err := bootstrapLaunchd(); err != nil {
			fmt.Printf("[WARN] Could not bootstrap launchd service: %v\n", err)
		} else if err := kickstartLaunchd(); err != nil {
			fmt.Printf("[WARN] Could not kickstart launchd service: %v\n", err)
		} else if err := waitForLaunchdRunning(15 * time.Second); err != nil {
			fmt.Printf("[WARN] Service did not reach running state: %v\n", err)
		} else {
			serviceVerified = true
			fmt.Println("[OK] Agent service is running")
		}
	}

	fmt.Println()
	fmt.Println("========================================")
	switch {
	case ready && serviceVerified:
		fmt.Println("   Installation complete!")
		fmt.Println("========================================")
		fmt.Println()
		fmt.Println("The agent is running and proxying traffic on", getString(cfg, "listen_addr"))
		fmt.Println()
		fmt.Println("Useful commands:")
		fmt.Println("  sudo themisto-agent -service-status   Show service status")
		fmt.Println("  sudo themisto-agent -service-stop     Stop the agent")
		fmt.Println("  sudo themisto-agent -service-start    Start the agent")
		fmt.Println("  sudo themisto-agent -uninstall        Remove the agent")
		fmt.Println()
		fmt.Println("SAFETY: If your internet stops working, run:")
		fmt.Println("  sudo themisto-agent -proxy-off")
	case ready && !serviceVerified:
		fmt.Println("   Installed With Warning")
		fmt.Println("========================================")
		fmt.Println()
		fmt.Println("The launchd service was installed but did not reach running state.")
		fmt.Println("Inspect logs at:", filepath.Join(darwinLogDir, "agent.stderr.log"))
		fmt.Println("Then run: sudo themisto-agent -service-start")
	default:
		fmt.Println("   Installed (needs configuration)")
		fmt.Println("========================================")
		fmt.Println()
		fmt.Println("Edit:", darwinConfigPath)
		fmt.Println("Set: agent_id, gateway_url")
		fmt.Println("Place certificates in:", darwinCertsDir)
		fmt.Println()
		fmt.Println("Then start the service:")
		fmt.Println("  sudo themisto-agent -service-start")
	}
	fmt.Println()
	return nil
}

// ---------------------------------------------------------------------------
// Uninstall
// ---------------------------------------------------------------------------

func runUninstall(logger *stdLogger) error {
	if !isRoot() {
		return fmt.Errorf("root privileges required: re-run with 'sudo %s -uninstall'", self())
	}

	fmt.Println()
	fmt.Println("[..] Stopping agent service...")
	_ = bootoutLaunchd()

	fmt.Println("[..] Clearing system proxy...")
	_ = runProxyOff(logger) // best-effort

	fmt.Println("[..] Removing launchd plist...")
	if err := os.Remove(darwinLaunchDaemonPath); err != nil && !os.IsNotExist(err) {
		fmt.Printf("[WARN] Could not remove %s: %v\n", darwinLaunchDaemonPath, err)
	}

	fmt.Println("[..] Removing agent binary...")
	if err := os.Remove(darwinInstallBin); err != nil && !os.IsNotExist(err) {
		fmt.Printf("[WARN] Could not remove %s: %v\n", darwinInstallBin, err)
	}

	fmt.Println()
	fmt.Println("[OK] Themisto Agent uninstalled")
	fmt.Println("     Configuration kept at:", darwinConfigDir)
	fmt.Println()
	return nil
}

// ---------------------------------------------------------------------------
// Service management
// ---------------------------------------------------------------------------

func runServiceStart(logger *stdLogger) error {
	if !isRoot() {
		return fmt.Errorf("root privileges required: re-run with 'sudo %s -service-start'", self())
	}
	if _, err := os.Stat(darwinLaunchDaemonPath); err != nil {
		return fmt.Errorf("start service: launchd plist not found at %s (run -install first)", darwinLaunchDaemonPath)
	}
	// bootstrap is idempotent-ish: it fails if the service is already loaded,
	// which is fine â€” we kickstart either way.
	_ = bootstrapLaunchd()
	if err := kickstartLaunchd(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	fmt.Println("[OK] Service started")
	return nil
}

func runServiceStop(logger *stdLogger) error {
	if !isRoot() {
		return fmt.Errorf("root privileges required: re-run with 'sudo %s -service-stop'", self())
	}
	if err := bootoutLaunchd(); err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
	fmt.Println("[OK] Service stopped")
	return nil
}

func runServiceStatus(logger *stdLogger) error {
	state, detail := queryLaunchdState()
	fmt.Printf("Service state: %s\n", state)
	fmt.Printf("Detail: %s\n", detail)
	return nil
}

// ---------------------------------------------------------------------------
// launchd helpers
// ---------------------------------------------------------------------------

func ensureDarwinDirs() error {
	dirs := []struct {
		path string
		mode os.FileMode
	}{
		{filepath.Dir(darwinInstallBin), 0755},
		{darwinConfigDir, 0755},
		{darwinCertsDir, 0700},
		{darwinLogDir, 0700},
		{filepath.Dir(darwinLaunchDaemonPath), 0755},
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d.path, d.mode); err != nil {
			return fmt.Errorf("create %s: %w", d.path, err)
		}
		if err := os.Chmod(d.path, d.mode); err != nil {
			// Non-fatal: MkdirAll sets perms only for dirs it creates.
		}
	}
	return nil
}

func copySelfToDarwinInstall() error {
	src, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	absSrc, _ := filepath.Abs(src)
	absDst, _ := filepath.Abs(darwinInstallBin)
	if absSrc == absDst {
		// Already installed at the canonical path â€” just re-stat perms.
		return os.Chmod(darwinInstallBin, 0755)
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	// Write to a temp file in the same directory and rename atomically.
	dir := filepath.Dir(darwinInstallBin)
	tmp, err := os.CreateTemp(dir, "themisto-agent-*.new")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("copy bytes: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, darwinInstallBin); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename to %s: %w", darwinInstallBin, err)
	}
	return nil
}

// writeLaunchDaemonPlist writes the canonical com.themisto.agent.plist atomically
// with 0644 and root:wheel ownership (since we already run as root during install).
func writeLaunchDaemonPlist() error {
	plist := renderAgentPlist(darwinInstallBin, darwinConfigPath, darwinLogDir)
	tmp, err := os.CreateTemp(filepath.Dir(darwinLaunchDaemonPath), "com.themisto.agent-*.plist")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(plist); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0644); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, darwinLaunchDaemonPath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// renderAgentPlist produces the LaunchDaemon plist contents. Pulled into its
// own function so tests can assert output without touching /Library.
func renderAgentPlist(exe, cfg, logDir string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>-config</string>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ThrottleInterval</key>
  <integer>10</integer>
  <key>StandardOutPath</key>
  <string>%s/agent.stdout.log</string>
  <key>StandardErrorPath</key>
  <string>%s/agent.stderr.log</string>
  <key>UserName</key>
  <string>root</string>
  <key>GroupName</key>
  <string>wheel</string>
</dict>
</plist>
`, darwinLaunchLabel, exe, cfg, logDir, logDir)
}

func bootstrapLaunchd() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return runCmd(ctx, "launchctl", "bootstrap", "system", darwinLaunchDaemonPath)
}

func bootoutLaunchd() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// bootout exits non-zero if the service wasn't loaded â€” that's fine here.
	err := runCmd(ctx, "launchctl", "bootout", "system", darwinLaunchDaemonPath)
	if err != nil && strings.Contains(err.Error(), "Could not find specified service") {
		return nil
	}
	return err
}

func kickstartLaunchd() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return runCmd(ctx, "launchctl", "kickstart", "-k", "system/"+darwinLaunchLabel)
}

// queryLaunchdState maps launchctl print output to the same vocabulary
// Windows uses: running | stopped | not_found | unknown.
func queryLaunchdState() (string, string) {
	if _, err := os.Stat(darwinLaunchDaemonPath); err != nil {
		return "not_found", "Themisto launchd service is not installed."
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "launchctl", "print", "system/"+darwinLaunchLabel).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if strings.Contains(text, "Could not find service") {
			return "stopped", "LaunchDaemon plist is present but the service is not loaded."
		}
		if text == "" {
			text = err.Error()
		}
		return "unknown", "Could not query the macOS launchd service: " + text
	}
	if pid := extractLaunchctlField(text, "pid = "); pid != "" {
		if p, _ := strconv.Atoi(pid); p > 0 {
			return "running", fmt.Sprintf("Themisto launchd service is running (pid %d).", p)
		}
	}
	if exitCode := extractLaunchctlField(text, "last exit code = "); exitCode != "" {
		return "stopped", fmt.Sprintf("Themisto launchd service is installed but stopped (last exit code %s).", exitCode)
	}
	return "stopped", "Themisto launchd service is installed but stopped."
}

func waitForLaunchdRunning(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, _ := queryLaunchdState()
		if state == "running" {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("service did not reach running state within %s", timeout)
}

func extractLaunchctlField(output, prefix string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimRight(strings.TrimSpace(strings.TrimPrefix(line, prefix)), ";")
		}
		if idx := strings.Index(line, prefix); idx >= 0 {
			val := strings.TrimSpace(line[idx+len(prefix):])
			return strings.TrimSpace(strings.TrimRight(val, ";"))
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Config + cert helpers (mirror the Windows versions)
// ---------------------------------------------------------------------------

func loadOrCreateDarwinConfig(sourceDir string) (map[string]interface{}, error) {
	installed, installedErr := loadConfigMap(darwinConfigPath)
	sourceCfgPath := filepath.Join(sourceDir, "agent.json")
	source, sourceErr := loadConfigMap(sourceCfgPath)

	if installedErr == nil {
		if sourceErr == nil {
			mergeDarwinConfigDefaults(installed, source)
		}
		return installed, nil
	}
	if sourceErr == nil {
		return source, nil
	}
	return map[string]interface{}{
		"agent_id":                         "",
		"gateway_url":                      "",
		"default_decision":                 "forward",
		"listen_addr":                      "127.0.0.1:9090",
		"https_intercept_enabled":          false,
		"https_intercept_domains":          []string{},
		"https_intercept_fail_mode":        "fail_open",
		"https_intercept_protocols":        []string{"http"},
		"https_intercept_capture_mode":     "encrypted_full_body",
		"prompt_semantics_enabled":         true,
		"prompt_semantics_local_url":       "http://127.0.0.1:17177/v1/classify",
		"prompt_semantics_gateway_enabled": false,
		"prompt_semantics_local_timeout":   "1500ms",
		"prompt_semantics_gateway_timeout": "900ms",
		"cert_path":                        filepath.Join(darwinCertsDir, "device.crt"),
		"key_path":                         filepath.Join(darwinCertsDir, "device.key"),
		"ca_path":                          filepath.Join(darwinCertsDir, "ca-chain.pem"),
	}, nil
}

// mergeDarwinConfigDefaults fills empty fields in installed from source. Never
// overwrites existing non-empty values â€” preserves enrolled identity across
// reinstalls, exactly like mergeConfigDefaults on Windows.
func mergeDarwinConfigDefaults(installed, source map[string]interface{}) {
	for _, key := range []string{
		"gateway_url", "backend_url", "listen_addr", "org_name",
		"default_decision", "https_intercept_fail_mode",
		"https_intercept_capture_mode",
		"prompt_semantics_policy", "prompt_semantics_local_url",
		"prompt_semantics_local_timeout", "prompt_semantics_gateway_timeout",
	} {
		if getString(installed, key) == "" && getString(source, key) != "" {
			installed[key] = source[key]
		}
	}
	for _, key := range []string{
		"prompt_semantics_enabled", "prompt_semantics_gateway_enabled",
	} {
		if _, ok := installed[key]; !ok {
			if value, exists := source[key]; exists {
				installed[key] = value
			}
		}
	}
}

// canonicalizeDarwinCredentialPaths forces absolute canonical cert paths into
// the installed config. Relative paths from dev configs must never leak into
// /etc/themisto/agent.json because launchd runs the agent with `/` as cwd.
func canonicalizeDarwinCredentialPaths(cfg map[string]interface{}) {
	canon := map[string]string{
		"cert_path": filepath.Join(darwinCertsDir, "device.crt"),
		"key_path":  filepath.Join(darwinCertsDir, "device.key"),
		"ca_path":   filepath.Join(darwinCertsDir, "ca-chain.pem"),
	}
	for key, path := range canon {
		current := strings.TrimSpace(getString(cfg, key))
		if current == "" || !filepath.IsAbs(current) {
			cfg[key] = path
		}
	}
}

// detectAndCopyDarwinCerts copies device.crt/device.key/ca-chain.pem from
// sourceDir (or sourceDir/certs) into /etc/themisto/identity with tight modes.
// Returns true only when all three files were copied. Mirrors the Windows
// behavior so that bundled-cert installers work identically on Mac.
func detectAndCopyDarwinCerts(sourceDir string, cfg map[string]interface{}) bool {
	searchDirs := []string{sourceDir, filepath.Join(sourceDir, "certs")}

	certFiles := []struct {
		file     string
		cfgKey   string
		destName string
		mode     os.FileMode
	}{
		{"device.crt", "cert_path", "device.crt", 0444},
		{"device.key", "key_path", "device.key", 0400},
		{"ca-chain.pem", "ca_path", "ca-chain.pem", 0444},
	}

	for _, dir := range searchDirs {
		allFound := true
		for _, c := range certFiles {
			if _, err := os.Stat(filepath.Join(dir, c.file)); err != nil {
				allFound = false
				break
			}
		}
		if !allFound {
			continue
		}
		for _, c := range certFiles {
			src := filepath.Join(dir, c.file)
			dst := filepath.Join(darwinCertsDir, c.destName)
			data, err := os.ReadFile(src)
			if err != nil {
				fmt.Printf("[WARN] Could not read %s: %v\n", src, err)
				return false
			}
			if err := os.WriteFile(dst, data, c.mode); err != nil {
				fmt.Printf("[WARN] Could not write %s: %v\n", dst, err)
				return false
			}
			cfg[c.cfgKey] = dst
		}
		return true
	}

	for _, c := range certFiles {
		if getString(cfg, c.cfgKey) == "" {
			cfg[c.cfgKey] = filepath.Join(darwinCertsDir, c.destName)
		}
	}
	return false
}

func isReadyToRunDarwin(cfg map[string]interface{}) bool {
	if getString(cfg, "gateway_url") == "" || getString(cfg, "agent_id") == "" {
		return false
	}
	for _, key := range []string{"cert_path", "key_path", "ca_path"} {
		p := strings.TrimSpace(getString(cfg, key))
		if p == "" {
			return false
		}
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}

func doDarwinEnrollmentDuringInstall(cfg map[string]interface{}, logger *stdLogger) error {
	return runActivation(activationOptions{
		ConfigPath:      darwinConfigPath,
		BackendURL:      getString(cfg, "backend_url"),
		GatewayURL:      getString(cfg, "gateway_url"),
		DeviceID:        getString(cfg, "device_id"),
		EnrollmentToken: getString(cfg, "enrollment_token"),
		OrgName:         getString(cfg, "org_name"),
		InsecureTLS:     getBool(cfg, "insecure_enrollment_tls"),
		ClearToken:      true,
	}, logger)
}

// ---------------------------------------------------------------------------
// Proxy emergency off (keeps existing implementation shape from v0)
// ---------------------------------------------------------------------------

func runProxyOff(logger *stdLogger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	services, err := listAllNetworkServices(ctx)
	if err != nil {
		return fmt.Errorf("list network services: %w", err)
	}
	if len(services) == 0 {
		return nil
	}

	var firstErr error
	for _, svc := range services {
		if err := runCmd(ctx, "networksetup", "-setwebproxystate", svc, "off"); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := runCmd(ctx, "networksetup", "-setsecurewebproxystate", svc, "off"); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil {
		return fmt.Errorf("disable system proxy: %w", firstErr)
	}
	logger.Info("system web proxies disabled", "services", strings.Join(services, ", "))
	return nil
}

func listAllNetworkServices(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "networksetup", "-listallnetworkservices").Output()
	if err != nil {
		return nil, err
	}
	var services []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		v := strings.TrimSpace(line)
		if v == "" || strings.HasPrefix(v, "An asterisk") || strings.HasPrefix(v, "*") {
			continue
		}
		services = append(services, v)
	}
	return services, nil
}

// ---------------------------------------------------------------------------
// process helpers
// ---------------------------------------------------------------------------

func runCmd(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return nil
}

func isRoot() bool { return os.Geteuid() == 0 }

func self() string {
	if p, err := os.Executable(); err == nil {
		return p
	}
	return "themisto-agent"
}

func selfDir() string {
	if p, err := os.Executable(); err == nil {
		return filepath.Dir(p)
	}
	return "."
}
