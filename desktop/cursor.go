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

// CursorIntegrationStatus is the authoritative status for Cursor hook integration.
type CursorIntegrationStatus struct {
	Installed           bool   `json:"installed"`
	CLIPath             string `json:"cli_path,omitempty"`
	HooksConfigPath     string `json:"hooks_config_path,omitempty"`
	EnterpriseHooksPath string `json:"enterprise_hooks_path,omitempty"`
	HookConfigured      bool   `json:"hook_configured"`
	HookScriptExists    bool   `json:"hook_script_exists"`
	HookSource          string `json:"hook_source,omitempty"`
	IntegrationState    string `json:"integration_state"`
	Confidence          string `json:"confidence"`
	Detail              string `json:"detail"`
	Remediation         string `json:"remediation"`
	LastError           string `json:"last_error,omitempty"`
	EnterpriseManaged   bool   `json:"enterprise_managed,omitempty"`
}

const cursorHookScriptMarker = "cursor-hook.ps1"

// cursorHookScriptSHMarker is the filename used on macOS/Linux.
const cursorHookScriptSHMarker = "cursor-hook.sh"

var (
	cursorStatusMu       sync.Mutex
	cursorStatusCached   CursorIntegrationStatus
	cursorStatusCachedAt time.Time
)

func cursorUserHooksPath() string {
	home := os.Getenv("USERPROFILE")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, ".cursor", "hooks.json")
}

func cursorEnterpriseHooksPath() string {
	if runtime.GOOS == "windows" {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "Cursor", "hooks.json")
	}
	return "/etc/cursor/hooks.json"
}

func cursorHookScriptPath() string {
	if runtime.GOOS != "windows" {
		return filepath.Join(themistoHooksBaseDir(), cursorHookScriptSHMarker)
	}
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		home := os.Getenv("USERPROFILE")
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		localAppData = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(localAppData, "Themisto", "hooks", cursorHookScriptMarker)
}

func detectCursorInstalled() (installed bool, cliPath string) {
	if p, err := exec.LookPath("cursor"); err == nil {
		return true, p
	}

	if runtime.GOOS == "windows" {
		if out, err := newHiddenCommand("where.exe", "cursor").Output(); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" {
					return true, line
				}
			}
		}

		if found, path := checkCursorCommonPaths(); found {
			return true, path
		}
		if found, _ := checkCursorRegistry(); found {
			return true, ""
		}
	}

	cursorDir := filepath.Dir(cursorUserHooksPath())
	if info, err := os.Stat(cursorDir); err == nil && info.IsDir() {
		return true, ""
	}

	return false, ""
}

func detectCursorIntegration() CursorIntegrationStatus {
	status := detectCursorIntegrationWithInstallDetector(
		cursorUserHooksPath(),
		cursorEnterpriseHooksPath(),
		cursorHookScriptPath(),
		detectCursorInstalled,
	)
	cursorStatusMu.Lock()
	cursorStatusCached = status
	cursorStatusCachedAt = time.Now()
	cursorStatusMu.Unlock()
	return status
}

// cachedCursorIntegrationForDiagnostics returns the most recent cached status
// if available, avoiding redundant detection during diagnostics snapshot collection.
func cachedCursorIntegrationForDiagnostics() CursorIntegrationStatus {
	cursorStatusMu.Lock()
	cached := cursorStatusCached
	cachedAt := cursorStatusCachedAt
	cursorStatusMu.Unlock()

	if !cachedAt.IsZero() && time.Since(cachedAt) < 30*time.Second {
		return cached
	}
	return detectCursorIntegration()
}

func detectCursorIntegrationAt(userHooksPath, enterpriseHooksPath, scriptPath string) CursorIntegrationStatus {
	return detectCursorIntegrationWithInstallDetector(
		userHooksPath,
		enterpriseHooksPath,
		scriptPath,
		func() (bool, string) { return false, "" },
	)
}

