package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ClaudeCodeIntegrationStatus is the authoritative status for Claude Code hook integration.
type ClaudeCodeIntegrationStatus struct {
	Target               string `json:"target,omitempty"`
	DisplayName          string `json:"display_name,omitempty"`
	EnvironmentAvailable bool   `json:"environment_available"`
	ActionsSupported     bool   `json:"actions_supported"`
	SettingsPath         string `json:"settings_path,omitempty"`
	ManagedSettingsPath  string `json:"managed_settings_path,omitempty"`
	CLIInstalled         bool   `json:"cli_installed"`
	CLIPath              string `json:"cli_path,omitempty"`
	HookConfigured       bool   `json:"hook_configured"`
	HookScriptExists     bool   `json:"hook_script_exists"`
	HookSource           string `json:"hook_source,omitempty"`
	IntegrationState     string `json:"integration_state"`
	Confidence           string `json:"confidence"`
	Detail               string `json:"detail"`
	Remediation          string `json:"remediation"`
	LastError            string `json:"last_error,omitempty"`
	HooksDisabled        bool   `json:"hooks_disabled,omitempty"`
	ManagedOverride      bool   `json:"managed_override,omitempty"`
}

// ClaudeCodeIntegrationOverview reports Claude Code coverage across supported
// execution environments on the device, such as Windows and WSL.
type ClaudeCodeIntegrationOverview struct {
	Targets         []ClaudeCodeIntegrationStatus `json:"targets"`
	PreferredTarget string                        `json:"preferred_target,omitempty"`
}

// ActionResult is a generic success/failure result for Wails-exposed actions.
type ActionResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

var (
	claudeCodeOverviewMu       sync.Mutex
	claudeCodeOverviewCached   ClaudeCodeIntegrationOverview
	claudeCodeOverviewCachedAt time.Time
)

// hookScriptMarker is a substring present in Themisto hook commands that lets us
// identify our own entries when reading back settings.json.
var claudeCodeHookMarkers = []string{"claude-code-hook.ps1", "claude-code-hook.sh"}

const claudeCodeHookAgentFlag = "-claude-code-hook"

// claudeCodeUserSettingsPath returns the per-user Claude Code settings.json path.
func claudeCodeUserSettingsPath() string {
	return filepath.Join(currentUserHomeDir(), ".claude", "settings.json")
}

// claudeCodeManagedSettingsPath returns the system-wide managed settings path.
func claudeCodeManagedSettingsPath() string {
	switch runtime.GOOS {
	case "windows":
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "ClaudeCode", "managed-settings.json")
	case "darwin":
		return "/Library/Application Support/ClaudeCode/managed-settings.json"
	default:
		return "/etc/claude-code/managed-settings.json"
	}
}

// claudeCodeHookScriptPath returns the per-user hook script location.
func claudeCodeHookScriptPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(themistoHookDir(), "claude-code-hook.ps1")
	}
	return filepath.Join(themistoHookDir(), "claude-code-hook.sh")
}

// --- CLI detection ---

// detectClaudeCodeCLI checks common locations for the Claude Code CLI.
func detectClaudeCodeCLI() (installed bool, cliPath string) {
	if runtime.GOOS != "windows" {
		if p, err := exec.LookPath("claude"); err == nil {
			return true, p
		}
		candidates := []string{
			"/opt/homebrew/bin/claude",
			"/usr/local/bin/claude",
			filepath.Join(currentUserHomeDir(), ".local", "bin", "claude"),
			filepath.Join(currentUserHomeDir(), "bin", "claude"),
		}
		for _, candidate := range candidates {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return true, candidate
			}
		}
		return false, ""
	}

	// Windows: check where.exe first (finds anything in PATH).
	if out, err := newHiddenCommand("where.exe", "claude").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				return true, line
			}
		}
	}

	// Common install locations on Windows.
	candidates := []string{}
	if lad := os.Getenv("LOCALAPPDATA"); lad != "" {
		candidates = append(candidates, filepath.Join(lad, "Programs", "claude-code", "claude.exe"))
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		candidates = append(candidates, filepath.Join(appData, "npm", "claude.cmd"))
	}
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		candidates = append(candidates, filepath.Join(pf, "ClaudeCode", "claude.exe"))
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return true, c
		}
	}
	return false, ""
}

