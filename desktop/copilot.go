package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

type GitHubCopilotIntegrationStatus struct {
	Installed         bool   `json:"installed"`
	ActionsSupported  bool   `json:"actions_supported"`
	CLIPath           string `json:"cli_path,omitempty"`
	SettingsPath      string `json:"settings_path,omitempty"`
	HookDirectoryPath string `json:"hook_directory_path,omitempty"`
	HookConfigPath    string `json:"hook_config_path,omitempty"`
	HookConfigured    bool   `json:"hook_configured"`
	HookScriptExists  bool   `json:"hook_script_exists"`
	HookSource        string `json:"hook_source,omitempty"`
	IntegrationState  string `json:"integration_state"`
	Confidence        string `json:"confidence"`
	Detail            string `json:"detail"`
	Remediation       string `json:"remediation"`
	LastError         string `json:"last_error,omitempty"`
}

const (
	gitHubCopilotHookDirSetting = "chat.hookFilesLocations"
	copilotHookScriptMarker     = "copilot-hook.ps1"
	copilotHookScriptMarkerUnix = "copilot-hook.sh"
	copilotHookConfigMarker     = "themisto-copilot-hooks.json"
	copilotHookStateDirName     = "copilot-state"
	copilotHookAgentFlag        = "-copilot-hook"
)

var (
	copilotStatusMu       sync.Mutex
	copilotStatusCached   GitHubCopilotIntegrationStatus
	copilotStatusCachedAt time.Time
)

func gitHubCopilotVSCodeSettingsPath() string {
	return filepath.Join(userConfigRootDir(), "Code", "User", "settings.json")
}

func gitHubCopilotHooksDir() string {
	return filepath.Join(themistoHookDir(), "copilot")
}

func gitHubCopilotHookScriptPath() string {
	if runtime.GOOS != "windows" {
		return filepath.Join(gitHubCopilotHooksDir(), copilotHookScriptMarkerUnix)
	}
	return filepath.Join(gitHubCopilotHooksDir(), copilotHookScriptMarker)
}

func gitHubCopilotHookConfigPath() string {
	return filepath.Join(gitHubCopilotHooksDir(), copilotHookConfigMarker)
}

func gitHubCopilotStateDirPath() string {
	return filepath.Join(gitHubCopilotHooksDir(), copilotHookStateDirName)
}

func gitHubCopilotHookLocationSettingValue(hooksDir string) string {
	home := currentUserHomeDir()
	home = strings.TrimSpace(home)
	if home == "" {
		return filepath.ToSlash(hooksDir)
	}

	cleanHome := filepath.Clean(home)
	cleanHooks := filepath.Clean(hooksDir)
	if sameNormalizedPath(cleanHooks, cleanHome) {
		return "~"
	}
	if rel, err := filepath.Rel(cleanHome, cleanHooks); err == nil && rel != "." && rel != "" && !strings.HasPrefix(rel, "..") {
		return "~/" + filepath.ToSlash(rel)
	}
	return filepath.ToSlash(cleanHooks)
}

func detectGitHubCopilotInstalled() (installed bool, cliPath string) {
	if runtime.GOOS != "windows" {
		if p, err := exec.LookPath("code"); err == nil {
			return true, p
		}
		candidates := []string{
			"/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code",
			"/Applications/Visual Studio Code.app/Contents/MacOS/Electron",
			"/usr/local/bin/code",
			"/opt/homebrew/bin/code",
		}
		for _, candidate := range candidates {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return true, candidate
			}
		}
		settingsDir := filepath.Dir(gitHubCopilotVSCodeSettingsPath())
		if info, err := os.Stat(settingsDir); err == nil && info.IsDir() {
			return true, ""
		}
		return false, ""
	}
	if p, err := exec.LookPath("code"); err == nil {
		return true, p
	}
	if out, err := newHiddenCommand("where.exe", "code").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				return true, line
			}
		}
	}

	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		home := os.Getenv("USERPROFILE")
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		localAppData = filepath.Join(home, "AppData", "Local")
	}
	candidates := []string{
		filepath.Join(localAppData, "Programs", "Microsoft VS Code", "Code.exe"),
		filepath.Join(localAppData, "Programs", "VS Code", "Code.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft VS Code", "Code.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft VS Code", "Code.exe"),
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return true, candidate
		}
	}

	settingsDir := filepath.Dir(gitHubCopilotVSCodeSettingsPath())
	if info, err := os.Stat(settingsDir); err == nil && info.IsDir() {
		return true, ""
	}
	return false, ""
}

