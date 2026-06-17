package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type browserSpec struct {
	name      string
	key       string
	supported bool
}

var supportedBrowsers = []browserSpec{
	{"Chrome", "chrome", true},
	{"Edge", "edge", true},
	{"Brave", "brave", true},
	{"Firefox", "firefox", true},
}

var browserCatalog = func() []browserSpec {
	extra := []browserSpec{
		{"Opera", "opera", false},
		{"Opera GX", "opera_gx", false},
		{"Vivaldi", "vivaldi", false},
		{"Arc", "arc", false},
		{"Chromium", "chromium", false},
	}
	out := make([]browserSpec, 0, len(supportedBrowsers)+len(extra))
	out = append(out, supportedBrowsers...)
	out = append(out, extra...)
	return out
}()

// Browser registry paths for detection.
var browserRegistry = map[string][]string{
	"chrome": {
		`HKLM\SOFTWARE\Google\Chrome`,
		`HKLM\SOFTWARE\WOW6432Node\Google\Chrome`,
		`HKCU\SOFTWARE\Google\Chrome`,
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\chrome.exe`,
		`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\chrome.exe`,
	},
	"edge": {
		`HKLM\SOFTWARE\Microsoft\Edge`,
		`HKLM\SOFTWARE\WOW6432Node\Microsoft\Edge`,
		`HKCU\SOFTWARE\Microsoft\Edge`,
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\msedge.exe`,
		`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\msedge.exe`,
	},
	"brave": {
		`HKLM\SOFTWARE\BraveSoftware\Brave-Browser`,
		`HKLM\SOFTWARE\WOW6432Node\BraveSoftware\Brave-Browser`,
		`HKCU\SOFTWARE\BraveSoftware\Brave-Browser`,
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\brave.exe`,
		`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\brave.exe`,
	},
	"firefox": {
		`HKLM\SOFTWARE\Mozilla\Mozilla Firefox`,
		`HKLM\SOFTWARE\WOW6432Node\Mozilla\Mozilla Firefox`,
		`HKCU\SOFTWARE\Mozilla\Mozilla Firefox`,
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\firefox.exe`,
		`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\firefox.exe`,
	},
	"opera": {
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\opera.exe`,
		`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\opera.exe`,
	},
	"opera_gx": {
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\opera.exe`,
		`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\opera.exe`,
	},
	"vivaldi": {
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\vivaldi.exe`,
		`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\vivaldi.exe`,
	},
	"arc": {
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\Arc.exe`,
		`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\Arc.exe`,
	},
	"chromium": {
		`HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\chromium.exe`,
		`HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\chromium.exe`,
	},
}

// Extension policy registry paths (Chromium-based force install).
var extensionPolicyKeys = map[string]string{
	"chrome": `HKLM\SOFTWARE\Policies\Google\Chrome\ExtensionInstallForcelist`,
	"edge":   `HKLM\SOFTWARE\Policies\Microsoft\Edge\ExtensionInstallForcelist`,
	"brave":  `HKLM\SOFTWARE\Policies\BraveSoftware\Brave\ExtensionInstallForcelist`,
}

const (
	firefoxExtKey     = `HKLM\SOFTWARE\Mozilla\Firefox\Extensions`
	firefoxExtID      = "themisto-prompt-capture@themisto.local"
	extensionXPIDir   = `C:\ProgramData\Themisto\extensions\firefox\themisto.xpi`
	chromiumUpdateURL = "http://127.0.0.1:17175/extensions/chromium/updates.xml"
	chromiumExtName   = "Themisto Prompt Capture"
	chromiumExtHint   = "managed AI web apps"
	chromiumLocalHost = "127.0.0.1:17175"
	chromiumRepoPath  = "adapter/prompt/browser/chromium"
	// Chromium extension IDs are 32 lowercase a-p characters derived from
	// the extension's public key hash. This is a deterministic placeholder
	// following the required format; replace with the real ID once the
	// extension signing key is generated.
	extensionID = "bgijehgoebfjapkdoopgbmkfhganpmna"
)