// --- Hook status reading ---

// readClaudeCodeHookStatus reads settings.json and checks if a Themisto hook is configured.
func readClaudeCodeHookStatus(settingsPath string) (hookPresent bool, err error) {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read Claude Code settings: %w", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return false, fmt.Errorf("parse Claude Code settings: %w", err)
	}

	return settingsContainThemistoHook(settings), nil
}

// settingsContainThemistoHook walks the hooks.UserPromptSubmit array looking for
// entries whose command contains our hook script marker.
func settingsContainThemistoHook(settings map[string]interface{}) bool {
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		return false
	}
	upsEntries, ok := hooks["UserPromptSubmit"].([]interface{})
	if !ok {
		return false
	}
	for _, entry := range upsEntries {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		innerHooks, ok := entryMap["hooks"].([]interface{})
		if !ok {
			continue
		}
		for _, h := range innerHooks {
			hMap, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			cmd, _ := hMap["command"].(string)
			for _, marker := range claudeCodeHookMarkers {
				if strings.Contains(cmd, marker) {
					return true
				}
			}
		}
	}
	return false
}

// --- Disabled/override detection ---

type hookOverrideInfo struct {
	HooksDisabled   bool
	ManagedOverride bool
}

// checkHooksDisabledOrOverridden detects whether hooks are disabled globally or
// user hooks are blocked by managed settings.
func checkHooksDisabledOrOverridden(userSettingsPath, managedSettingsPath string) hookOverrideInfo {
	info := hookOverrideInfo{}

	// Check managed settings (read-only, may not exist).
	if data, err := os.ReadFile(managedSettingsPath); err == nil {
		var managed map[string]interface{}
		if json.Unmarshal(data, &managed) == nil {
			if v, ok := managed["disableAllHooks"].(bool); ok && v {
				info.HooksDisabled = true
			}
			if v, ok := managed["allowManagedHooksOnly"].(bool); ok && v {
				info.ManagedOverride = true
			}
		}
	}

	// Check user settings for disableAllHooks.
	if !info.HooksDisabled {
		if data, err := os.ReadFile(userSettingsPath); err == nil {
			var user map[string]interface{}
			if json.Unmarshal(data, &user) == nil {
				if v, ok := user["disableAllHooks"].(bool); ok && v {
					info.HooksDisabled = true
				}
			}
		}
	}

	return info
}

// --- Integration detection (authoritative) ---

func detectClaudeCodeIntegration() ClaudeCodeIntegrationStatus {
	overview := detectClaudeCodeIntegrationOverview()
	return choosePreferredClaudeCodeStatus(overview.Targets)
}

func detectClaudeCodeIntegrationOverview() ClaudeCodeIntegrationOverview {
	overview := detectClaudeCodeIntegrationOverviewFresh()
	claudeCodeOverviewMu.Lock()
	claudeCodeOverviewCached = overview
	claudeCodeOverviewCachedAt = time.Now()
	claudeCodeOverviewMu.Unlock()
	return overview
}

func detectClaudeCodeIntegrationOverviewFresh() ClaudeCodeIntegrationOverview {
	if runtime.GOOS != "windows" {
		status := detectClaudeCodeIntegrationAt(
			claudeCodeUserSettingsPath(),
			claudeCodeManagedSettingsPath(),
			claudeCodeHookScriptPath(),
		)
		status.Target = runtime.GOOS
		status.DisplayName = platformDisplayName(runtime.GOOS)
		status.EnvironmentAvailable = true
		status.ActionsSupported = claudeCodeActionsSupported()
		status.SettingsPath = claudeCodeUserSettingsPath()
		status.ManagedSettingsPath = claudeCodeManagedSettingsPath()
		return ClaudeCodeIntegrationOverview{
			Targets:         []ClaudeCodeIntegrationStatus{status},
			PreferredTarget: status.Target,
		}
	}

	targets := []ClaudeCodeIntegrationStatus{detectWindowsClaudeCodeIntegration()}
	if wslStatus := detectWSLClaudeCodeIntegration(); shouldExposeClaudeCodeTarget(wslStatus) {
		targets = append(targets, wslStatus)
	}
	preferred := choosePreferredClaudeCodeStatus(targets)
	return ClaudeCodeIntegrationOverview{
		Targets:         targets,
		PreferredTarget: preferred.Target,
	}
}