func detectGitHubCopilotIntegration() GitHubCopilotIntegrationStatus {
	status := detectGitHubCopilotIntegrationWithInstallDetector(
		gitHubCopilotVSCodeSettingsPath(),
		gitHubCopilotHooksDir(),
		gitHubCopilotHookScriptPath(),
		gitHubCopilotHookConfigPath(),
		detectGitHubCopilotInstalled,
	)
	copilotStatusMu.Lock()
	copilotStatusCached = status
	copilotStatusCachedAt = time.Now()
	copilotStatusMu.Unlock()
	return status
}

func cachedGitHubCopilotIntegrationForDiagnostics() GitHubCopilotIntegrationStatus {
	copilotStatusMu.Lock()
	cached := copilotStatusCached
	cachedAt := copilotStatusCachedAt
	copilotStatusMu.Unlock()
	if !cachedAt.IsZero() && time.Since(cachedAt) < 30*time.Second {
		return cached
	}
	return detectGitHubCopilotIntegration()
}

func detectGitHubCopilotIntegrationWithInstallDetector(settingsPath, hooksDir, scriptPath, hookConfigPath string, installDetector func() (bool, string)) GitHubCopilotIntegrationStatus {
	status := GitHubCopilotIntegrationStatus{
		SettingsPath:      settingsPath,
		HookDirectoryPath: hooksDir,
		HookConfigPath:    hookConfigPath,
		Confidence:        "high",
		ActionsSupported:  gitHubCopilotActionsSupported(),
	}

	installed, cliPath := installDetector()
	status.Installed = installed
	status.CLIPath = cliPath

	settings, settingsExist, err := readVSCodeSettings(settingsPath)
	if err != nil {
		status.IntegrationState = "check_failed"
		status.Detail = "Could not read VS Code user settings to check GitHub Copilot hook configuration."
		status.Remediation = "Verify that " + settingsPath + " is readable JSON/JSONC."
		status.LastError = err.Error()
		return status
	}

	status.HookConfigured = settingsContainGitHubCopilotHook(settings, hooksDir)
	status.HookSource = ""
	if status.HookConfigured {
		status.HookSource = "vscode_user_settings"
	}
	scriptSupportDetail := ""
	if _, err := os.Stat(scriptPath); err == nil {
		if _, err := os.Stat(hookConfigPath); err == nil {
			status.HookScriptExists = true
			if supported, detail := validateGitHubCopilotHookScript(scriptPath); !supported {
				status.HookScriptExists = false
				scriptSupportDetail = detail
			}
		}
	}

	if !status.Installed {
		settingsDir := filepath.Dir(settingsPath)
		if info, err := os.Stat(settingsDir); err == nil && info.IsDir() {
			status.Installed = true
			status.Confidence = "medium"
		}
	}

	if !status.Installed {
		status.IntegrationState = "not_installed"
		status.Detail = "VS Code stable was not detected on this machine, so Themisto could not configure GitHub Copilot hooks."
		if status.ActionsSupported {
			status.Remediation = "Install VS Code stable with GitHub Copilot to enable prompt monitoring and validated tool controls."
		} else {
			status.Remediation = "Install VS Code to continue building the macOS/Linux integration path. Themisto now reports the expected local VS Code and hook locations for this platform."
		}
		return status
	}

	switch {
	case status.HookConfigured && status.HookScriptExists:
		status.IntegrationState = "configured"
		status.Detail = "Themisto found the managed GitHub Copilot hook directory in VS Code user settings and the local hook assets exist. Prompt submission is audit-visible, and the current validated tool control applies to Copilot-issued run_in_terminal calls tied to a prompt Themisto already flagged in that same session."
		status.Remediation = "Restart VS Code if you recently changed the hook. Copilot prompt submission remains audit-only; the current validated tool control is narrower than full prompt blocking."
	case status.HookConfigured && !status.HookScriptExists:
		status.IntegrationState = "missing_script"
		if scriptSupportDetail != "" {
			status.Detail = scriptSupportDetail
			status.Remediation = "Upgrade or reinstall the local Themisto agent so the GitHub Copilot hook runner is available, then use Repair to recreate the local hook assets."
		} else {
			status.Detail = "VS Code is configured to load the Themisto GitHub Copilot hooks directory, but the managed hook assets are missing from disk."
			status.Remediation = "Use Repair to recreate the local GitHub Copilot hook assets."
		}
	case !status.HookConfigured && status.HookScriptExists:
		status.IntegrationState = "partial"
		status.Detail = "The Themisto GitHub Copilot hook assets exist on disk, but VS Code user settings do not currently load the managed hook directory."
		status.Remediation = "Use Install to add the Themisto Copilot hook directory to VS Code settings."
	default:
		status.IntegrationState = "missing_hook"
		status.Confidence = "medium"
		if settingsExist {
			status.Detail = "VS Code user settings are present, but Themisto did not find the managed GitHub Copilot hook directory."
		} else {
			status.Detail = "VS Code appears to be installed, but Themisto did not find a user settings entry for GitHub Copilot hooks yet."
		}
		if status.ActionsSupported {
			status.Remediation = "Click Install to enable GitHub Copilot prompt monitoring and validated tool controls in VS Code."
		} else {
			status.Remediation = "Themisto is now using the correct local VS Code and hook paths for this platform. Automatic hook installation still needs a native shell implementation before the desktop app can wire it automatically."
		}
	}

	return status
}

