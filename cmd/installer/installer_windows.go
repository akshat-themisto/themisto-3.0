//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

//go:embed all:assets
var installerAssets embed.FS

//go:embed assets/agent.json
var defaultConfig []byte

// Browser extension source files â€” build step must copy adapter/prompt/browser/*
// into assets/extensions/ before building the installer.
//
//go:embed all:assets/extensions
var extensionFS embed.FS

//go:embed all:assets/bootstrap
var bootstrapFS embed.FS

//go:embed all:assets/classifier
var classifierFS embed.FS

const (
	// Chromium extension ID â€” must match desktop/browser_extensions.go extensionID.
	chromiumExtID = "bgijehgoebfjapkdoopgbmkfhganpmna"

	installDir         = `C:\Program Files\Themisto`
	configDir          = `C:\ProgramData\Themisto`
	certsDir           = `C:\ProgramData\Themisto\certs`
	extensionDir       = `C:\ProgramData\Themisto\extensions`
	classifierDir      = `C:\Program Files\Themisto\classifier`
	classifierModelDir = `C:\ProgramData\Themisto\models\deberta`
	classifierTaskName = "ThemistoSemanticClassifier"
	taskName           = "ThemistoAgent"
	appName            = "Themisto"
	publisher          = "Themisto Labs"
	uninstallKey       = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Themisto`
)

var (
	agentExePath           = filepath.Join(installDir, "themisto-agent.exe")
	desktopExePath         = filepath.Join(installDir, "themisto-desktop.exe")
	classifierLauncherPath = filepath.Join(classifierDir, "start-semantic-classifier.ps1")
	configPath             = filepath.Join(configDir, "agent.json")
	installerCopyPath      = filepath.Join(installDir, "ThemistoSetup.exe")
	uninstallerExePath     = filepath.Join(installDir, "ThemistoUninstall.exe")
)

func hiddenCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	return cmd
}

func reportInstallerError(err error) {
	if err == nil {
		return
	}
	showInstallerMessage(0, "Themisto Setup", err.Error(), mbIconError)
}

// --- Progress window (Win32) ---

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procSetWindowTextW  = user32.NewProc("SetWindowTextW")
	procIsUserAnAdmin   = shell32.NewProc("IsUserAnAdmin")
	procShellExecuteW   = shell32.NewProc("ShellExecuteW")
	procRegCreateKeyExW = advapi32.NewProc("RegCreateKeyExW")
	procRegSetValueExW  = advapi32.NewProc("RegSetValueExW")
	procRegCloseKey     = advapi32.NewProc("RegCloseKey")
	procRegDeleteKeyW   = advapi32.NewProc("RegDeleteKeyW")
)

func runInstall() error {
	if !isAdmin() {
		fmt.Println("[Themisto] Requesting administrator privileges...")
		if err := relaunchElevated(); err != nil {
			return fmt.Errorf("could not elevate: %w\nPlease right-click and choose 'Run as administrator'", err)
		}
		os.Exit(0)
	}

	hideConsoleWindow()

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("   Themisto - One-Click Installer")
	fmt.Println("========================================")
	fmt.Println()

	var (
		cfg   map[string]interface{}
		ready bool
	)

	initialConfig, err := loadOrCreateInstalledConfig()
	if err != nil {
		return err
	}
	cfg, err = maybeCollectInstallerEnrollmentConfig(initialConfig)
	if err != nil {
		return fmt.Errorf("prepare enrollment bootstrap: %w", err)
	}

	type installStep struct {
		name     string
		fn       func() error
		critical bool
	}

	steps := []installStep{
		{"Creating directories", createDirectories, true},
		{"Stopping existing Themisto processes", stopInstalledThemistoProcesses, false},
		{"Installing agent binary", installAgentBinary, true},
		{"Installing desktop app", installDesktopBinary, true},
		{"Installing semantic classifier", installSemanticClassifierAssets, true},
		{"Preparing configuration", func() error {
			return writeConfigMap(configPath, cfg)
		}, true},
		{"Installing bootstrap credentials", func() error {
			if hasUsableCredentialFiles(cfg) {
				return nil
			}
			installed, err := installEmbeddedBootstrapCredentials(cfg)
			if err != nil {
				return err
			}
			if !installed {
				return nil
			}
			return writeConfigMap(configPath, cfg)
		}, true},
		{"Running device enrollment", func() error {
			if !shouldAttemptEnrollment(cfg) || hasUsableCredentialFiles(cfg) {
				return nil
			}
			if err := activateInstalledAgent(); err != nil {
				return err
			}
			reloaded, err := loadConfigMap(configPath)
			if err != nil {
				return fmt.Errorf("reload config after activation: %w", err)
			}
			cfg = prepareInstalledConfig(reloaded, nil)
			return writeConfigMap(configPath, cfg)
		}, true},
		{"Registering agent service", registerAgentTask, true},
		{"Registering semantic classifier", registerSemanticClassifierTask, false},
		{"Setting up browser extensions", setupBrowserExtensions, false},
		{"Creating Start Menu shortcut", createStartMenuShortcut, false},
		{"Creating Desktop shortcut", createDesktopShortcut, false},
		{"Registering in Add/Remove Programs", registerUninstaller, false},
		{"Starting agent service", func() error {
			ready = isReadyToRun(cfg)
			if !ready {
				fmt.Println("      Startup skipped because enrollment/config is still incomplete.")
				return nil
			}
			if err := startSemanticClassifierTask(); err != nil {
				fmt.Printf("[WARN] Semantic classifier did not start: %v\n", err)
			}
			return startAgentTask()
		}, true},
		{"Launching desktop app", launchDesktopApp, false},
	}

	progress := startInstallerProgressWindow(len(steps))

	for i, step := range steps {
		progress.SetStep(i, step.name)
		fmt.Printf("[%d/%d] %s...\n", i+1, len(steps), step.name)
		if err := step.fn(); err != nil {
			if step.critical {
				fmt.Printf("[FAIL] %s: %v\n", step.name, err)
				if progress.enabled {
					progress.Fail(fmt.Sprintf("Themisto setup could not finish.\n\nStep: %s\nError: %v", step.name, err))
				} else {
					fmt.Println()
					fmt.Println("========================================")
					fmt.Println("   Installation Failed!")
					fmt.Println("========================================")
					fmt.Printf("\nCritical step failed: %s\n", step.name)
					fmt.Printf("Error: %v\n", err)
					fmt.Println()
					fmt.Println("Press Enter to close this window...")
					fmt.Scanln()
				}
				return fmt.Errorf("critical step %q failed: %w", step.name, err)
			}
			fmt.Printf("[WARN] %s: %v\n", step.name, err)
		} else {
			fmt.Printf("[OK]   %s\n", step.name)
		}
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("   Installation Complete!")
	fmt.Println("========================================")
	fmt.Println()
	if ready {
		fmt.Println("The Themisto agent was installed, enrolled, and started successfully.")
		fmt.Println("The desktop app is now launching.")
	} else {
		fmt.Println("The desktop app is now launching.")
		fmt.Println("If your device is not yet enrolled, the app will guide you through setup.")
	}
	fmt.Println()
	if progress.enabled {
		successMessage := "Themisto was installed and the desktop app is now launching."
		if !ready {
			successMessage = "Themisto was installed. The desktop app is now launching and can finish enrollment for this device."
		}
		progress.Complete(successMessage)
	} else {
		fmt.Println("Press Enter to close this window...")
		fmt.Scanln()
	}

	return nil
}

func runUninstall() error {
	if !isAdmin() {
		fmt.Println("[Themisto] Requesting administrator privileges...")
		if err := relaunchElevated(); err != nil {
			return fmt.Errorf("could not elevate: %w", err)
		}
		os.Exit(0)
	}

	hideConsoleWindow()

	// Confirmation dialog
	if !showInstallerConfirm(0, "Themisto Uninstall",
		"Are you sure you want to uninstall Themisto?\n\nThis will stop the agent, remove the application, and clean up browser extensions.") {
		return nil
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("   Themisto - Uninstaller")
	fmt.Println("========================================")
	fmt.Println()

	fmt.Println("[..] Stopping agent service...")
	_ = hiddenCommand("schtasks.exe", "/End", "/TN", taskName).Run()
	_ = hiddenCommand("schtasks.exe", "/End", "/TN", classifierTaskName).Run()
	time.Sleep(2 * time.Second)

	fmt.Println("[..] Stopping Themisto processes...")
	stopInstalledProcessByPath(agentExePath)
	stopInstalledProcessByPath(desktopExePath)
	time.Sleep(1 * time.Second)

	fmt.Println("[..] Removing scheduled task...")
	_ = hiddenCommand("schtasks.exe", "/Delete", "/TN", taskName, "/F").Run()
	_ = hiddenCommand("schtasks.exe", "/Delete", "/TN", classifierTaskName, "/F").Run()

	fmt.Println("[..] Clearing system proxy...")
	clearSystemProxy()

	fmt.Println("[..] Removing browser extension policies...")
	removeBrowserExtensionPolicies()

	fmt.Println("[..] Removing Start Menu shortcut...")
	removeStartMenuShortcut()

	fmt.Println("[..] Removing Desktop shortcut...")
	removeDesktopShortcut()

	fmt.Println("[..] Removing from Add/Remove Programs...")
	removeUninstallEntry()

	// Ask whether to also remove config/data
	removeData := showInstallerConfirm(0, "Themisto Uninstall",
		"Do you also want to remove Themisto configuration and device credentials?\n\n"+
			"Location: "+configDir+"\n\n"+
			"Choose \"Yes\" for a clean removal.\n"+
			"Choose \"No\" to keep configuration (useful if you plan to reinstall).")

	if removeData {
		fmt.Println("[..] Removing configuration and data...")
		_ = os.RemoveAll(configDir)
	} else {
		fmt.Println("[--] Keeping configuration at:", configDir)
	}

	fmt.Println("[..] Removing install directory...")
	// Self-delete: the running exe is inside installDir, so we schedule a
	// delayed removal via cmd.exe after this process exits.
	selfDeleteInstallDir()

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("   Themisto Uninstalled")
	fmt.Println("========================================")
	fmt.Println()

	msg := "Themisto has been uninstalled successfully."
	if !removeData {
		msg += "\n\nConfiguration was kept at:\n" + configDir
	}
	showInstallerMessage(0, "Themisto Uninstall", msg, mbIconInfo)

	return nil
}

// selfDeleteInstallDir spawns a background cmd.exe that waits briefly then
// removes the install directory (including the running uninstaller exe).
func selfDeleteInstallDir() {
	// Try direct removal first (works if we're not running from installDir).
	if err := os.RemoveAll(installDir); err == nil {
		return
	}
	// Fall back to delayed self-delete via cmd.exe.
	cmd := exec.Command("cmd.exe", "/C",
		fmt.Sprintf(`ping 127.0.0.1 -n 3 >nul & rmdir /s /q "%s"`, installDir))
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	_ = cmd.Start()
}

// --- Install steps ---

func createDirectories() error {
	dirs := []string{installDir, configDir, certsDir, extensionDir,
		filepath.Join(extensionDir, "chromium"),
		filepath.Join(extensionDir, "firefox"),
		classifierDir,
		classifierModelDir,
		filepath.Join(configDir, "logs"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}

func installAgentBinary() error {
	agentBinary, err := readRequiredInstallerAsset("assets/themisto-agent.exe")
	if err != nil {
		return err
	}
	if len(agentBinary) == 0 {
		return fmt.Errorf("agent binary not embedded")
	}
	return os.WriteFile(agentExePath, agentBinary, 0755)
}

func installDesktopBinary() error {
	desktopBinary, err := readRequiredInstallerAsset("assets/themisto-desktop.exe")
	if err != nil {
		return err
	}
	if len(desktopBinary) == 0 {
		return fmt.Errorf("desktop binary not embedded")
	}
	return os.WriteFile(desktopExePath, desktopBinary, 0755)
}

func readRequiredInstallerAsset(path string) ([]byte, error) {
	data, err := installerAssets.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s missing; run the installer asset build before packaging: %w", path, err)
	}
	return data, nil
}

func installSemanticClassifierAssets() error {
	if err := copyEmbeddedTree(classifierFS, "assets/classifier", classifierDir); err != nil {
		return err
	}
	if embeddedDirExists(classifierFS, "assets/classifier/model") {
		if err := copyEmbeddedTree(classifierFS, "assets/classifier/model", classifierModelDir); err != nil {
			return fmt.Errorf("copy bundled semantic model: %w", err)
		}
		_ = os.RemoveAll(filepath.Join(classifierDir, "model"))
	}
	return nil
}

func stopInstalledThemistoProcesses() error {
	_ = hiddenCommand("schtasks.exe", "/End", "/TN", taskName).Run()
	_ = hiddenCommand("schtasks.exe", "/End", "/TN", classifierTaskName).Run()
	time.Sleep(2 * time.Second)

	stopInstalledProcessByPath(agentExePath)
	stopInstalledProcessByPath(desktopExePath)
	time.Sleep(1 * time.Second)
	return nil
}

func stopInstalledProcessByPath(targetPath string) {
	quotedPath := strings.ReplaceAll(targetPath, `'`, `''`)
	ps := fmt.Sprintf(`Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object { $_.ExecutablePath -eq '%s' } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }`, quotedPath)
	_ = hiddenCommand("powershell", "-NoProfile", "-Command", ps).Run()
}