func cachedClaudeCodeIntegrationForDiagnostics() ClaudeCodeIntegrationStatus {
	claudeCodeOverviewMu.Lock()
	defer claudeCodeOverviewMu.Unlock()

	if len(claudeCodeOverviewCached.Targets) == 0 {
		return ClaudeCodeIntegrationStatus{
			IntegrationState: "not_checked",
			Confidence:       "low",
			ActionsSupported: claudeCodeActionsSupported(),
			Detail:           "Claude Code integration has not been checked in this session. Open Extensions to refresh native tool status.",
			Remediation:      "Open the Extensions page to collect Claude Code integration status on demand.",
		}
	}
	return choosePreferredClaudeCodeStatus(claudeCodeOverviewCached.Targets)
}

func detectWindowsClaudeCodeIntegration() ClaudeCodeIntegrationStatus {
	status := detectClaudeCodeIntegrationAt(
		claudeCodeUserSettingsPath(),
		claudeCodeManagedSettingsPath(),
		claudeCodeHookScriptPath(),
	)
	status.Target = "windows"
	status.DisplayName = "Windows"
	status.EnvironmentAvailable = runtime.GOOS == "windows"
	status.ActionsSupported = true
	status.SettingsPath = claudeCodeUserSettingsPath()
	status.ManagedSettingsPath = claudeCodeManagedSettingsPath()
	return status
}

func shouldExposeClaudeCodeTarget(status ClaudeCodeIntegrationStatus) bool {
	return status.EnvironmentAvailable || status.CLIInstalled || status.HookConfigured || status.LastError != ""
}

func choosePreferredClaudeCodeStatus(targets []ClaudeCodeIntegrationStatus) ClaudeCodeIntegrationStatus {
	if len(targets) == 0 {
		return ClaudeCodeIntegrationStatus{}
	}
	best := targets[0]
	bestScore := claudeCodeStatusScore(best)
	for _, candidate := range targets[1:] {
		score := claudeCodeStatusScore(candidate)
		if score > bestScore {
			best = candidate
			bestScore = score
		}
	}
	return best
}

func claudeCodeStatusScore(status ClaudeCodeIntegrationStatus) int {
	score := 0
	if status.HookConfigured && status.HookScriptExists && status.IntegrationState == "configured" {
		score += 100
	}
	if status.CLIInstalled {
		score += 50
	}
	if status.EnvironmentAvailable {
		score += 10
	}
	return score
}

func defaultClaudeCodeTarget() string {
	overview := detectClaudeCodeIntegrationOverview()
	if overview.PreferredTarget != "" {
		return overview.PreferredTarget
	}
	return "windows"
}

func installClaudeCodeHookForTarget(target string) error {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "wsl":
		return installWSLClaudeCodeHook()
	default:
		if !claudeCodeActionsSupported() {
			return fmt.Errorf("Claude Code hook installation is not wired on %s yet. Themisto now reports the correct local settings and hook paths so macOS/Linux support can be built without Windows-specific assumptions", platformDisplayName(runtime.GOOS))
		}
		return installClaudeCodeHook()
	}
}

func repairClaudeCodeHookForTarget(target string) error {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "wsl":
		return repairWSLClaudeCodeHook()
	default:
		if !claudeCodeActionsSupported() {
			return fmt.Errorf("Claude Code hook repair is not wired on %s yet. Themisto now reports the correct local settings and hook paths so macOS/Linux support can be built without Windows-specific assumptions", platformDisplayName(runtime.GOOS))
		}
		return repairClaudeCodeHook()
	}
}

