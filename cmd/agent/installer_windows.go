//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	windows "github.com/themisto/agent/adapter/windows"
	"github.com/themisto/agent/pkg/winsvc"
)

const (
	installDir         = `C:\Program Files\Themisto`
	configDir          = `C:\ProgramData\Themisto`
	certsDir           = `C:\ProgramData\Themisto\certs`
	classifierDir      = `C:\Program Files\Themisto\classifier`
	classifierModelDir = `C:\ProgramData\Themisto\models\deberta`
	classifierTaskName = "ThemistoSemanticClassifier"
)

var (
	exePath                = filepath.Join(installDir, "themisto-agent.exe")
	configPath             = filepath.Join(configDir, "agent.json")
	classifierLauncherPath = filepath.Join(classifierDir, "start-semantic-classifier.ps1")
)

// ---------------------------------------------------------------------------
// Install
// ---------------------------------------------------------------------------

func runInstall(sourceDir string, logger *stdLogger) error {
	if sourceDir == "" {
		sourceDir = selfDir()
	}

	if !isAdmin() {
		fmt.Println("[Themisto] Requesting administrator privileges...")
		if err := relaunchElevated(sourceDir); err != nil {
			return fmt.Errorf("could not elevate: %w\nPlease right-click the exe and choose 'Run as administrator'", err)
		}
		os.Exit(0)
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("   Themisto Agent - Windows Installer")
	fmt.Println("========================================")
	fmt.Println()

	// 1. Create directories
	for _, dir := range []string{installDir, configDir, certsDir, classifierDir, classifierModelDir, filepath.Join(configDir, "logs")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	fmt.Println("[OK] Created directories")

	// 2. Stop existing service AND legacy task before copying binary
	fmt.Println("[..] Stopping existing agent (if running)...")
	legacyExists := winsvc.LegacyTaskExists()
	_ = winsvc.StopServiceAndWait(10 * time.Second)
	if legacyExists {
		winsvc.StopLegacyTask()
	}
	time.Sleep(2 * time.Second)

	// 3. Copy binary (retry with targeted kill if exe is locked)
	if err := copySelf(); err != nil {
		fmt.Println("[..] Binary locked, attempting to stop installed agent process...")
		killInstalledAgent()
		time.Sleep(1 * time.Second)
		if retryErr := copySelf(); retryErr != nil {
			return fmt.Errorf("copy binary (after retry): %w", retryErr)
		}
	}
	fmt.Println("[OK] Installed binary to", exePath)

	if err := copyClassifierAssets(sourceDir); err != nil {
		fmt.Printf("[WARN] Could not install semantic classifier assets: %v\n", err)
	} else {
		fmt.Println("[OK] Semantic classifier assets installed")
	}

	// 4. Handle config
	cfg, err := loadOrCreateConfig(sourceDir)
	if err != nil {
		return err
	}

	// 5. Handle certificates
	certsFound := detectAndCopyCerts(sourceDir, cfg)
	if certsFound {
		fmt.Println("[OK] Certificates installed")
	}
	canonicalizeInstalledCredentialPaths(cfg)
	canonicalizePromptSemanticsConfig(cfg)

	// 6. Write final config
	if err := writeJSON(configPath, cfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	fmt.Println("[OK] Configuration written to", configPath)

	// 7. Enrollment (if token present and no certs)
	if !certsFound && getString(cfg, "enrollment_token") != "" {
		fmt.Println("[..] Running device enrollment...")
		if err := doEnrollmentDuringInstall(cfg, logger); err != nil {
			fmt.Printf("[WARN] Enrollment failed: %v\n", err)
			fmt.Println("       You can enroll later with: themisto-agent.exe -activate ...")
		} else {
			fmt.Println("[OK] Device enrolled and certificates received")
			if updated, err := loadConfigMap(configPath); err == nil {
				cfg = updated
			}
		}
	}

	// 8. Install or update Windows Service (create-or-update, never deletes a working service)
	serviceRegistered := false
	fallbackPreserved := false
	var persistenceErr error
	if err := winsvc.InstallOrUpdateService(exePath, configPath); err != nil {
		persistenceErr = fmt.Errorf("install Windows Service: %w", err)
		fmt.Printf("[WARN] Could not install Windows Service: %v\n", err)
		if legacyExists {
			fmt.Println("[..] Falling back to legacy scheduled task...")
			winsvc.RestartLegacyTask()
			fallbackPreserved = true
		}
	} else {
		serviceRegistered = true
		fmt.Println("[OK] Windows Service registered")
	}
	if err := registerSemanticClassifierTask(); err != nil {
		fmt.Printf("[WARN] Could not register semantic classifier task: %v\n", err)
	}

	// 9. Validate before starting
	ready := isReadyToRun(cfg)

	// 10. Start service and verify
	serviceVerified := false
	if ready && serviceRegistered {
		if err := startSemanticClassifierTask(); err != nil {
			fmt.Printf("[WARN] Could not start semantic classifier task: %v\n", err)
		}
		if err := winsvc.StartService(); err != nil {
			persistenceErr = fmt.Errorf("start Windows Service: %w", err)
			fmt.Printf("[WARN] Could not start service: %v\n", err)
		} else if err := winsvc.WaitForRunning(15 * time.Second); err != nil {
			persistenceErr = fmt.Errorf("verify Windows Service: %w", err)
			fmt.Printf("[WARN] Service did not reach running state: %v\n", err)
		} else {
			serviceVerified = true
			fmt.Println("[OK] Agent service is running")
		}
	}

	// 11. Remove legacy task only after service is verified running
	if legacyExists {
		if serviceVerified {
			winsvc.RemoveLegacyTask()
			fmt.Println("[OK] Migrated from scheduled task to Windows Service")
		} else if ready {
			fmt.Println("[WARN] Service not verified running, keeping legacy scheduled task as fallback")
			winsvc.RestartLegacyTask()
			fallbackPreserved = true
		}
	}

	if persistenceErr != nil {
		fmt.Println()
		fmt.Println("========================================")
		fmt.Println("   Installed With Warning")
		fmt.Println("========================================")
		fmt.Println()
		if fallbackPreserved {
			fmt.Println("The Windows Service was not verified. The legacy scheduled task was kept as a fallback.")
		} else {
			fmt.Println("The Windows Service was not installed or verified successfully.")
		}
		fmt.Println()
		fmt.Println("Press Enter to close...")
		fmt.Scanln()
		return persistenceErr
	}

	// Print summary
	fmt.Println()
	fmt.Println("========================================")
	if ready {
		fmt.Println("   Installation complete!")
		fmt.Println("========================================")
		fmt.Println()
		fmt.Println("The agent is running and proxying traffic on", getString(cfg, "listen_addr"))
		fmt.Println()
		fmt.Println("Useful commands (run from C:\\Program Files\\Themisto):")
		fmt.Println("  themisto-agent.exe -service-status   Show service status")
		fmt.Println("  themisto-agent.exe -service-stop     Stop the agent")
		fmt.Println("  themisto-agent.exe -service-start    Start the agent")
		fmt.Println("  themisto-agent.exe -uninstall        Remove the agent")
		fmt.Println()
		fmt.Println("SAFETY: If your internet stops working, run:")
		fmt.Println("  themisto-agent.exe -proxy-off")
	} else {
		fmt.Println("   Installed (needs configuration)")
		fmt.Println("========================================")
		fmt.Println()
		fmt.Println("Edit:", configPath)
		fmt.Println("Set: agent_id, gateway_url")
		fmt.Println("Place certificates in:", certsDir)
		fmt.Println()
		fmt.Println("Then start:")
		fmt.Printf("  \"%s\" -service-start\n", exePath)
	}
	fmt.Println()
	fmt.Println("Press Enter to close...")
	fmt.Scanln()

	return nil
}

// ---------------------------------------------------------------------------
// Uninstall
// ---------------------------------------------------------------------------

func runUninstall(logger *stdLogger) error {
	if !isAdmin() {
		fmt.Println("[Themisto] Requesting administrator privileges...")
		if err := relaunchElevated(""); err != nil {
			return fmt.Errorf("could not elevate: %w", err)
		}
		os.Exit(0)
	}

	fmt.Println()
	fmt.Println("[..] Stopping agent service...")
	_ = winsvc.StopServiceAndWait(10 * time.Second)
	_ = exec.Command("schtasks.exe", "/End", "/TN", classifierTaskName).Run()

	fmt.Println("[..] Clearing system proxy...")
	restoreProxy(logger)

	fmt.Println("[..] Removing Windows Service...")
	_ = winsvc.RemoveService()

	// Also clean up any legacy scheduled task from previous versions
	winsvc.StopLegacyTask()
	winsvc.RemoveLegacyTask()
	_ = exec.Command("schtasks.exe", "/Delete", "/TN", classifierTaskName, "/F").Run()

	fmt.Println("[..] Removing install directory...")
	if err := os.RemoveAll(installDir); err != nil {
		fmt.Printf("[WARN] Could not remove %s: %v\n", installDir, err)
	}

	fmt.Println()
	fmt.Println("[OK] Themisto Agent uninstalled")
	fmt.Println("     Configuration kept at:", configDir)
	fmt.Println()
	fmt.Println("Press Enter to close...")
	fmt.Scanln()

	return nil
}

// ---------------------------------------------------------------------------
// Service management
// ---------------------------------------------------------------------------

func runServiceStart(logger *stdLogger) error {
	if !isAdmin() {
		if err := relaunchElevated(""); err != nil {
			return fmt.Errorf("could not elevate: %w", err)
		}
		os.Exit(0)
	}
	if err := startSemanticClassifierTask(); err != nil {
		fmt.Printf("[WARN] Semantic classifier did not start: %v\n", err)
	}
	if err := winsvc.StartService(); err != nil {
		return err
	}
	fmt.Println("[OK] Service started")
	return nil
}

func runServiceStop(logger *stdLogger) error {
	if !isAdmin() {
		if err := relaunchElevated(""); err != nil {
			return fmt.Errorf("could not elevate: %w", err)
		}
		os.Exit(0)
	}
	_ = winsvc.StopServiceAndWait(10 * time.Second)
	_ = exec.Command("schtasks.exe", "/End", "/TN", classifierTaskName).Run()
	fmt.Println("[OK] Service stopped")
	return nil
}

func runServiceStatus(logger *stdLogger) error {
	state, detail := winsvc.QueryState()
	fmt.Printf("Service state: %s\n", state)
	fmt.Printf("Detail: %s\n", detail)
	return nil
}

// ---------------------------------------------------------------------------
// Proxy emergency off
// ---------------------------------------------------------------------------

func defaultConfigPath() string { return configPath }

func runProxyOff(logger *stdLogger) error {
	if !isAdmin() {
		if err := relaunchElevated(""); err != nil {
			return fmt.Errorf("could not elevate: %w", err)
		}
		os.Exit(0)
	}
	restoreProxy(logger)
	fmt.Println("[OK] System proxy cleared. Your internet should work now.")
	return nil
}

// restoreProxy attempts to restore proxy settings from Themisto backup values.
// If no backup exists, falls back to clearing all Themisto-related proxy state.
func restoreProxy(logger *stdLogger) {
	if err := windows.EmergencyRestoreProxy(logger); err != nil {
		logger.Warn("backup-based restore failed, falling back to brute-force clear", "error", err)
		clearProxyFallback()
	}
}

// clearProxyFallback is the brute-force last resort when no backup values exist.
func clearProxyFallback() {
	_ = exec.Command("reg", "add",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		"/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f").Run()
	_ = exec.Command("reg", "delete",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		"/v", "ProxyServer", "/f").Run()
	_ = exec.Command("reg", "delete",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		"/v", "ProxyOverride", "/f").Run()
	_ = exec.Command("reg", "add",
		`HKLM\Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		"/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f").Run()
	_ = exec.Command("netsh", "winhttp", "reset", "proxy").Run()
	_ = exec.Command("reg", "delete", `HKLM\SOFTWARE\Themisto\Agent`, "/f").Run()
}

func copyClassifierAssets(sourceDir string) error {
	src := filepath.Join(sourceDir, "classifier")
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		return nil
	}
	if err := os.RemoveAll(classifierDir); err != nil {
		return fmt.Errorf("clear classifier dir: %w", err)
	}
	if err := copyDir(src, classifierDir); err != nil {
		return err
	}
	modelSrc := filepath.Join(src, "model")
	if info, err := os.Stat(modelSrc); err == nil && info.IsDir() {
		if err := copyDir(modelSrc, classifierModelDir); err != nil {
			return fmt.Errorf("copy bundled semantic model: %w", err)
		}
	}
	return nil
}

func registerSemanticClassifierTask() error {
	if _, err := os.Stat(classifierLauncherPath); err != nil {
		return nil
	}
	_ = exec.Command("schtasks.exe", "/Delete", "/TN", classifierTaskName, "/F").Run()
	task := fmt.Sprintf(`powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%s" -ModelDir "%s" -ClassifierDir "%s" -Port 17177`, classifierLauncherPath, classifierModelDir, classifierDir)
	out, err := exec.Command("schtasks.exe",
		"/Create",
		"/TN", classifierTaskName,
		"/TR", task,
		"/SC", "ONSTART",
		"/RU", "SYSTEM",
		"/RL", "HIGHEST",
		"/F",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("register semantic classifier task: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func startSemanticClassifierTask() error {
	if _, err := os.Stat(classifierLauncherPath); err != nil {
		return nil
	}
	out, err := exec.Command("schtasks.exe", "/Run", "/TN", classifierTaskName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("start semantic classifier task: %s: %w", strings.TrimSpace(string(out)), err)
	}
	time.Sleep(2 * time.Second)
	return nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

func loadOrCreateConfig(sourceDir string) (map[string]interface{}, error) {
	// Prefer existing installed config (preserves enrolled state on reinstall).
	installed, installedErr := loadConfigMap(configPath)
	sourceCfg := filepath.Join(sourceDir, "agent.json")
	source, sourceErr := loadConfigMap(sourceCfg)

	if installedErr == nil {
		if sourceErr == nil {
			mergeConfigDefaults(installed, source)
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
		"cert_path":                        filepath.Join(certsDir, "device.crt"),
		"key_path":                         filepath.Join(certsDir, "device.key"),
		"ca_path":                          filepath.Join(certsDir, "ca-chain.pem"),
	}, nil
}

// mergeConfigDefaults fills empty fields in installed config from source config.
// Never overwrites existing non-empty values â€” preserves enrolled identity.
func mergeConfigDefaults(installed, source map[string]interface{}) {
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

// canonicalizeInstalledCredentialPaths forces the installed runtime to use the
// canonical ProgramData cert layout. Relative paths from repo/dev configs must
// not leak into the installed config because the service runs with a different
// working directory.
func canonicalizeInstalledCredentialPaths(cfg map[string]interface{}) {
	for key, path := range map[string]string{
		"cert_path": filepath.Join(certsDir, "device.crt"),
		"key_path":  filepath.Join(certsDir, "device.key"),
		"ca_path":   filepath.Join(certsDir, "ca-chain.pem"),
	} {
		current := strings.TrimSpace(getString(cfg, key))
		if current == "" || !filepath.IsAbs(current) {
			cfg[key] = path
		}
	}
}

func canonicalizePromptSemanticsConfig(cfg map[string]interface{}) {
	if strings.TrimSpace(getString(cfg, "prompt_semantics_local_url")) == "" ||
		strings.TrimSpace(getString(cfg, "prompt_semantics_local_url")) == "http://127.0.0.1:17176/v1/classify" {
		cfg["prompt_semantics_local_url"] = "http://127.0.0.1:17177/v1/classify"
	}
}

func detectAndCopyCerts(sourceDir string, cfg map[string]interface{}) bool {
	searchDirs := []string{
		sourceDir,
		filepath.Join(sourceDir, "certs"),
	}

	certFiles := []struct {
		file     string
		cfgKey   string
		destName string
	}{
		{"device.crt", "cert_path", "device.crt"},
		{"device.key", "key_path", "device.key"},
		{"ca-chain.pem", "ca_path", "ca-chain.pem"},
	}

	for _, dir := range searchDirs {
		allFound := true
		for _, c := range certFiles {
			if _, err := os.Stat(filepath.Join(dir, c.file)); err != nil {
				allFound = false
				break
			}
		}
		if allFound {
			for _, c := range certFiles {
				src := filepath.Join(dir, c.file)
				dst := filepath.Join(certsDir, c.destName)
				data, err := os.ReadFile(src)
				if err != nil {
					fmt.Printf("[WARN] Could not read %s: %v\n", src, err)
					return false
				}
				if err := os.WriteFile(dst, data, 0400); err != nil {
					fmt.Printf("[WARN] Could not write %s: %v\n", dst, err)
					return false
				}
				cfg[c.cfgKey] = dst
			}
			return true
		}
	}

	for _, c := range certFiles {
		if getString(cfg, c.cfgKey) == "" {
			cfg[c.cfgKey] = filepath.Join(certsDir, c.destName)
		}
	}
	return false
}

func isReadyToRun(cfg map[string]interface{}) bool {
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

func doEnrollmentDuringInstall(cfg map[string]interface{}, logger *stdLogger) error {
	return runActivation(activationOptions{
		ConfigPath:      configPath,
		BackendURL:      getString(cfg, "backend_url"),
		GatewayURL:      getString(cfg, "gateway_url"),
		DeviceID:        getString(cfg, "device_id"),
		EnrollmentToken: getString(cfg, "enrollment_token"),
		OrgName:         getString(cfg, "org_name"),
		InsecureTLS:     getBool(cfg, "insecure_enrollment_tls"),
		ClearToken:      true,
	}, logger)
}

func writeJSON(path string, data map[string]interface{}) error {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0644)
}

// ---------------------------------------------------------------------------
// UAC elevation
// ---------------------------------------------------------------------------

func isAdmin() bool {
	mod := syscall.NewLazyDLL("shell32.dll")
	proc := mod.NewProc("IsUserAnAdmin")
	ret, _, _ := proc.Call()
	return ret != 0
}

func relaunchElevated(installSourceDir string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	rawArgs := os.Args[1:]
	// Inject -install-source-dir if we have one and it's not already present
	if installSourceDir != "" {
		hasFlag := false
		for _, a := range rawArgs {
			if strings.HasPrefix(a, "-install-source-dir") {
				hasFlag = true
				break
			}
		}
		if !hasFlag {
			rawArgs = append(rawArgs, fmt.Sprintf(`"-install-source-dir=%s"`, installSourceDir))
		}
	}
	args := strings.Join(rawArgs, " ")

	mod := syscall.NewLazyDLL("shell32.dll")
	proc := mod.NewProc("ShellExecuteW")

	verb, _ := syscall.UTF16PtrFromString("runas")
	exe, _ := syscall.UTF16PtrFromString(self)
	params, _ := syscall.UTF16PtrFromString(args)
	dir, _ := syscall.UTF16PtrFromString(filepath.Dir(self))

	ret, _, _ := proc.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(exe)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(dir)),
		1, // SW_SHOWNORMAL
	)
	if ret <= 32 {
		return fmt.Errorf("ShellExecute returned %d", ret)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

func selfDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

func copySelf() error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	selfAbs, _ := filepath.Abs(self)
	targetAbs, _ := filepath.Abs(exePath)
	if strings.EqualFold(selfAbs, targetAbs) {
		return nil
	}
	data, err := os.ReadFile(self)
	if err != nil {
		return fmt.Errorf("read binary: %w", err)
	}
	return os.WriteFile(exePath, data, 0755)
}

// killInstalledAgent terminates only the installed agent binary by matching
// its full path. Does not kill repo-run or dev agents.
func killInstalledAgent() {
	// Use WMIC to find the PID of the installed binary by full path.
	// This avoids killing dev/repo agents running from other locations.
	target := strings.ReplaceAll(exePath, `\`, `\\`)
	out, err := exec.Command("wmic", "process", "where",
		fmt.Sprintf(`ExecutablePath='%s'`, target),
		"get", "ProcessId", "/format:list").CombinedOutput()
	if err != nil {
		fmt.Printf("[WARN] Could not query installed agent process: %v\n", err)
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ProcessId=") {
			pid := strings.TrimPrefix(line, "ProcessId=")
			pid = strings.TrimSpace(pid)
			if pid != "" && pid != "0" {
				fmt.Printf("[..] Killing installed agent process (PID %s)\n", pid)
				_ = exec.Command("taskkill", "/F", "/PID", pid).Run()
			}
		}
	}
}
