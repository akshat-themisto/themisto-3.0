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

// WindsurfIntegrationStatus is the authoritative status for Windsurf hook integration.
type WindsurfIntegrationStatus struct {
	Installed        bool   `json:"installed"`
	CLIPath          string `json:"cli_path,omitempty"`
	HooksConfigPath  string `json:"hooks_config_path,omitempty"`
	SystemHooksPath  string `json:"system_hooks_path,omitempty"`
	HookConfigured   bool   `json:"hook_configured"`
	HookScriptExists bool   `json:"hook_script_exists"`
	HookSource       string `json:"hook_source,omitempty"`
	IntegrationState string `json:"integration_state"`
	Confidence       string `json:"confidence"`
	Detail           string `json:"detail"`
	Remediation      string `json:"remediation"`
	LastError        string `json:"last_error,omitempty"`
	SystemManaged    bool   `json:"system_managed,omitempty"`
	WorkspaceManaged bool   `json:"workspace_managed,omitempty"`
}

const (
	windsurfHookScriptMarker   = "windsurf-hook.ps1"
	windsurfHookScriptSHMarker = "windsurf-hook.sh"
	windsurfHookLauncherMarker = "windsurf-hook.cmd"
	windsurfHookAgentFlag      = "-windsurf-hook"
)

var (
	windsurfStatusMu       sync.Mutex
	windsurfStatusCached   WindsurfIntegrationStatus
	windsurfStatusCachedAt time.Time
)

func windsurfUserHooksPath() string {
	home := os.Getenv("USERPROFILE")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, ".codeium", "windsurf", "hooks.json")
}

func windsurfSystemHooksPath() string {
	if runtime.GOOS == "windows" {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "Windsurf", "hooks.json")
	}
	return "/etc/windsurf/hooks.json"
}

func windsurfHookScriptPath() string {
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			home := os.Getenv("USERPROFILE")
			if home == "" {
				home, _ = os.UserHomeDir()
			}
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(localAppData, "Themisto", "hooks", "windsurf-hook.ps1")
	}
	return filepath.Join(themistoHooksBaseDir(), windsurfHookScriptSHMarker)
}

func windsurfHookLauncherPath() string {
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			home := os.Getenv("USERPROFILE")
			if home == "" {
				home, _ = os.UserHomeDir()
			}
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(localAppData, "Themisto", "hooks", windsurfHookLauncherMarker)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "themisto", "hooks", "windsurf-hook.sh")
}

func detectWindsurfInstalled() (installed bool, cliPath string) {
	if p, err := exec.LookPath("windsurf"); err == nil {
		return true, p
	}

	if runtime.GOOS == "windows" {
		if out, err := newHiddenCommand("where.exe", "windsurf").Output(); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" {
					return true, line
				}
			}
		}

		if found, path := checkWindsurfCommonPaths(); found {
			return true, path
		}
		if found, _ := checkWindsurfRegistry(); found {
			return true, ""
		}
	}

	windsurfDir := filepath.Dir(windsurfUserHooksPath())
	if info, err := os.Stat(windsurfDir); err == nil && info.IsDir() {
		return true, ""
	}

	return false, ""
}

func detectWindsurfIntegration() WindsurfIntegrationStatus {
	status := detectWindsurfIntegrationWithInstallDetector(
		windsurfUserHooksPath(),
		windsurfSystemHooksPath(),
		windsurfHookScriptPath(),
		detectWindsurfInstalled,
	)
	windsurfStatusMu.Lock()
	windsurfStatusCached = status
	windsurfStatusCachedAt = time.Now()
	windsurfStatusMu.Unlock()
	return status
}

// cachedWindsurfIntegrationForDiagnostics returns the most recent cached status
// if available, avoiding redundant detection during diagnostics snapshot collection.
func cachedWindsurfIntegrationForDiagnostics() WindsurfIntegrationStatus {
	windsurfStatusMu.Lock()
	cached := windsurfStatusCached
	cachedAt := windsurfStatusCachedAt
	windsurfStatusMu.Unlock()

	if !cachedAt.IsZero() && time.Since(cachedAt) < 30*time.Second {
		return cached
	}
	return detectWindsurfIntegration()
}