func detectCursorIntegrationWithInstallDetector(userHooksPath, enterpriseHooksPath, scriptPath string, installDetector func() (bool, string)) CursorIntegrationStatus {
	status := CursorIntegrationStatus{
		HooksConfigPath:     userHooksPath,
		EnterpriseHooksPath: enterpriseHooksPath,
		Confidence:          "high",
	}

	installed, cliPath := installDetector()
	status.Installed = installed
	status.CLIPath = cliPath

	userConfig, userExists, err := readCursorHooksConfig(userHooksPath)
	if err != nil {
		status.IntegrationState = "check_failed"
		status.Detail = "Could not read Cursor hooks.json to check hook configuration."
		status.Remediation = "Verify that " + userHooksPath + " is readable and valid JSON."
		status.LastError = err.Error()
		return status
	}

	enterpriseConfig, enterpriseExists, err := readCursorHooksConfig(enterpriseHooksPath)
	if err != nil {
		status.IntegrationState = "check_failed"
		status.Detail = "Could not read Cursor enterprise hooks.json to check hook configuration."
		status.Remediation = "Verify that " + enterpriseHooksPath + " is readable and valid JSON."
		status.LastError = err.Error()
		return status
	}

	status.EnterpriseManaged = enterpriseExists

	userHookPresent := cursorHooksContainThemisto(userConfig)
	enterpriseHookPresent := cursorHooksContainThemisto(enterpriseConfig)
	status.HookConfigured = userHookPresent || enterpriseHookPresent

	switch {
	case userHookPresent && enterpriseHookPresent:
		status.HookSource = "user_and_enterprise"
	case enterpriseHookPresent:
		status.HookSource = "enterprise"
	case userHookPresent:
		status.HookSource = "user"
	}

	scriptSupportDetail := ""
	if _, err := os.Stat(scriptPath); err == nil {
		status.HookScriptExists = true
		if supported, detail := validateCursorHookScript(scriptPath); !supported {
			status.HookScriptExists = false
			scriptSupportDetail = detail
		}
	}

	if !status.Installed {
		cursorDir := filepath.Dir(userHooksPath)
		if info, err := os.Stat(cursorDir); err == nil && info.IsDir() {
			status.Installed = true
			status.Confidence = "medium"
		}
	}

	if !status.Installed {
		status.IntegrationState = "not_installed"
		status.Detail = "Cursor was not detected on this machine."
		status.Remediation = "Install Cursor to enable prompt governance integration."
		return status
	}

	switch {
	case status.HookConfigured && status.HookScriptExists:
		status.IntegrationState = "configured"
		switch status.HookSource {
		case "enterprise":
			status.Detail = "Themisto found a Cursor hook in enterprise hooks.json and the local hook script exists."
			status.Remediation = "Cursor should pick up enterprise hook changes automatically on restart. If prompt governance still does not apply, ask IT to verify the managed hooks rollout."
		case "user_and_enterprise":
			status.Detail = "Themisto found Cursor hooks in both user and enterprise hooks.json, and the local hook script exists."
			status.Remediation = "Enterprise-managed hooks may take precedence. Restart Cursor after hook changes and ask IT to reconcile duplicate hook sources if behavior is unexpected."
		default:
			status.Detail = "Themisto found a Cursor hook in user hooks.json and the local hook script exists."
			status.Remediation = "Restart Cursor if you recently installed or changed the hook so the current session picks it up cleanly."
		}
	case status.HookConfigured && !status.HookScriptExists:
		status.IntegrationState = "missing_script"
		if scriptSupportDetail != "" {
			status.Detail = scriptSupportDetail
			status.Remediation = "Upgrade or reinstall the local Themisto agent so the Cursor hook runner is available, then use Repair to rewrite the hook script."
		} else {
			status.Detail = "Cursor is configured to run the Themisto hook, but the local hook script is missing from disk."
			status.Remediation = "Use Repair to reinstall the Cursor hook script."
		}
	case !status.HookConfigured && status.HookScriptExists:
		status.IntegrationState = "partial"
		status.Detail = "The Themisto Cursor hook script exists on disk, but no beforeSubmitPrompt hook references it."
		status.Remediation = "Use Install to add the Themisto hook to Cursor hooks.json."
	default:
		status.IntegrationState = "missing_hook"
		status.Confidence = "medium"
		if status.EnterpriseManaged {
			status.Detail = "Cursor is installed, but Themisto did not find a beforeSubmitPrompt hook in user or enterprise hooks.json."
			status.Remediation = "Click Install to add a user-level hook, or ask IT whether Cursor hooks are centrally managed on this device."
		} else {
			status.Detail = "Cursor is installed, but Themisto did not find a beforeSubmitPrompt hook in hooks.json."
			status.Remediation = "Click Install to add the Themisto prompt governance hook to Cursor."
		}
	}

	if !userExists && status.HooksConfigPath == "" {
		status.HooksConfigPath = userHooksPath
	}

	return status
}