func gitHubCopilotActionsSupported() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "darwin"
}

func validateGitHubCopilotHookScript(scriptPath string) (bool, string) {
	if isPowerShellScriptPath(scriptPath) {
		return true, ""
	}
	supported, detail := agentBinarySupportsHookFlag(copilotHookAgentFlag)
	if supported {
		return true, ""
	}
	detail = strings.TrimSpace(detail)
	if detail != "" {
		detail = ": " + detail
	}
	return false, fmt.Sprintf("The Themisto GitHub Copilot hook script exists, but the installed Themisto agent at %s does not support GitHub Copilot hook execution yet%s", agentBinaryPath, detail)
}

func readVSCodeSettings(path string) (map[string]interface{}, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read VS Code settings: %w", err)
	}

	cleaned, err := stripJSONC(data)
	if err != nil {
		return nil, true, fmt.Errorf("prepare VS Code settings: %w", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(cleaned, &settings); err != nil {
		return nil, true, fmt.Errorf("parse VS Code settings: %w", err)
	}
	return settings, true, nil
}

func settingsContainGitHubCopilotHook(settings map[string]interface{}, hooksDir string) bool {
	if settings == nil {
		return false
	}
	raw, ok := settings[gitHubCopilotHookDirSetting]
	if !ok || raw == nil {
		return false
	}
	locations, ok := raw.(map[string]interface{})
	if !ok {
		return false
	}
	for key, value := range locations {
		enabled, ok := value.(bool)
		if !ok || !enabled {
			continue
		}
		if samePathToken(key, hooksDir) {
			return true
		}
	}
	return false
}

func installGitHubCopilotHook() error {
	if !gitHubCopilotActionsSupported() {
		return fmt.Errorf("GitHub Copilot hook installation is not wired on %s yet. Themisto now reports the correct VS Code and hook paths so macOS/Linux support can be built without Windows-specific assumptions", platformDisplayName(runtime.GOOS))
	}
	return installGitHubCopilotHookAt(
		gitHubCopilotVSCodeSettingsPath(),
		gitHubCopilotHooksDir(),
		gitHubCopilotHookScriptPath(),
		gitHubCopilotHookConfigPath(),
	)
}

func installGitHubCopilotHookAt(settingsPath, hooksDir, scriptPath, hookConfigPath string) error {
	if err := ensureGitHubCopilotHookScript(scriptPath); err != nil {
		return fmt.Errorf("write GitHub Copilot hook script: %w", err)
	}
	if err := ensureGitHubCopilotHookConfig(hookConfigPath, scriptPath); err != nil {
		return fmt.Errorf("write GitHub Copilot hooks config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return fmt.Errorf("create VS Code settings directory: %w", err)
	}

	settings, _, err := readVSCodeSettings(settingsPath)
	if err != nil {
		return err
	}
	if settings == nil {
		settings = make(map[string]interface{})
	}
	if err := upsertGitHubCopilotHookLocation(settings, hooksDir); err != nil {
		return err
	}
	return writeJSONAtomic(settingsPath, settings)
}

func repairGitHubCopilotHook() error {
	if !gitHubCopilotActionsSupported() {
		return fmt.Errorf("GitHub Copilot hook repair is not wired on %s yet. Themisto now reports the correct VS Code and hook paths so macOS/Linux support can be built without Windows-specific assumptions", platformDisplayName(runtime.GOOS))
	}
	return repairGitHubCopilotHookAt(
		gitHubCopilotVSCodeSettingsPath(),
		gitHubCopilotHooksDir(),
		gitHubCopilotHookScriptPath(),
		gitHubCopilotHookConfigPath(),
	)
}

func repairGitHubCopilotHookAt(settingsPath, hooksDir, scriptPath, hookConfigPath string) error {
	if err := removeGitHubCopilotHookAt(settingsPath, hooksDir, scriptPath, hookConfigPath); err != nil {
		return fmt.Errorf("remove during repair: %w", err)
	}
	return installGitHubCopilotHookAt(settingsPath, hooksDir, scriptPath, hookConfigPath)
}

func removeGitHubCopilotHook() error {
	return removeGitHubCopilotHookAt(
		gitHubCopilotVSCodeSettingsPath(),
		gitHubCopilotHooksDir(),
		gitHubCopilotHookScriptPath(),
		gitHubCopilotHookConfigPath(),
	)
}

func removeGitHubCopilotHookAt(settingsPath, hooksDir, scriptPath, hookConfigPath string) error {
	settings, exists, err := readVSCodeSettings(settingsPath)
	if err != nil {
		return err
	}
	if exists && settings != nil {
		if removeGitHubCopilotHookLocation(settings, hooksDir) {
			if err := writeJSONAtomic(settingsPath, settings); err != nil {
				return err
			}
		}
	}

	_ = os.Remove(hookConfigPath)
	_ = os.Remove(scriptPath)
	_ = os.RemoveAll(filepath.Join(hooksDir, copilotHookStateDirName))
	_ = os.Remove(hooksDir)
	return nil
}

func upsertGitHubCopilotHookLocation(settings map[string]interface{}, hooksDir string) error {
	hookLocation := gitHubCopilotHookLocationSettingValue(hooksDir)
	raw, ok := settings[gitHubCopilotHookDirSetting]
	if !ok || raw == nil {
		settings[gitHubCopilotHookDirSetting] = map[string]interface{}{hookLocation: true}
		return nil
	}
	locations, ok := raw.(map[string]interface{})
	if !ok {
		return fmt.Errorf("existing VS Code setting %q is not an object", gitHubCopilotHookDirSetting)
	}
	for key := range locations {
		if samePathToken(key, hooksDir) {
			locations[key] = true
			settings[gitHubCopilotHookDirSetting] = locations
			return nil
		}
	}
	locations[hookLocation] = true
	settings[gitHubCopilotHookDirSetting] = locations
	return nil
}

func removeGitHubCopilotHookLocation(settings map[string]interface{}, hooksDir string) bool {
	raw, ok := settings[gitHubCopilotHookDirSetting]
	if !ok || raw == nil {
		return false
	}
	locations, ok := raw.(map[string]interface{})
	if !ok {
		return false
	}
	removed := false
	for key := range locations {
		if samePathToken(key, hooksDir) {
			delete(locations, key)
			removed = true
		}
	}
	if len(locations) == 0 {
		delete(settings, gitHubCopilotHookDirSetting)
	} else {
		settings[gitHubCopilotHookDirSetting] = locations
	}
	return removed
}

func ensureGitHubCopilotHookScript(scriptPath string) error {
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0755); err != nil {
		return fmt.Errorf("create Copilot hook directory: %w", err)
	}
	content := gitHubCopilotHookPS1
	mode := os.FileMode(0644)
	if runtime.GOOS != "windows" {
		content = posixHookScript(copilotHookAgentFlag, map[string]string{
			"THEMISTO_COPILOT_STATE_DIR": filepath.Join(filepath.Dir(scriptPath), copilotHookStateDirName),
		})
		mode = 0755
	}
	return os.WriteFile(scriptPath, []byte(content), mode)
}

func ensureGitHubCopilotHookConfig(configPath, scriptPath string) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("create Copilot hook directory: %w", err)
	}
	command := buildGitHubCopilotHookCommand(scriptPath)
	userPromptSubmit := map[string]interface{}{
		"type":    "command",
		"command": command,
		"timeout": 15,
	}
	preToolUse := map[string]interface{}{
		"type":    "command",
		"command": command,
		"timeout": 15,
	}
	if runtime.GOOS == "windows" {
		userPromptSubmit["windows"] = command
		preToolUse["windows"] = command
	}
	config := map[string]interface{}{
		"hooks": map[string]interface{}{
			"UserPromptSubmit": []interface{}{
				userPromptSubmit,
			},
			"PreToolUse": []interface{}{
				preToolUse,
			},
		},
	}
	return writeJSONAtomic(configPath, config)
}