func loadOrCreateInstalledConfig() (map[string]interface{}, error) {
	var installed map[string]interface{}
	if cfg, err := loadConfigMap(configPath); err == nil {
		installed = cfg
	}

	var embedded map[string]interface{}
	if len(defaultConfig) > 0 {
		if err := json.Unmarshal(defaultConfig, &embedded); err != nil {
			return nil, fmt.Errorf("parse embedded installer config: %w", err)
		}
	}

	return prepareInstalledConfig(installed, embedded), nil
}

func prepareInstalledConfig(installed, embedded map[string]interface{}) map[string]interface{} {
	cfg := cloneConfigMap(installed)
	if cfg == nil {
		cfg = cloneConfigMap(embedded)
	}
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	if embedded != nil {
		mergeInstalledConfigDefaults(cfg, embedded)
	}
	ensureInstallerConfigDefaults(cfg)
	canonicalizeInstalledCredentialPaths(cfg)
	return cfg
}

func cloneConfigMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mergeInstalledConfigDefaults(installed, embedded map[string]interface{}) {
	if shouldAdoptEmbeddedBootstrap(installed, embedded) {
		for _, key := range []string{
			"agent_id", "device_id", "gateway_url", "backend_url",
			"org_name", "enrollment_token", "backend_ca_path",
		} {
			if value := strings.TrimSpace(getString(embedded, key)); value != "" {
				installed[key] = embedded[key]
			}
		}
		if _, ok := embedded["insecure_enrollment_tls"]; ok {
			installed["insecure_enrollment_tls"] = embedded["insecure_enrollment_tls"]
		}
	}

	for _, key := range []string{
		"agent_id", "device_id", "gateway_url", "backend_url", "org_name",
		"enrollment_token", "backend_ca_path", "listen_addr",
		"default_decision", "cert_path", "key_path", "ca_path",
		"https_intercept_fail_mode", "https_intercept_capture_mode",
		"prompt_semantics_policy", "prompt_semantics_local_url",
		"prompt_semantics_local_timeout", "prompt_semantics_gateway_timeout",
	} {
		if strings.TrimSpace(getString(installed, key)) == "" && strings.TrimSpace(getString(embedded, key)) != "" {
			installed[key] = embedded[key]
		}
	}

	for _, key := range []string{
		"https_intercept_enabled", "https_intercept_domains",
		"https_intercept_protocols", "insecure_enrollment_tls",
		"prompt_semantics_enabled", "prompt_semantics_gateway_enabled",
	} {
		if _, ok := installed[key]; !ok {
			if value, exists := embedded[key]; exists {
				installed[key] = value
			}
		}
	}
}