func readCursorHooksConfig(path string) (map[string]interface{}, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read Cursor hooks config: %w", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, true, fmt.Errorf("parse Cursor hooks config: %w", err)
	}

	return config, true, nil
}

func cursorHooksContainThemisto(config map[string]interface{}) bool {
	if config == nil {
		return false
	}

	hooks, ok := config["hooks"].(map[string]interface{})
	if !ok {
		return false
	}

	entries, ok := hooks["beforeSubmitPrompt"].([]interface{})
	if !ok {
		return false
	}

	for _, entry := range entries {
		if cursorEntryContainsThemisto(entry) {
			return true
		}
	}

	return false
}

func cursorEntryContainsThemisto(entry interface{}) bool {
	entryMap, ok := entry.(map[string]interface{})
	if !ok {
		return false
	}
	command, _ := entryMap["command"].(string)
	return strings.Contains(command, cursorHookScriptMarker) ||
		strings.Contains(command, cursorHookScriptSHMarker)
}

func installCursorHook() error {
	return installCursorHookAt(cursorUserHooksPath(), cursorHookScriptPath())
}

func installCursorHookAt(userHooksPath, scriptPath string) error {
	if err := ensureCursorHookScript(scriptPath); err != nil {
		return fmt.Errorf("write Cursor hook script: %w", err)
	}

	hooksDir := filepath.Dir(userHooksPath)
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("create Cursor hooks directory: %w", err)
	}

	config := map[string]interface{}{
		"version": 1,
		"hooks":   map[string]interface{}{},
	}
	if data, err := os.ReadFile(userHooksPath); err == nil {
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("existing Cursor hooks.json is invalid JSON: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read Cursor hooks.json: %w", err)
	}

	upsertCursorThemistoHook(config, buildCursorHookCommand(scriptPath))
	return writeJSONAtomic(userHooksPath, config)
}

func repairCursorHook() error {
	return repairCursorHookAt(cursorUserHooksPath(), cursorHookScriptPath())
}

func repairCursorHookAt(userHooksPath, scriptPath string) error {
	if err := removeCursorHookAt(userHooksPath); err != nil {
		return fmt.Errorf("remove during repair: %w", err)
	}
	return installCursorHookAt(userHooksPath, scriptPath)
}

func removeCursorHook() error {
	return removeCursorHookAt(cursorUserHooksPath())
}

func removeCursorHookAt(userHooksPath string) error {
	data, err := os.ReadFile(userHooksPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read Cursor hooks.json: %w", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parse Cursor hooks.json: %w", err)
	}

	if !removeCursorThemistoHook(config) {
		return nil
	}

	return writeJSONAtomic(userHooksPath, config)
}

func ensureCursorHookScript(scriptPath string) error {
	dir := filepath.Dir(scriptPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create hook directory %s: %w", dir, err)
	}
	content := cursorHookPS1
	mode := os.FileMode(0644)
	if !isPowerShellScriptPath(scriptPath) {
		content = posixHookScript("-cursor-hook", nil)
		mode = 0755
	}
	return os.WriteFile(scriptPath, []byte(content), mode)
}

// validateCursorHookScript checks whether the installed agent binary supports the
// Cursor hook flag. PS1 scripts are Windows-only and assumed capable; shell scripts
// require the installed agent to accept -cursor-hook.
func validateCursorHookScript(scriptPath string) (bool, string) {
	if isPowerShellScriptPath(scriptPath) {
		return true, ""
	}
	supported, detail := agentBinarySupportsHookFlag("-cursor-hook")
	if supported {
		return true, ""
	}
	detail = strings.TrimSpace(detail)
	if detail != "" {
		detail = ": " + detail
	}
	return false, fmt.Sprintf("The Themisto Cursor hook script exists, but the installed Themisto agent at %s does not support Cursor hook execution yet%s", agentBinaryPath, detail)
}