// --- Richer browser protection model ---

// BrowserProtectionStatus is the authoritative per-browser status model.
type BrowserProtectionStatus struct {
	Name               string `json:"name"`
	Supported          bool   `json:"supported"`
	Installed          bool   `json:"installed"`
	DeploymentDetected bool   `json:"deployment_detected"`
	ProtectionStatus   string `json:"protection_status"` // deployed, policy_detected, not_deployed, check_failed, not_installed, unsupported
	Confidence         string `json:"confidence"`        // high, medium, low
	Detail             string `json:"detail"`
	Remediation        string `json:"remediation"`
	LastError          string `json:"last_error,omitempty"`
}

type browserDetectResult struct {
	installed bool
	err       error // only for genuine failures (exec error, permission denied), not missing keys
}

type extensionDetectResult struct {
	policyDetected   bool
	artifactDetected bool
	err              error // only for genuine failures, not missing policy keys
}

// detectBrowserProtection is the authoritative detection function.
func detectBrowserProtection() []BrowserProtectionStatus {
	var result []BrowserProtectionStatus
	for _, b := range browserCatalog {
		bp := BrowserProtectionStatus{
			Name:      b.name,
			Supported: b.supported,
		}

		br := detectBrowserInstalled(b.key)
		if br.err != nil {
			bp.ProtectionStatus = "check_failed"
			bp.Confidence = "low"
			bp.LastError = br.err.Error()
			bp.Detail, bp.Remediation = deriveExtensionGuidance(b.name, "check_failed", "low", bp.LastError)
			result = append(result, bp)
			continue
		}

		bp.Installed = br.installed
		if !bp.Installed {
			continue
		}

		if !bp.Supported {
			bp.ProtectionStatus = "unsupported"
			bp.Confidence = "high"
			bp.Detail, bp.Remediation = deriveExtensionGuidance(b.name, "unsupported", "high", "")
			result = append(result, bp)
			continue
		}

		er := detectExtensionDeployment(b.key)
		if er.err != nil {
			bp.ProtectionStatus = "check_failed"
			bp.Confidence = "low"
			bp.LastError = er.err.Error()
			bp.Detail, bp.Remediation = deriveExtensionGuidance(b.name, "check_failed", "low", bp.LastError)
			result = append(result, bp)
			continue
		}

		bp.DeploymentDetected = er.policyDetected || er.artifactDetected
		if er.artifactDetected {
			bp.ProtectionStatus = "deployed"
			bp.Confidence = "high"
		} else if er.policyDetected {
			bp.ProtectionStatus = "policy_detected"
			bp.Confidence = "medium"
		} else if runtime.GOOS != "windows" {
			bp.ProtectionStatus = "check_failed"
			bp.Confidence = "low"
			bp.LastError = "non-Windows extension rollout could not be confirmed from local browser profile metadata"
		} else {
			bp.ProtectionStatus = "not_deployed"
			bp.Confidence = "high"
		}
		bp.Detail, bp.Remediation = deriveExtensionGuidance(b.name, bp.ProtectionStatus, bp.Confidence, bp.LastError)
		result = append(result, bp)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Supported != result[j].Supported {
			return result[i].Supported
		}
		return result[i].Name < result[j].Name
	})
	return result
}

// detectBrowserInstalled checks registry for browser presence.
// Missing registry key is a normal negative (installed=false, err=nil).
// Only returns error for genuine exec/permission failures.
func detectBrowserInstalled(browser string) browserDetectResult {
	if runtime.GOOS != "windows" {
		return detectBrowserInstalledNonWindows(browser)
	}

	paths, ok := browserRegistry[browser]
	if !ok {
		return browserDetectResult{installed: false}
	}
	for _, regPath := range paths {
		out, err := newHiddenCommand("reg", "query", regPath).CombinedOutput()
		if err != nil {
			// "reg query" exits non-zero when key doesn't exist — that's a normal negative.
			// But if the command itself fails to execute, that's a genuine error.
			if isRegQueryNotFound(err, out) {
				continue
			}
			return browserDetectResult{err: fmt.Errorf("registry query for %s: %w", browser, err)}
		}
		if len(out) > 0 {
			return browserDetectResult{installed: true}
		}
	}

	for _, candidate := range windowsBrowserExecutablePaths(browser) {
		if candidate == "" {
			continue
		}
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return browserDetectResult{installed: true}
		}
	}
	return browserDetectResult{installed: false}
}