func removeClaudeCodeHookForTarget(target string) error {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "wsl":
		return removeWSLClaudeCodeHook()
	default:
		return removeClaudeCodeHook()
	}
}

func detectClaudeCodeIntegrationAt(userSettingsPath, managedSettingsPath, scriptPath string) ClaudeCodeIntegrationStatus {
	status := ClaudeCodeIntegrationStatus{
		Confidence:       "high",
		ActionsSupported: claudeCodeActionsSupported(),
	}

	// Step 1: detect CLI
	cliInstalled, cliPath := detectClaudeCodeCLI()
	status.CLIInstalled = cliInstalled
	status.CLIPath = cliPath

	if !cliInstalled {
		// Also check if the .claude directory exists (CLI may be installed but
		// not in PATH — e.g. using npx or VS Code extension).
		settingsDir := filepath.Dir(userSettingsPath)
		if _, err := os.Stat(settingsDir); err != nil {
			status.IntegrationState = "not_installed"
			status.Detail = "Claude Code CLI was not detected on this machine."
			status.Remediation = "Install Claude Code to enable prompt governance integration."
			return status
		}
		// The .claude directory exists even though the CLI wasn't found in PATH.
		// Proceed with detection — they may be using it via npx or IDE plugin.
		status.Confidence = "medium"
	}

	// Step 2: check hook presence in user settings.
	userHookPresent, err := readClaudeCodeHookStatus(userSettingsPath)
	if err != nil {
		status.IntegrationState = "check_failed"
		status.Detail = "Could not read Claude Code settings to check hook configuration."
		status.Remediation = "Verify that " + userSettingsPath + " is readable."
		status.LastError = err.Error()
		return status
	}

	// Step 3: check managed settings too. Enterprise deployments may place the
	// hook there, so user settings alone are not authoritative.
	managedHookPresent, err := readClaudeCodeHookStatus(managedSettingsPath)
	if err != nil {
		status.IntegrationState = "check_failed"
		status.Detail = "Could not read Claude Code managed settings to check hook configuration."
		status.Remediation = "Verify that " + managedSettingsPath + " is readable."
		status.LastError = err.Error()
		return status
	}

	switch {
	case managedHookPresent && userHookPresent:
		status.HookSource = "user_and_managed"
	case managedHookPresent:
		status.HookSource = "managed"
	case userHookPresent:
		status.HookSource = "user"
	}

	hookPresent := userHookPresent || managedHookPresent
	status.HookConfigured = hookPresent

	// Step 4: check script
	scriptSupportDetail := ""
	if _, scriptErr := os.Stat(scriptPath); scriptErr == nil {
		status.HookScriptExists = true
		if supported, detail := validateClaudeCodeHookScript(scriptPath); !supported {
			status.HookScriptExists = false
			scriptSupportDetail = detail
		}
	}

	// Step 5: check overrides
	overrides := checkHooksDisabledOrOverridden(userSettingsPath, managedSettingsPath)
	status.HooksDisabled = overrides.HooksDisabled
	status.ManagedOverride = overrides.ManagedOverride

	// Step 6: derive state
	switch {
	case hookPresent && overrides.HooksDisabled:
		status.IntegrationState = "hook_present_but_disabled"
		status.Detail = "The Themisto hook is present, but Claude Code hooks are globally disabled (disableAllHooks)."
		status.Remediation = "Remove the disableAllHooks setting from Claude Code user or managed settings to allow the hook to run."
	case userHookPresent && overrides.ManagedOverride && !managedHookPresent:
		status.IntegrationState = "hook_present_but_disabled"
		status.Detail = "The Themisto hook is configured in user settings, but managed settings only allow managed hooks."
		status.Remediation = "Contact IT to deploy the Themisto hook through Claude Code managed settings, or remove the managed-only restriction."
	case hookPresent && status.HookScriptExists:
		status.IntegrationState = "configured"
		switch status.HookSource {
		case "managed":
			status.Detail = "Themisto found a Claude Code hook in managed settings and the local hook script exists."
		case "user_and_managed":
			status.Detail = "Themisto found Claude Code hooks in both user and managed settings, and the local hook script exists."
		default:
			status.Detail = "Themisto found a Claude Code hook in user settings and the local hook script exists."
		}
		status.Remediation = "Claude Code snapshots hooks at startup. If you recently installed or changed the hook, restart Claude Code or review the change from /hooks before expecting it to run."
	case hookPresent && !status.HookScriptExists:
		status.IntegrationState = "missing_script"
		if scriptSupportDetail != "" {
			status.Detail = scriptSupportDetail
			status.Remediation = "Upgrade or reinstall the local Themisto agent so the Claude Code hook runner is available, then use Repair to rewrite the hook script."
		} else {
			status.Detail = "The Claude Code hook entry is configured, but the hook script is missing from disk."
			status.Remediation = "Use Repair to reinstall the hook script."
		}
	case !hookPresent && status.HookScriptExists:
		status.IntegrationState = "partial"
		status.Detail = "The Themisto hook script exists on disk, but Claude Code settings do not reference it."
		status.Remediation = "Use Install to configure the hook in Claude Code."
	default:
		status.IntegrationState = "missing_hook"
		status.Confidence = "medium"
		status.Detail = "Themisto did not find a Claude Code hook in user or managed settings. Project-level Claude Code settings are not included in this device-wide check."
		if status.ActionsSupported {
			status.Remediation = "Click Install to add the Themisto prompt governance hook at the user level, or confirm whether your team is using project-level Claude Code settings instead."
		} else {
			status.Remediation = "Themisto is now using the correct local Claude Code paths for this platform. Hook installation still needs a native shell implementation before the desktop app can wire it automatically."
		}
	}

	return status
}