func detectWindsurfIntegrationAt(userHooksPath, systemHooksPath, scriptPath string) WindsurfIntegrationStatus {
	return detectWindsurfIntegrationFull(
		userHooksPath,
		systemHooksPath,
		nil, // no workspace paths
		scriptPath,
		func() (bool, string) { return false, "" },
	)
}

func detectWindsurfIntegrationWithInstallDetector(userHooksPath, systemHooksPath, scriptPath string, installDetector func() (bool, string)) WindsurfIntegrationStatus {
	return detectWindsurfIntegrationFull(
		userHooksPath,
		systemHooksPath,
		nil, // no workspace paths
		scriptPath,
		installDetector,
	)
}

// detectWindsurfIntegrationFull is the full detection function that checks user,
// system, and workspace-level hooks. workspaceHooksPaths may be nil or empty
// when no workspace context is available (e.g. desktop diagnostics).
func detectWindsurfIntegrationFull(userHooksPath, systemHooksPath string, workspaceHooksPaths []string, scriptPath string, installDetector func() (bool, string)) WindsurfIntegrationStatus {
	status := WindsurfIntegrationStatus{
		HooksConfigPath: userHooksPath,
		SystemHooksPath: systemHooksPath,
		Confidence:      "high",
	}

	installed, cliPath := installDetector()
	status.Installed = installed
	status.CLIPath = cliPath

	userConfig, userExists, err := readWindsurfHooksConfig(userHooksPath)
	if err != nil {
		status.IntegrationState = "check_failed"
		status.Detail = "Could not read Windsurf hooks.json to check hook configuration."
		status.Remediation = "Verify that " + userHooksPath + " is readable and valid JSON."
		status.LastError = err.Error()
		return status
	}

	systemConfig, systemExists, err := readWindsurfHooksConfig(systemHooksPath)
	if err != nil {
		status.IntegrationState = "check_failed"
		status.Detail = "Could not read Windsurf system hooks.json to check hook configuration."
		status.Remediation = "Verify that " + systemHooksPath + " is readable and valid JSON."
		status.LastError = err.Error()
		return status
	}

	status.SystemManaged = systemExists

	userHookPresent := windsurfHooksContainThemisto(userConfig)
	systemHookPresent := windsurfHooksContainThemisto(systemConfig)

	// Check workspace-level hooks (.windsurf/hooks.json in project roots).
	workspaceHookPresent := false
	for _, wsPath := range workspaceHooksPaths {
		if wsPath == "" {
			continue
		}
		wsConfig, wsExists, wsErr := readWindsurfHooksConfig(wsPath)
		if wsErr != nil {
			continue // workspace config is best-effort; don't fail the whole check
		}
		if wsExists {
			status.WorkspaceManaged = true
			if windsurfHooksContainThemisto(wsConfig) {
				workspaceHookPresent = true
			}
		}
	}

	status.HookConfigured = userHookPresent || systemHookPresent || workspaceHookPresent

	// Build hook source label describing where the hook was found.
	var sources []string
	if userHookPresent {
		sources = append(sources, "user")
	}
	if systemHookPresent {
		sources = append(sources, "system")
	}
	if workspaceHookPresent {
		sources = append(sources, "workspace")
	}
	if len(sources) > 0 {
		status.HookSource = strings.Join(sources, "_and_")
	}

	launcherPath := filepath.Join(filepath.Dir(scriptPath), windsurfHookLauncherMarker)
	windsurfSupportDetail := ""
	if _, err := os.Stat(scriptPath); err == nil {
		// For PS1 scripts both the script and the .cmd launcher must exist;
		// for shell scripts the script alone is sufficient.
		scriptPresent := !isPowerShellScriptPath(scriptPath)
		if !scriptPresent {
			if _, err := os.Stat(launcherPath); err == nil {
				scriptPresent = true
			}
		}
		if scriptPresent {
			status.HookScriptExists = true
			if supported, detail := validateWindsurfHookScript(scriptPath); !supported {
				status.HookScriptExists = false
				windsurfSupportDetail = detail
			}
		}
	}

	if !status.Installed {
		windsurfDir := filepath.Dir(userHooksPath)
		if info, err := os.Stat(windsurfDir); err == nil && info.IsDir() {
			status.Installed = true
			status.Confidence = "medium"
		}
	}

	if !status.Installed {
		status.IntegrationState = "not_installed"
		status.Detail = "Windsurf was not detected on this machine."
		status.Remediation = "Install Windsurf to enable prompt monitoring integration."
		return status
	}

	// For workspace-only hooks, the hook script may not exist locally (the
	// workspace hook could invoke a different script or binary). Treat
	// workspace-only hook presence the same as configured even without the
	// local script, since the workspace manages its own command.
	workspaceOnlyHook := workspaceHookPresent && !userHookPresent && !systemHookPresent

	switch {
	case status.HookConfigured && (status.HookScriptExists || workspaceOnlyHook):
		status.IntegrationState = "configured"
		if strings.Contains(status.HookSource, "workspace") {
			status.Detail = "Themisto found a Windsurf monitoring hook in a workspace .windsurf/hooks.json. This confirms prompt monitoring configuration on disk, not reliable runtime blocking."
			status.Remediation = "Workspace hooks apply to this project. Restart Windsurf after changes, and treat this integration as audit-only until Windsurf reliably honors prompt blocking at runtime."
		} else if status.HookSource == "system" {
			status.Detail = "Themisto found a Windsurf monitoring hook in system hooks.json and the local hook assets exist. This confirms audit coverage on disk, not reliable runtime blocking."
			status.Remediation = "Restart Windsurf after changes. If your team expects hard blocking, treat that as a Windsurf runtime limitation until the vendor honors blocking reliably."
		} else if strings.Contains(status.HookSource, "system") {
			status.Detail = "Themisto found Windsurf monitoring hooks in multiple sources (" + status.HookSource + "), and the local hook assets exist. This confirms audit coverage on disk, not reliable runtime blocking."
			status.Remediation = "Restart Windsurf after changes. System-managed hooks execute first; ask IT to reconcile duplicate hook sources if monitoring behavior is unexpected."
		} else {
			status.Detail = "Themisto found a Windsurf monitoring hook in user hooks.json and the local hook assets exist. This confirms audit coverage on disk, not reliable runtime blocking."
			status.Remediation = "Restart Windsurf after changes. Themisto will monitor prompts and record what would have been blocked, but prompts may still reach the model."
		}
	case status.HookConfigured && !status.HookScriptExists:
		status.IntegrationState = "missing_script"
		if windsurfSupportDetail != "" {
			status.Detail = windsurfSupportDetail
			status.Remediation = "Upgrade or reinstall the local Themisto agent so the Windsurf hook runner is available, then use Repair Monitoring to rewrite the hook script."
		} else {
			status.Detail = "Windsurf is configured to run the Themisto monitoring hook, but the local hook assets are missing from disk."
			status.Remediation = "Use Repair Monitoring to recreate the Windsurf monitoring assets."
		}
	case !status.HookConfigured && status.HookScriptExists:
		status.IntegrationState = "partial"
		status.Detail = "The Themisto Windsurf monitoring assets exist on disk, but no pre_user_prompt hook references them."
		status.Remediation = "Use Enable Monitoring to add the Themisto Windsurf hook to hooks.json."
	default:
		status.IntegrationState = "missing_hook"
		status.Confidence = "medium"
		if status.SystemManaged || status.WorkspaceManaged {
			status.Detail = "Windsurf is installed, but Themisto did not find a pre_user_prompt monitoring hook in any hooks source checked."
			status.Remediation = "Click Enable Monitoring to add a user-level hook, or check whether hooks are managed at the system or workspace level."
		} else {
			status.Detail = "Windsurf is installed, but Themisto did not find a pre_user_prompt monitoring hook in hooks.json."
			status.Remediation = "Click Enable Monitoring to add the Themisto prompt monitoring hook to Windsurf."
		}
	}

	if !userExists && status.HooksConfigPath == "" {
		status.HooksConfigPath = userHooksPath
	}

	return status
}