// detectExtensionDeployment checks registry for extension policy presence.
// Missing policy key is a normal negative (detected=false, err=nil).
// Only returns error for genuine exec/permission failures.
func detectExtensionDeployment(browser string) extensionDetectResult {
	if runtime.GOOS != "windows" {
		return detectExtensionDeploymentNonWindows(browser)
	}

	if browser == "firefox" {
		policyDetected := false
		out, err := newHiddenCommand("reg", "query", firefoxExtKey, "/v", firefoxExtID).CombinedOutput()
		if err != nil {
			if isRegQueryNotFound(err, out) {
				policyDetected = false
			} else {
				return extensionDetectResult{err: fmt.Errorf("registry query for firefox extension: %w", err)}
			}
		} else {
			policyDetected = strings.Contains(string(out), firefoxExtID)
		}

		artifactDetected := false
		if profiles := windowsFirefoxProfilesPath(); profiles != "" {
			profileResult := detectFirefoxExtensionFromProfiles(profiles)
			if profileResult.err != nil {
				return profileResult
			}
			artifactDetected = profileResult.artifactDetected
		}
		return extensionDetectResult{policyDetected: policyDetected, artifactDetected: artifactDetected}
	}

	policyKey, ok := extensionPolicyKeys[browser]
	if !ok {
		return extensionDetectResult{policyDetected: false, artifactDetected: false}
	}
	policyDetected := false
	out, err := newHiddenCommand("reg", "query", policyKey).CombinedOutput()
	if err != nil {
		if isRegQueryNotFound(err, out) {
			policyDetected = false
		} else {
			return extensionDetectResult{err: fmt.Errorf("registry query for %s extension policy: %w", browser, err)}
		}
	} else {
		policyDetected = strings.Contains(string(out), extensionID)
	}

	artifactDetected := false
	if profileRoot := windowsChromiumProfileRoot(browser); profileRoot != "" {
		profileResult := detectChromiumExtensionFromProfiles(profileRoot)
		if profileResult.err != nil {
			return profileResult
		}
		artifactDetected = profileResult.artifactDetected
	}
	return extensionDetectResult{policyDetected: policyDetected, artifactDetected: artifactDetected}
}

func detectBrowserInstalledNonWindows(browser string) browserDetectResult {
	home, _ := os.UserHomeDir()
	pathsByBrowser := map[string][]string{
		"chrome": {
			"/Applications/Google Chrome.app",
			filepath.Join(home, "Applications", "Google Chrome.app"),
		},
		"edge": {
			"/Applications/Microsoft Edge.app",
			filepath.Join(home, "Applications", "Microsoft Edge.app"),
		},
		"brave": {
			"/Applications/Brave Browser.app",
			filepath.Join(home, "Applications", "Brave Browser.app"),
		},
		"firefox": {
			"/Applications/Firefox.app",
			filepath.Join(home, "Applications", "Firefox.app"),
		},
	}

	paths, ok := pathsByBrowser[browser]
	if !ok {
		return browserDetectResult{installed: false}
	}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return browserDetectResult{installed: true}
		}
	}
	return browserDetectResult{installed: false}
}