func shouldAdoptEmbeddedBootstrap(installed, embedded map[string]interface{}) bool {
	return shouldAttemptEnrollment(embedded) && !hasUsableCredentialFiles(installed)
}

func ensureInstallerConfigDefaults(cfg map[string]interface{}) {
	if strings.TrimSpace(getString(cfg, "default_decision")) == "" {
		cfg["default_decision"] = "forward"
	}
	if strings.TrimSpace(getString(cfg, "listen_addr")) == "" {
		cfg["listen_addr"] = "127.0.0.1:9090"
	}
	if _, ok := cfg["https_intercept_enabled"]; !ok {
		cfg["https_intercept_enabled"] = false
	}
	if _, ok := cfg["https_intercept_domains"]; !ok {
		cfg["https_intercept_domains"] = []string{}
	}
	if strings.TrimSpace(getString(cfg, "https_intercept_fail_mode")) == "" {
		cfg["https_intercept_fail_mode"] = "fail_open"
	}
	if _, ok := cfg["https_intercept_protocols"]; !ok {
		cfg["https_intercept_protocols"] = []string{"http"}
	}
	if strings.TrimSpace(getString(cfg, "https_intercept_capture_mode")) == "" {
		cfg["https_intercept_capture_mode"] = "encrypted_full_body"
	}
	if _, ok := cfg["prompt_semantics_enabled"]; !ok {
		cfg["prompt_semantics_enabled"] = true
	}
	if strings.TrimSpace(getString(cfg, "prompt_semantics_local_url")) == "" || strings.TrimSpace(getString(cfg, "prompt_semantics_local_url")) == "http://127.0.0.1:17176/v1/classify" {
		cfg["prompt_semantics_local_url"] = "http://127.0.0.1:17177/v1/classify"
	}
	if _, ok := cfg["prompt_semantics_gateway_enabled"]; !ok {
		cfg["prompt_semantics_gateway_enabled"] = true
	}
	if strings.TrimSpace(getString(cfg, "prompt_semantics_local_timeout")) == "" {
		cfg["prompt_semantics_local_timeout"] = "250ms"
	}
	if strings.TrimSpace(getString(cfg, "prompt_semantics_gateway_timeout")) == "" {
		cfg["prompt_semantics_gateway_timeout"] = "900ms"
	}
}

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