func readWindsurfHooksConfig(path string) (map[string]interface{}, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read Windsurf hooks config: %w", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, true, fmt.Errorf("parse Windsurf hooks config: %w", err)
	}

	return config, true, nil
}

func windsurfHooksContainThemisto(config map[string]interface{}) bool {
	if config == nil {
		return false
	}

	hooks, ok := config["hooks"].(map[string]interface{})
	if !ok {
		return false
	}

	entries, ok := hooks["pre_user_prompt"].([]interface{})
	if !ok {
		return false
	}

	for _, entry := range entries {
		if windsurfEntryContainsThemisto(entry) {
			return true
		}
	}

	return false
}

func windsurfEntryContainsThemisto(entry interface{}) bool {
	entryMap, ok := entry.(map[string]interface{})
	if !ok {
		return false
	}
	command, _ := entryMap["command"].(string)
	command = strings.ToLower(command)
	return strings.Contains(command, strings.ToLower(windsurfHookScriptMarker)) ||
		strings.Contains(command, strings.ToLower(windsurfHookLauncherMarker)) ||
		strings.Contains(command, windsurfHookScriptSHMarker) ||
		(strings.Contains(command, strings.ToLower(agentBinaryPath)) && strings.Contains(command, windsurfHookAgentFlag))
}

func installWindsurfHook() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("Windsurf hook installation is only supported on Windows")
	}
	return installWindsurfHookAt(windsurfUserHooksPath(), windsurfHookScriptPath(), windsurfHookLauncherPath())
}