func buildGitHubCopilotHookCommand(scriptPath string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`powershell.exe -ExecutionPolicy Bypass -NoProfile -File "%s"`, scriptPath)
	}
	return scriptPath
}

func samePathToken(a, b string) bool {
	return sameNormalizedPath(a, b)
}

func sameNormalizedPath(a, b string) bool {
	normalize := func(v string) string {
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, "~") {
			home := currentUserHomeDir()
			if home != "" {
				if v == "~" {
					v = home
				} else if strings.HasPrefix(v, "~/") || strings.HasPrefix(v, `~\`) {
					suffix := strings.TrimPrefix(strings.TrimPrefix(v, "~/"), `~\`)
					v = filepath.Join(home, filepath.FromSlash(strings.ReplaceAll(suffix, `\`, `/`)))
				}
			}
		}
		v = filepath.Clean(v)
		if runtime.GOOS == "windows" {
			v = strings.ReplaceAll(v, "/", `\`)
			v = strings.ToLower(v)
		} else {
			v = filepath.ToSlash(v)
		}
		return v
	}
	return normalize(a) == normalize(b)
}

func stripJSONC(data []byte) ([]byte, error) {
	var out strings.Builder
	out.Grow(len(data))

	inString := false
	escape := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(data); i++ {
		ch := data[i]
		var next byte
		if i+1 < len(data) {
			next = data[i+1]
		}

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
				out.WriteByte(ch)
			}
			continue
		}
		if inBlockComment {
			if ch == '*' && next == '/' {
				inBlockComment = false
				i++
			}
			continue
		}
		if inString {
			out.WriteByte(ch)
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out.WriteByte(ch)
			continue
		}
		if ch == '/' && next == '/' {
			inLineComment = true
			i++
			continue
		}
		if ch == '/' && next == '*' {
			inBlockComment = true
			i++
			continue
		}
		out.WriteByte(ch)
	}

	withoutComments := out.String()
	reTrailingComma := regexp.MustCompile(`,(\s*[}\]])`)
	cleaned := reTrailingComma.ReplaceAllString(withoutComments, "$1")
	return []byte(cleaned), nil
}

const gitHubCopilotHookPS1 = `# Themisto GitHub Copilot Hook
# UserPromptSubmit is audit-only in Copilot. PreToolUse can deny validated tools.

$script:LogPath = $null
function Initialize-HookLog {
    try {
        $logDir = Join-Path $env:LOCALAPPDATA 'Themisto\logs'
        if (-not (Test-Path $logDir)) { New-Item -ItemType Directory -Path $logDir -Force | Out-Null }
        $script:LogPath = Join-Path $logDir 'copilot-hook.log'
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
function Get-SessionStateDir {
    $dir = Join-Path $PSScriptRoot 'copilot-state'
    if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Path $dir -Force | Out-Null }
    return $dir
}
function Get-SessionStatePath($sessionId) {
    $safe = [Regex]::Replace([string]$sessionId, '[^A-Za-z0-9._-]', '_')
    return Join-Path (Get-SessionStateDir) ($safe + '.json')
}
function Remove-SessionState($sessionId) {
    if (-not $sessionId) { return }
    try {
        $path = Get-SessionStatePath $sessionId
        if (Test-Path $path) { Remove-Item $path -Force -ErrorAction SilentlyContinue }
    } catch { }
}
function Write-SessionState($sessionId, $evaluationId, $reason) {
    if (-not $sessionId) { return }
    try {
        $state = @{
            session_id     = [string]$sessionId
            evaluation_id  = [string]$evaluationId
            reason         = [string]$reason
            created_at_utc = [DateTime]::UtcNow.ToString('o')
        } | ConvertTo-Json -Depth 4 -Compress
        [IO.File]::WriteAllText((Get-SessionStatePath $sessionId), $state)
    } catch { }
}
function Read-SessionState($sessionId) {
    if (-not $sessionId) { return $null }
    try {
        $path = Get-SessionStatePath $sessionId
        if (-not (Test-Path $path)) { return $null }
        $raw = [IO.File]::ReadAllText($path)
        if (-not $raw) { return $null }
        $state = $raw | ConvertFrom-Json
        if ($state.created_at_utc) {
            $created = [DateTime]::Parse([string]$state.created_at_utc).ToUniversalTime()
            if (([DateTime]::UtcNow - $created).TotalMinutes -gt 60) {
                Remove-SessionState $sessionId
                return $null
            }
        }
        return $state
    } catch {
        return $null
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
} catch {
    Write-HookLog "stdin read error: $($_.Exception.Message) -- allow"
    exit 0
}

$stdinLen = if ($inputJson) { $inputJson.Length } else { 0 }
Write-HookLog "stdin received  bytes=$stdinLen"
if (-not $inputJson) {
    Write-HookLog "empty stdin -- allow"
    exit 0
}

try {
    $payload = $inputJson | ConvertFrom-Json
} catch {
    Write-HookLog "invalid json -- allow"
    exit 0
}

$hookName = $null
if ($payload.hook_event_name) { $hookName = [string]$payload.hook_event_name }
elseif ($payload.hookEventName) { $hookName = [string]$payload.hookEventName }
$sessionId = $null
if ($payload.session_id) { $sessionId = [string]$payload.session_id }
elseif ($payload.sessionId) { $sessionId = [string]$payload.sessionId }

Write-HookLog "hook name: $hookName"

if ($hookName -eq 'UserPromptSubmit') {
    $promptText = [string]$payload.prompt
    if (-not $promptText) {
        Write-HookLog "no prompt field -- allow"
        Remove-SessionState $sessionId
        exit 0
    }
    Write-HookLog "EVENT: UserPromptSubmit  prompt_len=$($promptText.Length)"

    $metadata = @{
        hook_event_name = [string]$payload.hook_event_name
        session_id      = [string]$sessionId
        transcript_path = [string]$payload.transcript_path
        cwd             = [string]$payload.cwd
    }
    $evalBody = @{
        prompt_text      = $promptText
        surface          = "github_copilot"
        app_name         = "GitHub Copilot"
        vendor           = "github"
        service_category = "ai_code"
        destination_host = "api.githubcopilot.com"
        metadata         = $metadata
    } | ConvertTo-Json -Depth 6 -Compress

    Write-HookLog "calling evaluator  POST http://127.0.0.1:17175/v1/prompt/evaluate"
    $evalResult = Invoke-ThemistoAPI 'http://127.0.0.1:17175/v1/prompt/evaluate' $evalBody 10000
    if (-not $evalResult.Success) {
        Write-HookLog "evaluator error  status=$($evalResult.StatusCode)  error=$($evalResult.Error) -- audit fail-open"
        Remove-SessionState $sessionId
        try {
            $outcomeBody = @{
                surface          = "github_copilot"
                outcome          = "degraded_fail_open"
                destination_host = "api.githubcopilot.com"
                vendor           = "github"
                service_category = "ai_code"
                app_name         = "GitHub Copilot"
                user_message_shown = $false
                error            = $evalResult.Error
            } | ConvertTo-Json -Depth 6 -Compress
            Invoke-ThemistoAPI 'http://127.0.0.1:17175/v1/prompt/outcome' $outcomeBody 3000 | Out-Null
        } catch { }
        exit 0
    }

    $evalResponse = $evalResult.Body | ConvertFrom-Json
    $decision = [string]$evalResponse.decision
    $evaluationId = [string]$evalResponse.evaluation_id
    Write-HookLog "evaluator responded  decision=$decision  evaluation_id=$evaluationId"

    if ($decision -eq 'block') {
        Write-HookLog "WOULD_BLOCK_PROMPT_SUBMIT  audit_only=true"
        Write-SessionState $sessionId $evaluationId ([string]$evalResponse.message)
        try {
            $outcomeBody = @{
                evaluation_id     = $evaluationId
                surface           = "github_copilot"
                outcome           = "would_block"
                destination_host  = "api.githubcopilot.com"
                vendor            = "github"
                service_category  = "ai_code"
                app_name          = "GitHub Copilot"
                user_message_shown = $false
            } | ConvertTo-Json -Depth 6 -Compress
            Invoke-ThemistoAPI 'http://127.0.0.1:17175/v1/prompt/outcome' $outcomeBody 3000 | Out-Null
        } catch { }
        exit 0
    }

    Remove-SessionState $sessionId
    Write-HookLog "AUDITED_PROMPT  decision=$decision"
    try {
        $outcomeBody = @{
            evaluation_id     = $evaluationId
            surface           = "github_copilot"
            outcome           = "allowed"
            destination_host  = "api.githubcopilot.com"
            vendor            = "github"
            service_category  = "ai_code"
            app_name          = "GitHub Copilot"
            user_message_shown = $false
        } | ConvertTo-Json -Depth 6 -Compress
        Invoke-ThemistoAPI 'http://127.0.0.1:17175/v1/prompt/outcome' $outcomeBody 3000 | Out-Null
    } catch { }
    exit 0
}

if ($hookName -eq 'PreToolUse') {
    $toolName = [string]$payload.tool_name
    Write-HookLog "EVENT: PreToolUse  tool_name=$toolName"
    $sessionState = Read-SessionState $sessionId
    if ($sessionState -and $toolName -eq 'run_in_terminal') {
        $reason = [string]$sessionState.reason
        if (-not $reason) {
            $reason = 'A previous Copilot prompt matched Themisto policy, so terminal execution is denied for this session.'
        }
        Write-HookLog "DENYING_TOOL  tool_name=$toolName  session_id=$sessionId"
        $denyResponse = @{
            hookSpecificOutput = @{
                hookEventName = "PreToolUse"
                permissionDecision = "deny"
                permissionDecisionReason = $reason
            }
        } | ConvertTo-Json -Depth 4 -Compress
        [Console]::Out.WriteLine($denyResponse)
        exit 0
    }

    Write-HookLog "ALLOWING_TOOL  tool_name=$toolName"
    exit 0
}

Write-HookLog "UNKNOWN_HOOK  hook_name=$hookName -- allow"
exit 0
`