func buildWindowsCursorHookCommand(scriptPath string) string {
	psPath := resolveAbsolutePowerShellPath()
	// Cursor's Windows hook runner is more reliable when command hooks are
	// launched through cmd.exe instead of invoking PowerShell directly.
	return fmt.Sprintf(`cmd /c %s -ExecutionPolicy Bypass -NoProfile -File "%s"`, psPath, scriptPath)
}

func buildCursorHookCommand(scriptPath string) string {
	if runtime.GOOS == "windows" {
		return buildWindowsCursorHookCommand(scriptPath)
	}
	return scriptPath
}

func upsertCursorThemistoHook(config map[string]interface{}, command string) {
	if _, ok := config["version"]; !ok {
		config["version"] = 1
	}

	hooks, ok := config["hooks"].(map[string]interface{})
	if !ok {
		hooks = make(map[string]interface{})
		config["hooks"] = hooks
	}

	existing, _ := hooks["beforeSubmitPrompt"].([]interface{})
	cleaned := make([]interface{}, 0, len(existing))
	for _, entry := range existing {
		if !cursorEntryContainsThemisto(entry) {
			cleaned = append(cleaned, entry)
		}
	}

	cleaned = append(cleaned, map[string]interface{}{
		"type":       "command",
		"command":    command,
		"timeout":    30,
		"failClosed": false,
	})
	hooks["beforeSubmitPrompt"] = cleaned
}

func removeCursorThemistoHook(config map[string]interface{}) bool {
	hooks, ok := config["hooks"].(map[string]interface{})
	if !ok {
		return false
	}

	existing, ok := hooks["beforeSubmitPrompt"].([]interface{})
	if !ok {
		return false
	}

	cleaned := make([]interface{}, 0, len(existing))
	for _, entry := range existing {
		if !cursorEntryContainsThemisto(entry) {
			cleaned = append(cleaned, entry)
		}
	}

	if len(cleaned) == 0 {
		delete(hooks, "beforeSubmitPrompt")
	} else {
		hooks["beforeSubmitPrompt"] = cleaned
	}

	if len(hooks) == 0 {
		delete(config, "hooks")
	}

	return true
}