func shouldAttemptEnrollment(cfg map[string]interface{}) bool {
	return strings.TrimSpace(getString(cfg, "backend_url")) != "" &&
		strings.TrimSpace(getString(cfg, "device_id")) != "" &&
		strings.TrimSpace(getString(cfg, "enrollment_token")) != "" &&
		strings.TrimSpace(getString(cfg, "org_name")) != ""
}

func hasUsableCredentialFiles(cfg map[string]interface{}) bool {
	if cfg == nil {
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

func isReadyToRun(cfg map[string]interface{}) bool {
	if strings.TrimSpace(getString(cfg, "gateway_url")) == "" || strings.TrimSpace(getString(cfg, "agent_id")) == "" {
		return false
	}
	return hasUsableCredentialFiles(cfg)
}

func activateInstalledAgent() error {
	cmd := hiddenCommand(agentExePath, "-activate", "-config", configPath)
	cmd.Dir = installDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("activate installed agent: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func installEmbeddedBootstrapCredentials(cfg map[string]interface{}) (bool, error) {
	required := []struct {
		assetName string
		destPath  string
		mode      os.FileMode
		cfgKey    string
	}{
		{assetName: "device.crt", destPath: filepath.Join(certsDir, "device.crt"), mode: 0444, cfgKey: "cert_path"},
		{assetName: "device.key", destPath: filepath.Join(certsDir, "device.key"), mode: 0400, cfgKey: "key_path"},
		{assetName: "ca-chain.pem", destPath: filepath.Join(certsDir, "ca-chain.pem"), mode: 0444, cfgKey: "ca_path"},
	}

	found := 0
	type stagedFile struct {
		data []byte
		meta struct {
			assetName string
			destPath  string
			mode      os.FileMode
			cfgKey    string
		}
	}
	staged := make([]stagedFile, 0, len(required))
	for _, file := range required {
		data, err := bootstrapFS.ReadFile("assets/bootstrap/" + file.assetName)
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(data)) == "" {
			continue
		}
		entry := stagedFile{data: data}
		entry.meta.assetName = file.assetName
		entry.meta.destPath = file.destPath
		entry.meta.mode = file.mode
		entry.meta.cfgKey = file.cfgKey
		staged = append(staged, entry)
		found++
	}

	if found == 0 {
		return false, nil
	}
	if found != len(required) {
		return false, fmt.Errorf("embedded bootstrap credentials are incomplete; expected device.crt, device.key, and ca-chain.pem")
	}

	for _, file := range staged {
		if err := os.WriteFile(file.meta.destPath, file.data, file.meta.mode); err != nil {
			return false, fmt.Errorf("write embedded bootstrap credential %s: %w", file.meta.assetName, err)
		}
		cfg[file.meta.cfgKey] = file.meta.destPath
	}
	return true, nil
}

func loadConfigMap(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config JSON: %w", err)
	}
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	return cfg, nil
}

func writeConfigMap(path string, cfg map[string]interface{}) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

func getString(cfg map[string]interface{}, key string) string {
	if cfg == nil {
		return ""
	}
	value, ok := cfg[key]
	if !ok {
		return ""
	}
	str, _ := value.(string)
	return str
}

func registerAgentTask() error {
	_ = hiddenCommand("schtasks.exe", "/Delete", "/TN", taskName, "/F").Run()
	cmd := hiddenCommand("schtasks.exe",
		"/Create",
		"/TN", taskName,
		"/TR", fmt.Sprintf(`"%s" -config "%s"`, agentExePath, configPath),
		"/SC", "ONSTART",
		"/RU", "SYSTEM",
		"/RL", "HIGHEST",
		"/F",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("register task: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func registerSemanticClassifierTask() error {
	if _, err := os.Stat(classifierLauncherPath); err != nil {
		return fmt.Errorf("classifier launcher missing: %w", err)
	}
	_ = hiddenCommand("schtasks.exe", "/Delete", "/TN", classifierTaskName, "/F").Run()
	task := fmt.Sprintf(`powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%s" -ModelDir "%s" -ClassifierDir "%s" -Port 17177`, classifierLauncherPath, classifierModelDir, classifierDir)
	cmd := hiddenCommand("schtasks.exe",
		"/Create",
		"/TN", classifierTaskName,
		"/TR", task,
		"/SC", "ONSTART",
		"/RU", "SYSTEM",
		"/RL", "HIGHEST",
		"/F",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("register semantic classifier task: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func startSemanticClassifierTask() error {
	out, err := hiddenCommand("schtasks.exe", "/Run", "/TN", classifierTaskName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("start semantic classifier task: %s: %w", strings.TrimSpace(string(out)), err)
	}
	time.Sleep(2 * time.Second)
	return nil
}

func setupBrowserExtensions() error {
	// Write embedded extension files to disk.
	if err := writeExtensionFiles(); err != nil {
		return fmt.Errorf("write extension files: %w", err)
	}

	// Install extensions for detected browsers
	browsers := map[string][]string{
		"chrome": {`HKLM\SOFTWARE\Google\Chrome`, `HKLM\SOFTWARE\WOW6432Node\Google\Chrome`},
		"edge":   {`HKLM\SOFTWARE\Microsoft\Edge`},
		"brave":  {`HKLM\SOFTWARE\BraveSoftware\Brave-Browser`},
	}

	policyKeys := map[string]string{
		"chrome": `SOFTWARE\Policies\Google\Chrome\ExtensionInstallForcelist`,
		"edge":   `SOFTWARE\Policies\Microsoft\Edge\ExtensionInstallForcelist`,
		"brave":  `SOFTWARE\Policies\BraveSoftware\Brave\ExtensionInstallForcelist`,
	}

	updateURL := "http://127.0.0.1:17175/extensions/chromium/updates.xml"
	// Chromium extension ID must be 32 lowercase a-p chars.
	extID := "bgijehgoebfjapkdoopgbmkfhganpmna"
	extValue := extID + ";" + updateURL

	for browser, regPaths := range browsers {
		installed := false
		for _, rp := range regPaths {
			out, err := hiddenCommand("reg", "query", rp).CombinedOutput()
			if err == nil && len(out) > 0 {
				installed = true
				break
			}
		}
		if !installed {
			continue
		}
		policyKey, ok := policyKeys[browser]
		if !ok {
			continue
		}
		_ = hiddenCommand("reg", "add", `HKLM\`+policyKey,
			"/v", "1", "/t", "REG_SZ", "/d", extValue, "/f").Run()
	}

	// Firefox
	out, err := hiddenCommand("reg", "query", `HKLM\SOFTWARE\Mozilla\Mozilla Firefox`).CombinedOutput()
	if err == nil && len(out) > 0 {
		xpiPath := filepath.Join(extensionDir, "firefox", "themisto.xpi")
		_ = hiddenCommand("reg", "add", `HKLM\SOFTWARE\Mozilla\Firefox\Extensions`,
			"/v", "themisto-prompt-capture@themisto.local",
			"/t", "REG_SZ", "/d", xpiPath, "/f").Run()
	}

	return nil
}

// writeExtensionFiles copies the embedded Chromium extension files to disk
// and creates the Firefox .xpi (ZIP archive) from the embedded Firefox files.
func writeExtensionFiles() error {
	// Copy Chromium extension files as-is (unpacked extension).
	chromiumDir := filepath.Join(extensionDir, "chromium")
	if err := copyEmbeddedDir("assets/extensions/chromium", chromiumDir); err != nil {
		return fmt.Errorf("chromium extension: %w", err)
	}

	// Package Firefox extension as .xpi (ZIP format).
	firefoxDir := filepath.Join(extensionDir, "firefox")
	xpiPath := filepath.Join(firefoxDir, "themisto.xpi")
	if err := createXPI("assets/extensions/firefox", xpiPath); err != nil {
		return fmt.Errorf("firefox xpi: %w", err)
	}
	return nil
}

func copyEmbeddedDir(srcDir, destDir string) error {
	return copyEmbeddedTree(extensionFS, srcDir, destDir)
}

func copyEmbeddedTree(source embed.FS, srcDir, destDir string) error {
	return fs.WalkDir(source, srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(srcDir, path)
		dest := filepath.Join(destDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		data, err := source.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0644)
	})
}

func embeddedDirExists(source embed.FS, path string) bool {
	info, err := fs.Stat(source, path)
	return err == nil && info.IsDir()
}

func createXPI(srcDir, xpiPath string) error {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	if err := fs.WalkDir(extensionFS, srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(srcDir, path)
		data, err := extensionFS.ReadFile(path)
		if err != nil {
			return err
		}
		fw, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		_, err = fw.Write(data)
		return err
	}); err != nil {
		return err
	}

	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(xpiPath, buf.Bytes(), 0644)
}

func createStartMenuShortcut() error {
	startMenu := filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "Start Menu", "Programs")
	shortcutDir := filepath.Join(startMenu, appName)
	_ = os.MkdirAll(shortcutDir, 0755)

	ps := fmt.Sprintf(`$ws = New-Object -ComObject WScript.Shell; `+
		`$app = $ws.CreateShortcut('%s'); `+
		`$app.TargetPath = '%s'; $app.WorkingDirectory = '%s'; $app.IconLocation = '%s,0'; $app.Description = 'Themisto AI Governance'; $app.Save(); `+
		`$un = $ws.CreateShortcut('%s'); `+
		`$un.TargetPath = '%s'; $un.WorkingDirectory = '%s'; $un.IconLocation = '%s,0'; $un.Description = 'Uninstall Themisto'; $un.Save()`,
		filepath.Join(shortcutDir, "Themisto.lnk"),
		desktopExePath,
		installDir,
		desktopExePath,
		filepath.Join(shortcutDir, "Uninstall Themisto.lnk"),
		uninstallerExePath,
		installDir,
		uninstallerExePath,
	)
	return hiddenCommand("powershell", "-Command", ps).Run()
}

func createDesktopShortcut() error {
	desktop := filepath.Join(os.Getenv("PUBLIC"), "Desktop")
	ps := fmt.Sprintf(`$ws = New-Object -ComObject WScript.Shell; $sc = $ws.CreateShortcut('%s'); $sc.TargetPath = '%s'; $sc.WorkingDirectory = '%s'; $sc.IconLocation = '%s,0'; $sc.Description = 'Themisto AI Governance'; $sc.Save()`,
		filepath.Join(desktop, "Themisto.lnk"),
		desktopExePath,
		installDir,
		desktopExePath,
	)
	return hiddenCommand("powershell", "-Command", ps).Run()
}

func registerUninstaller() error {
	key := `HKLM\` + uninstallKey
	uninstallCmd := fmt.Sprintf(`"%s"`, uninstallerExePath)

	cmds := [][]string{
		{"reg", "add", key, "/v", "DisplayName", "/t", "REG_SZ", "/d", appName, "/f"},
		{"reg", "add", key, "/v", "Publisher", "/t", "REG_SZ", "/d", publisher, "/f"},
		{"reg", "add", key, "/v", "UninstallString", "/t", "REG_SZ", "/d", uninstallCmd, "/f"},
		{"reg", "add", key, "/v", "QuietUninstallString", "/t", "REG_SZ", "/d", uninstallCmd, "/f"},
		{"reg", "add", key, "/v", "InstallLocation", "/t", "REG_SZ", "/d", installDir, "/f"},
		{"reg", "add", key, "/v", "DisplayIcon", "/t", "REG_SZ", "/d", desktopExePath, "/f"},
		{"reg", "add", key, "/v", "DisplayVersion", "/t", "REG_SZ", "/d", "0.1.0", "/f"},
		{"reg", "add", key, "/v", "NoModify", "/t", "REG_DWORD", "/d", "1", "/f"},
		{"reg", "add", key, "/v", "NoRepair", "/t", "REG_DWORD", "/d", "1", "/f"},
	}
	for _, c := range cmds {
		_ = hiddenCommand(c[0], c[1:]...).Run()
	}

	// Copy installer binary as both ThemistoSetup.exe and ThemistoUninstall.exe
	self, err := os.Executable()
	if err != nil {
		return nil // non-critical
	}
	data, err := os.ReadFile(self)
	if err != nil {
		return nil
	}
	_ = os.WriteFile(installerCopyPath, data, 0755)
	_ = os.WriteFile(uninstallerExePath, data, 0755)

	return nil
}

func startAgentTask() error {
	out, err := hiddenCommand("schtasks.exe", "/Run", "/TN", taskName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("start task: %s: %w", strings.TrimSpace(string(out)), err)
	}
	time.Sleep(2 * time.Second)
	return nil
}

func launchDesktopApp() error {
	cmd := exec.Command(desktopExePath)
	cmd.Dir = installDir
	return cmd.Start()
}

// --- Uninstall helpers ---

func clearSystemProxy() {
	_ = hiddenCommand("reg", "add",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		"/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f").Run()
	_ = hiddenCommand("reg", "delete",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		"/v", "ProxyServer", "/f").Run()
	_ = hiddenCommand("netsh", "winhttp", "reset", "proxy").Run()
}

func removeBrowserExtensionPolicies() {
	keys := []string{
		`HKLM\SOFTWARE\Policies\Google\Chrome\ExtensionInstallForcelist`,
		`HKLM\SOFTWARE\Policies\Microsoft\Edge\ExtensionInstallForcelist`,
		`HKLM\SOFTWARE\Policies\BraveSoftware\Brave\ExtensionInstallForcelist`,
	}
	for _, k := range keys {
		// Only delete Themisto's value, not the entire key.
		out, err := hiddenCommand("reg", "query", k).CombinedOutput()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if !strings.Contains(line, chromiumExtID) {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 1 {
				continue
			}
			_ = hiddenCommand("reg", "delete", k, "/v", fields[0], "/f").Run()
		}
	}
	_ = hiddenCommand("reg", "delete", `HKLM\SOFTWARE\Mozilla\Firefox\Extensions`,
		"/v", "themisto-prompt-capture@themisto.local", "/f").Run()
}

func removeStartMenuShortcut() {
	startMenu := filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "Start Menu", "Programs")
	_ = os.RemoveAll(filepath.Join(startMenu, appName))
}

func removeDesktopShortcut() {
	desktop := filepath.Join(os.Getenv("PUBLIC"), "Desktop")
	_ = os.Remove(filepath.Join(desktop, "Themisto.lnk"))
}

func removeUninstallEntry() {
	_ = hiddenCommand("reg", "delete", `HKLM\`+uninstallKey, "/f").Run()
}

// --- UAC ---

func isAdmin() bool {
	ret, _, _ := procIsUserAnAdmin.Call()
	return ret != 0
}

func relaunchElevated() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	args := strings.Join(os.Args[1:], " ")

	verb, _ := syscall.UTF16PtrFromString("runas")
	exe, _ := syscall.UTF16PtrFromString(self)
	params, _ := syscall.UTF16PtrFromString(args)
	dir, _ := syscall.UTF16PtrFromString(filepath.Dir(self))

	ret, _, _ := procShellExecuteW.Call(
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