func installWindsurfHookAt(userHooksPath, scriptPath, launcherPath string) error {
	if err := ensureWindsurfHookScript(scriptPath); err != nil {
		return fmt.Errorf("write Windsurf hook script: %w", err)
	}
	// The .cmd launcher is only used with PS1 scripts (Windows). Shell scripts
	// on macOS/Linux invoke the agent directly with no launcher file.
	hookCommandPath := scriptPath
	if isPowerShellScriptPath(scriptPath) {
		if err := ensureWindsurfHookLauncher(launcherPath); err != nil {
			return fmt.Errorf("write Windsurf hook launcher: %w", err)
		}
		hookCommandPath = launcherPath
	}

	hooksDir := filepath.Dir(userHooksPath)
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("create Windsurf hooks directory: %w", err)
	}

	config := map[string]interface{}{
		"hooks": map[string]interface{}{},
	}
	if data, err := os.ReadFile(userHooksPath); err == nil {
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("existing Windsurf hooks.json is invalid JSON: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read Windsurf hooks.json: %w", err)
	}

	upsertWindsurfThemistoHook(config, buildWindsurfHookCommand(hookCommandPath))
	return writeJSONAtomic(userHooksPath, config)
}

func repairWindsurfHook() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("Windsurf hook repair is only supported on Windows")
	}
	return repairWindsurfHookAt(windsurfUserHooksPath(), windsurfHookScriptPath(), windsurfHookLauncherPath())
}

func repairWindsurfHookAt(userHooksPath, scriptPath, launcherPath string) error {
	if err := removeWindsurfHookAt(userHooksPath); err != nil {
		return fmt.Errorf("remove during repair: %w", err)
	}
	return installWindsurfHookAt(userHooksPath, scriptPath, launcherPath)
}

func removeWindsurfHook() error {
	return removeWindsurfHookAt(windsurfUserHooksPath())
}

func removeWindsurfHookAt(userHooksPath string) error {
	data, err := os.ReadFile(userHooksPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read Windsurf hooks.json: %w", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parse Windsurf hooks.json: %w", err)
	}

	if !removeWindsurfThemistoHook(config) {
		return nil
	}

	return writeJSONAtomic(userHooksPath, config)
}