const cursorHookPS1 = `# Themisto Cursor Hook - beforeSubmitPrompt
# Evaluates prompts via the local Themisto agent before Cursor processes them.
# Stdout must be JSON: {"continue":true|false}
# Logs to %LOCALAPPDATA%\Themisto\logs\cursor-hook.log for diagnostics.

$script:LogPath = $null
function Initialize-HookLog {
    try {
        $logDir = Join-Path $env:LOCALAPPDATA 'Themisto\logs'
        if (-not (Test-Path $logDir)) { New-Item -ItemType Directory -Path $logDir -Force | Out-Null }
        $script:LogPath = Join-Path $logDir 'cursor-hook.log'
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
function Write-CursorResult($continue, $userMessage) {
    $result = @{ continue = $continue }
    if ($userMessage) { $result['user_message'] = $userMessage }
    $out = $result | ConvertTo-Json -Compress
    [Console]::Out.WriteLine($out)
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
        Write-CursorResult $true
        exit 0
    }

    $inputJson = $readTask.Result
    $reader.Dispose()

    $stdinLen = if ($inputJson) { $inputJson.Length } else { 0 }
    Write-HookLog "stdin received  bytes=$stdinLen"

    if (-not $inputJson) {
        Write-HookLog "empty stdin -- allow"
        Write-CursorResult $true
        exit 0
    }

    $payload = $inputJson | ConvertFrom-Json
    $promptText = $payload.prompt
    if (-not $promptText) {
        Write-HookLog "no prompt field in payload -- allow"
        Write-CursorResult $true
        exit 0
    }

    Write-HookLog "prompt parsed  len=$($promptText.Length)"

    $metadata = @{
        conversation_id = [string]$payload.conversation_id
        generation_id   = [string]$payload.generation_id
        model           = [string]$payload.model
        hook_event      = [string]$payload.hook_event_name
        cursor_version  = [string]$payload.cursor_version
        user_email      = [string]$payload.user_email
        transcript_path = [string]$payload.transcript_path
    }
    if ($payload.attachments -ne $null) {
        $metadata.attachments_json = ($payload.attachments | ConvertTo-Json -Depth 6 -Compress)
    }
    if ($payload.workspace_roots -ne $null) {
        $metadata.workspace_roots_json = ($payload.workspace_roots | ConvertTo-Json -Depth 6 -Compress)
    }

    $evalBody = @{
        prompt_text      = $promptText
        surface          = "cursor"
        app_name         = "Cursor"
        vendor           = "cursor"
        service_category = "ai_code"
        destination_host = "api2.cursor.sh"
        metadata         = $metadata
    } | ConvertTo-Json -Depth 6 -Compress

    Write-HookLog "calling evaluator  POST http://127.0.0.1:17175/v1/prompt/evaluate"
    $evalResult = Invoke-ThemistoAPI 'http://127.0.0.1:17175/v1/prompt/evaluate' $evalBody 10000

    if (-not $evalResult.Success) {
        Write-HookLog "evaluator error  status=$($evalResult.StatusCode)  error=$($evalResult.Error) -- allow"
        try {
            $outcomeBody = @{
                surface          = "cursor"
                outcome          = "degraded_fail_open"
                destination_host = "api2.cursor.sh"
                vendor           = "cursor"
                service_category = "ai_code"
                app_name         = "Cursor"
                error            = $evalResult.Error
            } | ConvertTo-Json -Depth 4 -Compress
            Invoke-ThemistoAPI 'http://127.0.0.1:17175/v1/prompt/outcome' $outcomeBody 3000 | Out-Null
        } catch { }
        Write-CursorResult $true
        exit 0
    }

    $evalResponse = $evalResult.Body | ConvertFrom-Json
    $decision = $evalResponse.decision
    $evaluationId = $evalResponse.evaluation_id
    Write-HookLog "evaluator responded  decision=$decision  evaluation_id=$evaluationId"

    if ($decision -eq 'block') {
        Write-HookLog "BLOCKED  continue=false"
        $evalMsg = $evalResponse.message
        $blockMessage = "Themisto blocked this prompt: sensitive data detected."
        if ($evalMsg) { $blockMessage = $evalMsg }
        $blockMessage += " Start a new chat -- this conversation may still show the blocked content in Cursor's local history. Themisto is cleaning up local storage automatically."
        try {
            $outcomeBody = @{
                evaluation_id = $evaluationId
                surface       = "cursor"
                outcome       = "blocked"
                app_name      = "Cursor"
            } | ConvertTo-Json -Depth 4 -Compress
            Invoke-ThemistoAPI 'http://127.0.0.1:17175/v1/prompt/outcome' $outcomeBody 3000 | Out-Null
        } catch { }
        Write-CursorResult $false $blockMessage
        exit 0
    } elseif ($decision -eq 'alert') {
        Write-HookLog "ALERT  evaluation_id=$evaluationId -- allow with warning  continue=true"
        $evalMsg = $evalResponse.message
        $alertMessage = "Themisto flagged this prompt for sensitive content. Review before sending."
        if ($evalMsg) { $alertMessage = $evalMsg }
        try {
            $outcomeBody = @{
                evaluation_id      = $evaluationId
                surface            = "cursor"
                outcome            = "allowed"
                app_name           = "Cursor"
                user_message_shown = $true
            } | ConvertTo-Json -Depth 4 -Compress
            Invoke-ThemistoAPI 'http://127.0.0.1:17175/v1/prompt/outcome' $outcomeBody 3000 | Out-Null
        } catch { }
        Write-CursorResult $true $alertMessage
        exit 0
    }

    Write-HookLog "ALLOWED  decision=$decision  continue=true"
    try {
        $outcomeBody = @{
            evaluation_id = $evaluationId
            surface       = "cursor"
            outcome       = "allowed"
            app_name      = "Cursor"
        } | ConvertTo-Json -Depth 4 -Compress
        Invoke-ThemistoAPI 'http://127.0.0.1:17175/v1/prompt/outcome' $outcomeBody 3000 | Out-Null
    } catch { }

    Write-CursorResult $true
    exit 0

} catch {
    Write-HookLog "FATAL  error=$($_.Exception.Message) -- allow"
    Write-CursorResult $true
    exit 0
}
`