func claudeCodeActionsSupported() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "darwin"
}

func validateClaudeCodeHookScript(scriptPath string) (bool, string) {
	if isPowerShellScriptPath(scriptPath) {
		return true, ""
	}
	supported, detail := agentBinarySupportsHookFlag(claudeCodeHookAgentFlag)
	if supported {
		return true, ""
	}
	detail = strings.TrimSpace(detail)
	if detail != "" {
		detail = ": " + detail
	}
	return false, fmt.Sprintf("The Themisto Claude Code hook script exists, but the installed Themisto agent at %s does not support Claude Code hook execution yet%s", agentBinaryPath, detail)
}

// --- Hook installation ---

func installClaudeCodeHook() error {
	return installClaudeCodeHookAt(claudeCodeUserSettingsPath(), claudeCodeHookScriptPath())
}

func installClaudeCodeHookAt(settingsPath, scriptPath string) error {
	// Ensure script is on disk first.
	if err := ensureHookScript(scriptPath); err != nil {
		return fmt.Errorf("write hook script: %w", err)
	}

	// Ensure the .claude directory exists.
	settingsDir := filepath.Dir(settingsPath)
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}

	// Read existing settings (or start fresh).
	settings := make(map[string]interface{})
	if data, err := os.ReadFile(settingsPath); err == nil {
		if jsonErr := json.Unmarshal(data, &settings); jsonErr != nil {
			return fmt.Errorf("existing Claude Code settings.json is invalid JSON: %w", jsonErr)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read Claude Code settings: %w", err)
	}

	upsertThemistoHook(settings, buildClaudeCodeHookCommand(scriptPath))

	// Write atomically.
	return writeJSONAtomic(settingsPath, settings)
}

func buildClaudeCodeHookCommand(scriptPath string) string {
	if runtime.GOOS != "windows" {
		return scriptPath
	}
	psPath := resolveAbsolutePowerShellPath()
	return fmt.Sprintf(`%s -ExecutionPolicy Bypass -NoProfile -File "%s"`, psPath, scriptPath)
}

// resolveAbsolutePowerShellPath returns the absolute path to powershell.exe.
func resolveAbsolutePowerShellPath() string {
	sysRoot := os.Getenv("SystemRoot")
	if sysRoot == "" {
		sysRoot = `C:\Windows`
	}
	return filepath.Join(sysRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
}

// --- Hook removal ---

func removeClaudeCodeHook() error {
	if err := removeClaudeCodeHookAt(claudeCodeUserSettingsPath()); err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		_ = os.Remove(claudeCodeHookScriptPath())
	}
	return nil
}

