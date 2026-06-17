package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	pathpkg "path"
	"runtime"
	"strings"
)

type wslInvoker func(args ...string) (string, error)

func defaultWSLInvoker(args ...string) (string, error) {
	cmd := newHiddenCommand("wsl.exe", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	text := sanitizeWSLText(strings.TrimSpace(stdout.String()))
	stderrText := sanitizeWSLText(strings.TrimSpace(stderr.String()))
	if err != nil {
		switch {
		case stderrText != "":
			return "", fmt.Errorf("%w: %s", err, stderrText)
		case text != "":
			return "", fmt.Errorf("%w: %s", err, text)
		}
		return "", err
	}
	return text, nil
}

func sanitizeWSLText(raw string) string {
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "localhost proxy configuration was detected but not mirrored into WSL") {
			continue
		}
		if strings.Contains(line, "WSL in NAT mode does not support localhost proxies") {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

func detectWSLClaudeCodeIntegration() ClaudeCodeIntegrationStatus {
	status := ClaudeCodeIntegrationStatus{
		Target:           "wsl",
		DisplayName:      "WSL",
		Confidence:       "high",
		ActionsSupported: true,
	}
	if runtime.GOOS != "windows" {
		status.IntegrationState = "not_available"
		status.Detail = "WSL integration is only relevant when the Themisto desktop app is running on Windows."
		return status
	}
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		status.IntegrationState = "not_available"
		status.Detail = "WSL was not detected on this Windows machine."
		return status
	}

	home, err := wslHomeDir(defaultWSLInvoker)
	if err != nil {
		status.IntegrationState = "not_available"
		status.Detail = "WSL was detected, but Themisto could not access the default WSL environment."
		status.Remediation = "Open WSL once, make sure your default distro is available, then refresh."
		status.LastError = err.Error()
		return status
	}

	status.EnvironmentAvailable = true
	status.SettingsPath = pathpkg.Join(home, ".claude", "settings.json")
	status.ManagedSettingsPath = "/etc/claude-code/managed-settings.json"

	cliInstalled, cliPath := detectWSLClaudeCodeCLI(defaultWSLInvoker)
	status.CLIInstalled = cliInstalled
	status.CLIPath = cliPath

	if !cliInstalled {
		settingsDir := pathpkg.Dir(status.SettingsPath)
		if !wslPathExists(defaultWSLInvoker, settingsDir, true) {
			status.IntegrationState = "not_installed"
			status.Detail = "Claude Code was not detected inside the default WSL environment."
			status.Remediation = "Install Claude Code in WSL to enable prompt governance there."
			return status
		}
		status.Confidence = "medium"
	}

	userHookPresent, err := readClaudeCodeHookStatusWSL(defaultWSLInvoker, status.SettingsPath)
	if err != nil {
		status.IntegrationState = "check_failed"
		status.Detail = "Could not read the WSL Claude Code user settings to check hook configuration."
		status.Remediation = "Verify that " + status.SettingsPath + " is readable inside WSL."
		status.LastError = err.Error()
		return status
	}

	managedHookPresent, err := readClaudeCodeHookStatusWSL(defaultWSLInvoker, status.ManagedSettingsPath)
	if err != nil {
		status.IntegrationState = "check_failed"
		status.Detail = "Could not read the WSL Claude Code managed settings to check hook configuration."
		status.Remediation = "Verify that " + status.ManagedSettingsPath + " is readable inside WSL."
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

	status.HookConfigured = userHookPresent || managedHookPresent
	_, scriptErr := os.Stat(claudeCodeHookScriptPath())
	status.HookScriptExists = scriptErr == nil

	if status.HookConfigured {
		if _, err := buildWSLClaudeCodeHookCommand(defaultWSLInvoker, resolveAbsolutePowerShellPath(), claudeCodeHookScriptPath()); err != nil {
			status.IntegrationState = "check_failed"
			status.Detail = "Themisto found WSL Claude Code settings, but could not prepare the Windows-to-WSL hook bridge."
			status.Remediation = "Verify that WSL can access Windows executables and mounted Windows paths, then refresh."
			status.LastError = err.Error()
			return status
		}
	}

	overrides := checkHooksDisabledOrOverriddenWSL(defaultWSLInvoker, status.SettingsPath, status.ManagedSettingsPath)
	status.HooksDisabled = overrides.HooksDisabled
	status.ManagedOverride = overrides.ManagedOverride

	switch {
	case status.HookConfigured && overrides.HooksDisabled:
		status.IntegrationState = "hook_present_but_disabled"
		status.Detail = "The Themisto hook is present in WSL Claude Code settings, but Claude Code hooks are globally disabled."
		status.Remediation = "Remove disableAllHooks from Claude Code user or managed settings inside WSL."
	case userHookPresent && overrides.ManagedOverride && !managedHookPresent:
		status.IntegrationState = "hook_present_but_disabled"
		status.Detail = "The Themisto hook is configured in WSL user settings, but managed settings only allow managed hooks."
		status.Remediation = "Contact IT to deploy the Themisto hook through WSL Claude Code managed settings, or remove the managed-only restriction."
	case status.HookConfigured && status.HookScriptExists:
		status.IntegrationState = "configured"
		switch status.HookSource {
		case "managed":
			status.Detail = "Themisto found a Claude Code hook in WSL managed settings and the Windows hook script exists."
		case "user_and_managed":
			status.Detail = "Themisto found Claude Code hooks in both WSL user and managed settings, and the Windows hook script exists."
		default:
			status.Detail = "Themisto found a Claude Code hook in WSL user settings and the Windows hook script exists."
		}
		status.Remediation = "WSL Claude Code still snapshots hooks at startup. If you recently installed or changed the hook, restart Claude Code in WSL or review the change from /hooks before expecting it to run."
	case status.HookConfigured && !status.HookScriptExists:
		status.IntegrationState = "missing_script"
		status.Detail = "The WSL Claude Code hook entry is configured, but the Windows Themisto hook script is missing from disk."
		status.Remediation = "Use Repair to reinstall the hook script from the Windows desktop app."
	default:
		status.IntegrationState = "missing_hook"
		status.Confidence = "medium"
		status.Detail = "Themisto did not find a Claude Code hook in WSL user or managed settings. Project-level Claude Code settings inside repositories are not included in this device-wide check."
		status.Remediation = "Install the WSL hook here, or confirm whether your team is using project-level Claude Code settings inside the repo."
	}

	return status
}

func detectWSLClaudeCodeCLI(run wslInvoker) (bool, string) {
	out, err := wslShell(run, "command -v claude 2>/dev/null || which claude 2>/dev/null || true")
	if err != nil {
		return false, ""
	}
	out = strings.TrimSpace(out)
	if out != "" {
		return true, out
	}

	home, err := wslHomeDir(run)
	if err != nil {
		return false, ""
	}

	candidates := []string{
		pathpkg.Join(home, ".npm-global", "bin", "claude"),
		pathpkg.Join(home, ".local", "bin", "claude"),
		"/usr/local/bin/claude",
		"/usr/bin/claude",
	}
	for _, candidate := range candidates {
		if wslPathExists(run, candidate, false) {
			return true, candidate
		}
	}
	return false, ""
}

func readClaudeCodeHookStatusWSL(run wslInvoker, settingsPath string) (bool, error) {
	data, err := wslReadFile(run, settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read WSL Claude Code settings: %w", err)
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return false, fmt.Errorf("parse WSL Claude Code settings: %w", err)
	}
	return settingsContainThemistoHook(settings), nil
}

func checkHooksDisabledOrOverriddenWSL(run wslInvoker, userSettingsPath, managedSettingsPath string) hookOverrideInfo {
	info := hookOverrideInfo{}
	if managed, err := wslReadJSONMap(run, managedSettingsPath); err == nil {
		if v, ok := managed["disableAllHooks"].(bool); ok && v {
			info.HooksDisabled = true
		}
		if v, ok := managed["allowManagedHooksOnly"].(bool); ok && v {
			info.ManagedOverride = true
		}
	}
	if !info.HooksDisabled {
		if user, err := wslReadJSONMap(run, userSettingsPath); err == nil {
			if v, ok := user["disableAllHooks"].(bool); ok && v {
				info.HooksDisabled = true
			}
		}
	}
	return info
}

func installWSLClaudeCodeHook() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("WSL Claude Code integration can only be managed from Windows")
	}
	if err := ensureHookScript(claudeCodeHookScriptPath()); err != nil {
		return err
	}

	home, err := wslHomeDir(defaultWSLInvoker)
	if err != nil {
		return fmt.Errorf("discover WSL home: %w", err)
	}
	settingsPath := pathpkg.Join(home, ".claude", "settings.json")

	settings := make(map[string]interface{})
	if data, err := wslReadFile(defaultWSLInvoker, settingsPath); err == nil {
		if jsonErr := json.Unmarshal(data, &settings); jsonErr != nil {
			return fmt.Errorf("existing WSL Claude Code settings.json is invalid JSON: %w", jsonErr)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	command, err := buildWSLClaudeCodeHookCommand(defaultWSLInvoker, resolveAbsolutePowerShellPath(), claudeCodeHookScriptPath())
	if err != nil {
		return err
	}
	upsertThemistoHook(settings, command)
	return wslWriteJSONAtomic(defaultWSLInvoker, settingsPath, settings)
}

func repairWSLClaudeCodeHook() error {
	if err := removeWSLClaudeCodeHook(); err != nil {
		return fmt.Errorf("remove during WSL repair: %w", err)
	}
	return installWSLClaudeCodeHook()
}

func removeWSLClaudeCodeHook() error {
	home, err := wslHomeDir(defaultWSLInvoker)
	if err != nil {
		return fmt.Errorf("discover WSL home: %w", err)
	}
	settingsPath := pathpkg.Join(home, ".claude", "settings.json")

	data, err := wslReadFile(defaultWSLInvoker, settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("parse WSL Claude Code settings: %w", err)
	}
	if !removeThemistoHook(settings) {
		return nil
	}
	return wslWriteJSONAtomic(defaultWSLInvoker, settingsPath, settings)
}

func buildWSLClaudeCodeHookCommand(run wslInvoker, powerShellWindowsPath, scriptWindowsPath string) (string, error) {
	powerShellWSLPath, err := wslTranslateWindowsPath(run, powerShellWindowsPath)
	if err != nil {
		return "", fmt.Errorf("translate PowerShell path for WSL: %w", err)
	}
	return fmt.Sprintf("%s -ExecutionPolicy Bypass -NoProfile -File %s", shQuote(powerShellWSLPath), shQuote(scriptWindowsPath)), nil
}

func wslHomeDir(run wslInvoker) (string, error) {
	return wslShell(run, `printf %s "$HOME"`)
}

func wslPathExists(run wslInvoker, path string, dir bool) bool {
	testFlag := "-e"
	if dir {
		testFlag = "-d"
	}
	out, err := wslShell(run, fmt.Sprintf("if [ %s %s ]; then printf yes; fi", testFlag, shQuote(path)))
	return err == nil && strings.TrimSpace(out) == "yes"
}

func wslReadFile(run wslInvoker, path string) ([]byte, error) {
	if !wslPathExists(run, path, false) {
		return nil, os.ErrNotExist
	}
	out, err := wslShell(run, fmt.Sprintf("cat %s", shQuote(path)))
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

func wslReadJSONMap(run wslInvoker, path string) (map[string]interface{}, error) {
	data, err := wslReadFile(run, path)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func wslWriteJSONAtomic(run wslInvoker, path string, data interface{}) error {
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	out = append(out, '\n')
	return wslWriteFileAtomic(run, path, out, "0644")
}

func wslWriteFileAtomic(run wslInvoker, path string, data []byte, mode string) error {
	dir := pathpkg.Dir(path)
	tmp := path + ".tmp"
	encoded := base64.StdEncoding.EncodeToString(data)
	script := fmt.Sprintf(
		"mkdir -p %s && printf %%s %s | base64 -d > %s && chmod %s %s && mv %s %s",
		shQuote(dir),
		shQuote(encoded),
		shQuote(tmp),
		mode,
		shQuote(tmp),
		shQuote(tmp),
		shQuote(path),
	)
	_, err := wslShell(run, script)
	if err != nil {
		return fmt.Errorf("write WSL file %s: %w", path, err)
	}
	return nil
}

func wslTranslateWindowsPath(run wslInvoker, windowsPath string) (string, error) {
	if translated, err := run("wslpath", "-a", "-u", windowsPath); err == nil && strings.TrimSpace(translated) != "" {
		return strings.TrimSpace(translated), nil
	}
	normalized := strings.ReplaceAll(windowsPath, `\`, `/`)
	if len(normalized) >= 2 && normalized[1] == ':' {
		drive := strings.ToLower(normalized[:1])
		rest := strings.TrimPrefix(normalized[2:], "/")
		return "/mnt/" + drive + "/" + rest, nil
	}
	return "", fmt.Errorf("could not translate Windows path %q into WSL form", windowsPath)
}

func wslShell(run wslInvoker, script string) (string, error) {
	return run("sh", "-lc", script)
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