func windowsBrowserExecutablePaths(browser string) []string {
	localAppData := os.Getenv("LocalAppData")
	programFiles := os.Getenv("ProgramFiles")
	programFilesX86 := os.Getenv("ProgramFiles(x86)")

	switch browser {
	case "chrome":
		return []string{
			filepath.Join(programFiles, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(programFilesX86, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(localAppData, "Google", "Chrome", "Application", "chrome.exe"),
		}
	case "edge":
		return []string{
			filepath.Join(programFiles, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(programFilesX86, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(localAppData, "Microsoft", "Edge", "Application", "msedge.exe"),
		}
	case "brave":
		return []string{
			filepath.Join(programFiles, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
			filepath.Join(programFilesX86, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
			filepath.Join(localAppData, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
		}
	case "firefox":
		return []string{
			filepath.Join(programFiles, "Mozilla Firefox", "firefox.exe"),
			filepath.Join(programFilesX86, "Mozilla Firefox", "firefox.exe"),
		}
	case "opera":
		return []string{
			filepath.Join(localAppData, "Programs", "Opera", "opera.exe"),
			filepath.Join(programFiles, "Opera", "launcher.exe"),
			filepath.Join(programFilesX86, "Opera", "launcher.exe"),
		}
	case "opera_gx":
		return []string{
			filepath.Join(localAppData, "Programs", "Opera GX", "opera.exe"),
		}
	case "vivaldi":
		return []string{
			filepath.Join(localAppData, "Vivaldi", "Application", "vivaldi.exe"),
			filepath.Join(programFiles, "Vivaldi", "Application", "vivaldi.exe"),
			filepath.Join(programFilesX86, "Vivaldi", "Application", "vivaldi.exe"),
		}
	case "arc":
		return []string{
			filepath.Join(localAppData, "Programs", "Arc", "Arc.exe"),
		}
	case "chromium":
		return []string{
			filepath.Join(programFiles, "Chromium", "Application", "chrome.exe"),
			filepath.Join(programFilesX86, "Chromium", "Application", "chrome.exe"),
			filepath.Join(localAppData, "Chromium", "Application", "chrome.exe"),
		}
	default:
		return nil
	}
}

func windowsChromiumProfileRoot(browser string) string {
	localAppData := os.Getenv("LocalAppData")
	switch browser {
	case "chrome":
		return filepath.Join(localAppData, "Google", "Chrome", "User Data")
	case "edge":
		return filepath.Join(localAppData, "Microsoft", "Edge", "User Data")
	case "brave":
		return filepath.Join(localAppData, "BraveSoftware", "Brave-Browser", "User Data")
	case "vivaldi":
		return filepath.Join(localAppData, "Vivaldi", "User Data")
	case "opera":
		return filepath.Join(localAppData, "Programs", "Opera")
	case "opera_gx":
		return filepath.Join(localAppData, "Programs", "Opera GX")
	case "chromium":
		return filepath.Join(localAppData, "Chromium", "User Data")
	default:
		return ""
	}
}

func windowsFirefoxProfilesPath() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return ""
	}
	return filepath.Join(appData, "Mozilla", "Firefox", "Profiles")
}

func detectExtensionDeploymentNonWindows(browser string) extensionDetectResult {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return extensionDetectResult{policyDetected: false, artifactDetected: false}
	}

	switch browser {
	case "chrome":
		return detectChromiumExtensionFromProfiles(filepath.Join(home, "Library", "Application Support", "Google", "Chrome"))
	case "edge":
		return detectChromiumExtensionFromProfiles(filepath.Join(home, "Library", "Application Support", "Microsoft Edge"))
	case "brave":
		return detectChromiumExtensionFromProfiles(filepath.Join(home, "Library", "Application Support", "BraveSoftware", "Brave-Browser"))
	case "firefox":
		return detectFirefoxExtensionFromProfiles(filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles"))
	default:
		return extensionDetectResult{policyDetected: false, artifactDetected: false}
	}
}

func detectChromiumExtensionFromProfiles(base string) extensionDetectResult {
	manifestPattern := filepath.Join(base, "*", "Extensions", "*", "*", "manifest.json")
	manifests, err := filepath.Glob(manifestPattern)
	if err != nil {
		return extensionDetectResult{err: fmt.Errorf("chromium manifest glob: %w", err)}
	}
	for _, path := range manifests {
		match, err := fileContainsAny(path,
			chromiumExtName,
			chromiumExtHint,
			"themisto-prompt-banner",
			"extensions/chromium/updates.xml",
		)
		if err != nil {
			return extensionDetectResult{err: fmt.Errorf("read chromium manifest %s: %w", path, err)}
		}
		if match {
			return extensionDetectResult{artifactDetected: true}
		}
	}

	// Chrome/Edge/Brave can also hold unpacked or external extension metadata
	// in profile preferences even when no packaged extension directory exists.
	preferencePatterns := []string{
		filepath.Join(base, "*", "Preferences"),
		filepath.Join(base, "*", "Secure Preferences"),
	}
	for _, pattern := range preferencePatterns {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			return extensionDetectResult{err: fmt.Errorf("chromium preferences glob: %w", err)}
		}
		for _, path := range paths {
			match, err := fileContainsAny(path,
				chromiumExtName,
				chromiumExtHint,
				chromiumLocalHost,
				chromiumRepoPath,
				"themisto-prompt-banner",
				"extensions/chromium/updates.xml",
				extensionID,
			)
			if err != nil {
				return extensionDetectResult{err: fmt.Errorf("read chromium profile metadata %s: %w", path, err)}
			}
			if match {
				return extensionDetectResult{artifactDetected: true}
			}
		}
	}

	return extensionDetectResult{artifactDetected: false}
}

func detectFirefoxExtensionFromProfiles(base string) extensionDetectResult {
	patterns := []string{
		filepath.Join(base, "*", "extensions", firefoxExtID+".xpi"),
		filepath.Join(base, "*", "extensions", firefoxExtID),
		filepath.Join(base, "*", "extensions.json"),
	}
	for _, pattern := range patterns {
		matches, globErr := filepath.Glob(pattern)
		if globErr != nil {
			return extensionDetectResult{err: fmt.Errorf("firefox extension glob: %w", globErr)}
		}
		for _, path := range matches {
			if strings.HasSuffix(path, ".xpi") || strings.HasSuffix(path, firefoxExtID) {
				return extensionDetectResult{artifactDetected: true}
			}
			match, err := fileContainsAny(path, firefoxExtID, chromiumExtName, chromiumExtHint)
			if err != nil {
				return extensionDetectResult{err: fmt.Errorf("read firefox profile metadata %s: %w", path, err)}
			}
			if match {
				return extensionDetectResult{artifactDetected: true}
			}
		}
	}
	return extensionDetectResult{artifactDetected: false}
}

func fileContainsAny(path string, needles ...string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	content := string(data)
	for _, needle := range needles {
		if needle != "" && strings.Contains(content, needle) {
			return true, nil
		}
	}
	return false, nil
}

func dirHasEntries(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) > 0
}

// isRegQueryNotFound returns true if the reg query error is just "key not found",
// which is a normal negative result, not a genuine failure.
func isRegQueryNotFound(err error, output []byte) bool {
	if err == nil {
		return false
	}
	// reg.exe exits with code 1 and prints "ERROR: The system was unable to find
	// the specified registry key or value." when the key doesn't exist.
	// An exec.ExitError with output containing this pattern is a normal negative.
	out := strings.ToLower(string(output))
	if strings.Contains(out, "unable to find") || strings.Contains(out, "not find") {
		return true
	}
	return false
}

// deriveExtensionGuidance returns (detail, remediation) for the given status.
func deriveExtensionGuidance(name, status, confidence, lastError string) (string, string) {
	switch status {
	case "deployed":
		return fmt.Sprintf("%s is installed and Themisto found the extension in local browser data. Protection should be available once the browser session is using the managed profile.", name),
			"No action needed. If issues persist, reload the browser or contact IT."
	case "policy_detected":
		return fmt.Sprintf("%s is installed and Themisto found rollout policy, but it has not yet confirmed the extension inside the browser profile.", name),
			"Open or fully restart the browser so it can apply managed extension policy, then refresh this page."
	case "not_deployed":
		return fmt.Sprintf("%s is installed, but the Themisto extension has not been deployed.", name),
			fmt.Sprintf("If you plan to use %s with Themisto protection, contact IT to deploy the browser extension.", name)
	case "check_failed":
		if strings.Contains(lastError, "non-Windows extension rollout could not be confirmed") {
			return fmt.Sprintf("Themisto detected %s, but this macOS build could not confirm extension rollout from local browser metadata.", name),
				"Open the browser's extensions page to confirm the Themisto extension is installed and enabled."
		}
		detail := fmt.Sprintf("Themisto could not read browser deployment state for %s.", name)
		if lastError != "" {
			detail += " (" + lastError + ")"
		}
		return detail, "Try refreshing. If this continues, share diagnostics with IT."
	case "not_installed":
		return "This browser is not installed on this device.",
			"No action is needed unless you plan to use it."
	case "unsupported":
		return fmt.Sprintf("%s is installed on this device, but Themisto does not currently manage or verify this browser.", name),
			"Use a supported browser like Chrome, Edge, Brave, or Firefox for Themisto extension coverage."
	default:
		return fmt.Sprintf("Unknown extension status for %s.", name), "Contact IT for assistance."
	}
}

// --- Legacy interface ---

// detectBrowsers returns the legacy BrowserInfo slice, delegating to detectBrowserProtection.
func detectBrowsers() []BrowserInfo {
	statuses := detectBrowserProtection()
	var result []BrowserInfo
	for _, s := range statuses {
		result = append(result, BrowserInfo{
			Name:      s.Name,
			Installed: s.Installed,
			Extension: s.DeploymentDetected,
		})
	}
	return result
}

// Legacy boolean helpers kept for backward compatibility with install/uninstall.
func isBrowserInstalled(browser string) bool {
	return detectBrowserInstalled(browser).installed
}

func isExtensionInstalled(browser string) bool {
	er := detectExtensionDeployment(browser)
	return er.policyDetected || er.artifactDetected
}

// --- Install / uninstall (unchanged, admin-gated) ---

func installExtension(browser string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("extension install from desktop is only supported on Windows in this build")
	}
	if browser == "firefox" {
		return installFirefoxExtension()
	}
	return installChromiumExtension(browser)
}

func uninstallExtension(browser string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("extension uninstall from desktop is only supported on Windows in this build")
	}
	if browser == "firefox" {
		return uninstallFirefoxExtension()
	}
	return uninstallChromiumExtension(browser)
}

func installChromiumExtension(browser string) error {
	policyKey, ok := extensionPolicyKeys[browser]
	if !ok {
		return fmt.Errorf("unsupported browser: %s", browser)
	}

	value := extensionID + ";" + chromiumUpdateURL

	out, err := newHiddenCommand("reg", "add", policyKey,
		"/v", "1",
		"/t", "REG_SZ",
		"/d", value,
		"/f",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("install extension for %s: %s: %w", browser, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func uninstallChromiumExtension(browser string) error {
	policyKey, ok := extensionPolicyKeys[browser]
	if !ok {
		return fmt.Errorf("unsupported browser: %s", browser)
	}

	out, err := newHiddenCommand("reg", "query", policyKey).CombinedOutput()
	if err != nil {
		return nil // key doesn't exist, nothing to remove
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, extensionID) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		valueName := fields[0]
		_ = newHiddenCommand("reg", "delete", policyKey, "/v", valueName, "/f").Run()
	}
	return nil
}

func installFirefoxExtension() error {
	out, err := newHiddenCommand("reg", "add", firefoxExtKey,
		"/v", firefoxExtID,
		"/t", "REG_SZ",
		"/d", extensionXPIDir,
		"/f",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("install firefox extension: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func uninstallFirefoxExtension() error {
	_ = newHiddenCommand("reg", "delete", firefoxExtKey, "/v", firefoxExtID, "/f").Run()
	return nil
}