func ensureWindsurfHookScript(scriptPath string) error {
	dir := filepath.Dir(scriptPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create hook directory %s: %w", dir, err)
	}
	content := windsurfHookPS1
	mode := os.FileMode(0644)
	if !isPowerShellScriptPath(scriptPath) {
		content = posixHookScript(windsurfHookAgentFlag, nil)
		mode = 0755
	}
	return os.WriteFile(scriptPath, []byte(content), mode)
}

func ensureWindsurfHookLauncher(launcherPath string) error {
	dir := filepath.Dir(launcherPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create hook directory %s: %w", dir, err)
	}
	content := fmt.Sprintf("@echo off\r\n\"%s\" %s\r\nexit /b %%ERRORLEVEL%%\r\n", agentBinaryPath, windsurfHookAgentFlag)
	return os.WriteFile(launcherPath, []byte(content), 0644)
}

// validateWindsurfHookScript checks whether the installed agent binary supports the
// Windsurf hook flag. PS1 scripts are Windows-only and assumed capable; shell scripts
// require the installed agent to accept -windsurf-hook.
func validateWindsurfHookScript(scriptPath string) (bool, string) {
	if isPowerShellScriptPath(scriptPath) {
		return true, ""
	}
	supported, detail := agentBinarySupportsHookFlag(windsurfHookAgentFlag)
	if supported {
		return true, ""
	}
	detail = strings.TrimSpace(detail)
	if detail != "" {
		detail = ": " + detail
	}
	return false, fmt.Sprintf("The Themisto Windsurf hook script exists, but the installed Themisto agent at %s does not support Windsurf hook execution yet%s", agentBinaryPath, detail)
}

// buildWindsurfHookCommand builds a platform-appropriate hook command string.
// On Windows, invoke a local launcher script via cmd.exe so the executable
// path itself has no spaces from Windsurf's point of view, while the launcher
// forwards the agent's exit code back to Windsurf.
func buildWindsurfHookCommand(scriptPath string) string {
	if runtime.GOOS == "windows" {
		comspec := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
		if strings.TrimSpace(os.Getenv("SystemRoot")) == "" {
			comspec = `C:\Windows\System32\cmd.exe`
		}
		return fmt.Sprintf(`%s /d /c ""%s""`, comspec, scriptPath)
	}
	return scriptPath
}

func upsertWindsurfThemistoHook(config map[string]interface{}, command string) {
	hooks, ok := config["hooks"].(map[string]interface{})
	if !ok {
		hooks = make(map[string]interface{})
		config["hooks"] = hooks
	}

	existing, _ := hooks["pre_user_prompt"].([]interface{})
	cleaned := make([]interface{}, 0, len(existing))
	for _, entry := range existing {
		if !windsurfEntryContainsThemisto(entry) {
			cleaned = append(cleaned, entry)
		}
	}

	cleaned = append(cleaned, map[string]interface{}{
		"command": command,
	})
	hooks["pre_user_prompt"] = cleaned
}

func removeWindsurfThemistoHook(config map[string]interface{}) bool {
	hooks, ok := config["hooks"].(map[string]interface{})
	if !ok {
		return false
	}

	existing, ok := hooks["pre_user_prompt"].([]interface{})
	if !ok {
		return false
	}

	cleaned := make([]interface{}, 0, len(existing))
	for _, entry := range existing {
		if !windsurfEntryContainsThemisto(entry) {
			cleaned = append(cleaned, entry)
		}
	}

	if len(cleaned) == 0 {
		delete(hooks, "pre_user_prompt")
	} else {
		hooks["pre_user_prompt"] = cleaned
	}

	if len(hooks) == 0 {
		delete(config, "hooks")
	}

	return true
}