func removeClaudeCodeHookAt(settingsPath string) error {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to remove
		}
		return fmt.Errorf("read Claude Code settings: %w", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("parse Claude Code settings: %w", err)
	}

	if !removeThemistoHook(settings) {
		return nil // no hooks section
	}

	return writeJSONAtomic(settingsPath, settings)
}

// --- Hook repair ---

func repairClaudeCodeHook() error {
	settingsPath := claudeCodeUserSettingsPath()
	scriptPath := claudeCodeHookScriptPath()

	if err := removeClaudeCodeHookAt(settingsPath); err != nil {
		return fmt.Errorf("remove during repair: %w", err)
	}
	return installClaudeCodeHookAt(settingsPath, scriptPath)
}

// --- Hook script management ---

func ensureHookScript(scriptPath string) error {
	dir := filepath.Dir(scriptPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create hook directory %s: %w", dir, err)
	}
	content := claudeCodeHookPS1
	mode := os.FileMode(0644)
	if runtime.GOOS != "windows" {
		content = posixHookScript(claudeCodeHookAgentFlag, nil)
		mode = 0755
	}
	return os.WriteFile(scriptPath, []byte(content), mode)
}

// entryContainsThemistoHook checks if a UserPromptSubmit entry has the Themisto hook script.
func entryContainsThemistoHook(entry interface{}) bool {
	entryMap, ok := entry.(map[string]interface{})
	if !ok {
		return false
	}
	innerHooks, ok := entryMap["hooks"].([]interface{})
	if !ok {
		return false
	}
	for _, h := range innerHooks {
		hMap, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		cmd, _ := hMap["command"].(string)
		for _, marker := range claudeCodeHookMarkers {
			if strings.Contains(cmd, marker) {
				return true
			}
		}
	}
	return false
}

func upsertThemistoHook(settings map[string]interface{}, command string) {
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		hooks = make(map[string]interface{})
		settings["hooks"] = hooks
	}

	upsEntries, _ := hooks["UserPromptSubmit"].([]interface{})
	cleaned := make([]interface{}, 0, len(upsEntries))
	for _, entry := range upsEntries {
		if !entryContainsThemistoHook(entry) {
			cleaned = append(cleaned, entry)
		}
	}

	themistoEntry := map[string]interface{}{
		"matcher": "",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": command,
				"timeout": 30,
			},
		},
	}

	cleaned = append(cleaned, themistoEntry)
	hooks["UserPromptSubmit"] = cleaned
}

func removeThemistoHook(settings map[string]interface{}) bool {
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		return false
	}

	upsEntries, ok := hooks["UserPromptSubmit"].([]interface{})
	if !ok {
		return false
	}

	cleaned := make([]interface{}, 0, len(upsEntries))
	for _, entry := range upsEntries {
		if !entryContainsThemistoHook(entry) {
			cleaned = append(cleaned, entry)
		}
	}

	if len(cleaned) == 0 {
		delete(hooks, "UserPromptSubmit")
	} else {
		hooks["UserPromptSubmit"] = cleaned
	}

	if len(hooks) == 0 {
		delete(settings, "hooks")
	}
	return true
}

// writeJSONAtomic writes a JSON object to a file atomically via a temp file + rename.
func writeJSONAtomic(path string, data interface{}) error {
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	out = append(out, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename temp to settings: %w", err)
	}
	return nil
}

// --- Embedded PowerShell hook script ---