const windsurfHookPS1 = `# Themisto Windsurf Hook - pre_user_prompt
# Evaluates prompts via the local Themisto agent before Windsurf processes them.
# Exit 0 = allow, Exit 2 = block (stderr shown in Cascade UI).
# Logs to %LOCALAPPDATA%\Themisto\logs\windsurf-hook.log for diagnostics.

$script:LogPath = $null
function Initialize-HookLog {
    try {
        $logDir = Join-Path $env:LOCALAPPDATA 'Themisto\logs'
        if (-not (Test-Path $logDir)) { New-Item -ItemType Directory -Path $logDir -Force | Out-Null }
        $script:LogPath = Join-Path $logDir 'windsurf-hook.log'
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
        [IO.File]::AppendAllText($script:LogPath, "$ts  $msg" + [Environment]::NewLine)
    } catch { }
}
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

Initialize-HookLog
Write-HookLog "hook invoked  pid=$PID"

try {
    $stdinStream = [Console]::OpenStandardInput()
    $reader = New-Object System.IO.StreamReader($stdinStream, [System.Text.Encoding]::UTF8)
    $readTask = $reader.ReadToEndAsync()
    if (-not $readTask.Wait(5000)) {
        Write-HookLog "stdin read timed out after 5s -- allow"
        $reader.Dispose()
        exit 0
    }

    $inputJson = $readTask.Result
    $reader.Dispose()

    $stdinLen = if ($inputJson) { $inputJson.Length } else { 0 }
    Write-HookLog "stdin received  bytes=$stdinLen"

    if (-not $inputJson) {
        Write-HookLog "empty stdin -- allow"
        exit 0
    }

    $payload = $inputJson | ConvertFrom-Json
    $promptText = $null
    if ($payload.tool_info -and $payload.tool_info.user_prompt) {
        $promptText = $payload.tool_info.user_prompt
    }
    if (-not $promptText) {
        Write-HookLog "no user_prompt in tool_info -- allow"
        exit 0
    }

    Write-HookLog "prompt parsed  len=$($promptText.Length)"

    $metadata = @{
        trajectory_id = [string]$payload.trajectory_id
        execution_id  = [string]$payload.execution_id
        action_name   = [string]$payload.agent_action_name
    }

    $evalBody = @{
        prompt_text      = $promptText
        surface          = "windsurf"
        app_name         = "Windsurf"
        vendor           = "windsurf"
        service_category = "ai_code"
        destination_host = "windsurf.ai"
        metadata         = $metadata
    } | ConvertTo-Json -Depth 6 -Compress

    Write-HookLog "calling evaluator  POST http://127.0.0.1:17175/v1/prompt/evaluate"
    $evalResult = Invoke-ThemistoAPI 'http://127.0.0.1:17175/v1/prompt/evaluate' $evalBody 10000

    if (-not $evalResult.Success) {
        Write-HookLog "evaluator error  status=$($evalResult.StatusCode)  error=$($evalResult.Error) -- allow"
        try {
            $outcomeBody = @{
                surface          = "windsurf"
                outcome          = "degraded_fail_open"
                destination_host = "windsurf.ai"
                vendor           = "windsurf"
                service_category = "ai_code"
                app_name         = "Windsurf"
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
        Write-HookLog "BLOCKED  exit=2"
        try {
            $outcomeBody = @{
                evaluation_id = $evaluationId
                surface       = "windsurf"
                outcome       = "blocked"
                app_name      = "Windsurf"
            } | ConvertTo-Json -Depth 4 -Compress
            Invoke-ThemistoAPI 'http://127.0.0.1:17175/v1/prompt/outcome' $outcomeBody 3000 | Out-Null
        } catch { }
        $blockMsg = $evalResponse.message
        if (-not $blockMsg) { $blockMsg = "Prompt blocked by Themisto governance policy." }
        [Console]::Error.WriteLine($blockMsg)
        exit 2
    }

    Write-HookLog "ALLOWED  decision=$decision  exit=0"
    try {
        $outcomeBody = @{
            evaluation_id = $evaluationId
            surface       = "windsurf"
            outcome       = "allowed"
            app_name      = "Windsurf"
        } | ConvertTo-Json -Depth 4 -Compress
        Invoke-ThemistoAPI 'http://127.0.0.1:17175/v1/prompt/outcome' $outcomeBody 3000 | Out-Null
    } catch { }

    exit 0

} catch {
    Write-HookLog "FATAL  error=$($_.Exception.Message) -- allow"
    exit 0
}
`