const claudeCodeHookPS1 = `# Themisto Claude Code Hook - UserPromptSubmit
# Evaluates prompts via the local Themisto agent before Claude Code processes them.
# Exit 0 = allow, Exit 2 = block (stderr shown to user).
# Logs to %LOCALAPPDATA%\Themisto\logs\claude-code-hook.log for diagnostics.

# -- Logging -------------------------------------------------------------------
$script:LogPath = $null
function Initialize-HookLog {
    try {
        $logDir = Join-Path $env:LOCALAPPDATA 'Themisto\logs'
        if (-not (Test-Path $logDir)) { New-Item -ItemType Directory -Path $logDir -Force | Out-Null }
        $script:LogPath = Join-Path $logDir 'claude-code-hook.log'
        # Rotate: truncate if over 1 MB.
        if ((Test-Path $script:LogPath) -and (Get-Item $script:LogPath).Length -gt 1048576) {
            $tail = Get-Content $script:LogPath -Tail 200
            $keep = $tail -join [Environment]::NewLine
            [IO.File]::WriteAllText($script:LogPath, $keep)
        }
    } catch { $script:LogPath = $null }
}
function Write-HookLog($msg) {
    if (-not $script:LogPath) { return }
    try {
        $ts = (Get-Date).ToString('yyyy-MM-dd HH:mm:ss.fff')
        $line = "$ts  $msg" + [Environment]::NewLine
        [IO.File]::AppendAllText($script:LogPath, $line)
    } catch { }
}

# -- HTTP helper ---------------------------------------------------------------
function Invoke-ThemistoAPI($uri, $jsonBody, $timeoutMs) {
    $bodyBytes = [System.Text.Encoding]::UTF8.GetBytes($jsonBody)
    $request = [System.Net.HttpWebRequest][System.Net.WebRequest]::Create($uri)
    $request.Method = 'POST'
    $request.ContentType = 'application/json; charset=utf-8'
    $request.Timeout = $timeoutMs
    $request.ReadWriteTimeout = $timeoutMs
    $request.Proxy = $null
    $request.ContentLength = $bodyBytes.Length

    try {
        $requestStream = $request.GetRequestStream()
        $requestStream.Write($bodyBytes, 0, $bodyBytes.Length)
        $requestStream.Close()

        $response = [System.Net.HttpWebResponse]$request.GetResponse()
        try {
            $reader = New-Object System.IO.StreamReader($response.GetResponseStream(), [System.Text.Encoding]::UTF8)
            $resp = $reader.ReadToEnd()
            $reader.Close()
        } finally {
            $response.Close()
        }
        return @{ Success = $true; Body = $resp; Error = $null; StatusCode = 200 }
    } catch [System.Net.WebException] {
        $errBody = ''
        $statusCode = 0
        if ($_.Exception.Response) {
            $response = [System.Net.HttpWebResponse]$_.Exception.Response
            $statusCode = [int]$response.StatusCode
            try {
                $reader = New-Object System.IO.StreamReader($response.GetResponseStream(), [System.Text.Encoding]::UTF8)
                $errBody = $reader.ReadToEnd()
                $reader.Close()
            } catch { } finally {
                $response.Close()
            }
        }
        return @{ Success = $false; Body = $errBody; Error = $_.Exception.Message; StatusCode = $statusCode }
    } catch {
        return @{ Success = $false; Body = ''; Error = $_.Exception.Message; StatusCode = 0 }
    }
}

# -- Main ----------------------------------------------------------------------
Initialize-HookLog
Write-HookLog "hook invoked  pid=$PID"

try {
    # Read stdin using OpenStandardInput for reliable cross-process piping (WSL interop).
    $stdinStream = [Console]::OpenStandardInput()
    $reader = New-Object System.IO.StreamReader($stdinStream, [System.Text.Encoding]::UTF8)

    # Async read with 5-second timeout to avoid blocking if stdin never closes.
    $readTask = $reader.ReadToEndAsync()
    if (-not $readTask.Wait(5000)) {
        Write-HookLog "stdin read timed out after 5s -- fail-open"
        $reader.Dispose()
        exit 0
    }
    $inputJson = $readTask.Result
    $reader.Dispose()

    $stdinLen = if ($inputJson) { $inputJson.Length } else { 0 }
    Write-HookLog "stdin received  bytes=$stdinLen"

    if (-not $inputJson) {
        Write-HookLog "empty stdin -- fail-open  exit=0"
        exit 0
    }

    $payload = $inputJson | ConvertFrom-Json
    $promptText = $payload.prompt
    if (-not $promptText) {
        Write-HookLog "no prompt field in payload -- fail-open  exit=0"
        exit 0
    }

    Write-HookLog "prompt parsed  len=$($promptText.Length)"

    $evalBody = @{
        prompt_text      = $promptText
        surface          = "claude_code"
        app_name         = "Claude Code"
        vendor           = "anthropic"
        service_category = "ai_code"
        destination_host = "api.anthropic.com"
        metadata         = @{
            session_id = $payload.session_id
            cwd        = $payload.cwd
            hook_event = $payload.hook_event_name
        }
    } | ConvertTo-Json -Depth 4 -Compress

    Write-HookLog "calling evaluator  POST http://127.0.0.1:17175/v1/prompt/evaluate"

    $evalResult = Invoke-ThemistoAPI 'http://127.0.0.1:17175/v1/prompt/evaluate' $evalBody 10000

    if (-not $evalResult.Success) {
        Write-HookLog "evaluator error  status=$($evalResult.StatusCode)  error=$($evalResult.Error)  body=$($evalResult.Body) -- fail-open  exit=0"
        # Best-effort degraded outcome.
        try {
            $outcomeBody = @{
                surface          = "claude_code"
                outcome          = "degraded_fail_open"
                destination_host = "api.anthropic.com"
                vendor           = "anthropic"
                service_category = "ai_code"
                app_name         = "Claude Code"
                error            = $evalResult.Error
            } | ConvertTo-Json -Depth 4 -Compress
            Invoke-ThemistoAPI 'http://127.0.0.1:17175/v1/prompt/outcome' $outcomeBody 3000 | Out-Null
        } catch { }
        exit 0
    }

    $evalResponse = $evalResult.Body | ConvertFrom-Json
    $decision = $evalResponse.decision
    $evaluationId = $evalResponse.evaluation_id
    Write-HookLog "evaluator responded  decision=$decision  evaluation_id=$evaluationId"

    if ($decision -eq 'block') {
        $message = $evalResponse.message
        if (-not $message) { $message = "Blocked by organization policy." }

        Write-HookLog "BLOCKED  exit=2"
        # Report blocked outcome.
        try {
            $outcomeBody = @{
                evaluation_id = $evaluationId
                surface       = "claude_code"
                outcome       = "blocked"
                app_name      = "Claude Code"
            } | ConvertTo-Json -Depth 4 -Compress
            Invoke-ThemistoAPI 'http://127.0.0.1:17175/v1/prompt/outcome' $outcomeBody 3000 | Out-Null
        } catch { }

        [Console]::Error.WriteLine("[Themisto] " + $message)
        exit 2
    } elseif ($decision -eq 'alert') {
        $message = $evalResponse.message
        if (-not $message) { $message = "Themisto flagged this prompt for sensitive content." }

        Write-HookLog "ALERT  evaluation_id=$evaluationId -- allow with warning  exit=0"
        try {
            $outcomeBody = @{
                evaluation_id      = $evaluationId
                surface            = "claude_code"
                outcome            = "allowed"
                app_name           = "Claude Code"
                user_message_shown = $true
            } | ConvertTo-Json -Depth 4 -Compress
            Invoke-ThemistoAPI 'http://127.0.0.1:17175/v1/prompt/outcome' $outcomeBody 3000 | Out-Null
        } catch { }

        [Console]::Error.WriteLine("[Themisto] Warning: " + $message)
        exit 0
    }

    Write-HookLog "ALLOWED  decision=$decision  exit=0"
    # Allowed / forward: report outcome and continue.
    try {
        $outcomeBody = @{
            evaluation_id = $evaluationId
            surface       = "claude_code"
            outcome       = "allowed"
            app_name      = "Claude Code"
        } | ConvertTo-Json -Depth 4 -Compress
        Invoke-ThemistoAPI 'http://127.0.0.1:17175/v1/prompt/outcome' $outcomeBody 3000 | Out-Null
    } catch { }
    exit 0

} catch {
    $fatalErr = $_.Exception.Message
    Write-HookLog "FATAL  error=$fatalErr -- fail-open  exit=0"
    exit 0
}
`
