package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDetectClaudeCodeHookPresent(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"UserPromptSubmit": []interface{}{
				map[string]interface{}{
					"matcher": "",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe -ExecutionPolicy Bypass -NoProfile -File "C:\Users\test\AppData\Local\Themisto\hooks\claude-code-hook.ps1"`,
							"timeout": 30,
						},
					},
				},
			},
		},
	}
	writeTestJSON(t, settingsPath, settings)

	present, err := readClaudeCodeHookStatus(settingsPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !present {
		t.Error("expected hook to be present")
	}
}

func TestDetectClaudeCodeHookAbsent(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")

	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Bash",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "some-other-script.sh",
						},
					},
				},
			},
		},
	}
	writeTestJSON(t, settingsPath, settings)

	present, err := readClaudeCodeHookStatus(settingsPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if present {
		t.Error("expected hook to be absent")
	}
}

func TestDetectClaudeCodeNoSettingsFile(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "nonexistent.json")

	present, err := readClaudeCodeHookStatus(settingsPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if present {
		t.Error("expected hook to be absent when file does not exist")
	}
}

func TestInstallClaudeCodeHookFresh(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	scriptPath := claudeCodeTestScriptPath(dir)

	if err := installClaudeCodeHookAt(settingsPath, scriptPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	// Verify settings.json was created with hook.
	present, err := readClaudeCodeHookStatus(settingsPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !present {
		t.Error("expected hook to be present after install")
	}

	// Verify script was written.
	if _, err := os.Stat(scriptPath); err != nil {
		t.Errorf("hook script not found: %v", err)
	}

	// Verify script content.
	content, _ := os.ReadFile(scriptPath)
	if runtime.GOOS == "windows" {
		if !strings.Contains(string(content), "Themisto Claude Code Hook") {
			t.Error("hook script does not contain expected header")
		}
	} else {
		if !strings.Contains(string(content), "#!/bin/sh") {
			t.Error("hook script does not contain expected shell header")
		}
		if !strings.Contains(string(content), claudeCodeHookAgentFlag) {
			t.Errorf("hook script does not invoke %s", claudeCodeHookAgentFlag)
		}
	}
}

func TestInstallClaudeCodeHookMerge(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	scriptPath := claudeCodeTestScriptPath(dir)

	// Pre-existing settings with other hooks and config.
	existing := map[string]interface{}{
		"theme": "dark",
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Bash",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "my-bash-validator.sh",
						},
					},
				},
			},
			"UserPromptSubmit": []interface{}{
				map[string]interface{}{
					"matcher": ".*",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "my-other-hook.sh",
						},
					},
				},
			},
		},
	}
	writeTestJSON(t, settingsPath, existing)

	if err := installClaudeCodeHookAt(settingsPath, scriptPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	// Reload and verify.
	data, _ := os.ReadFile(settingsPath)
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Theme preserved.
	if result["theme"] != "dark" {
		t.Error("theme was not preserved")
	}

	// PreToolUse preserved.
	hooks := result["hooks"].(map[string]interface{})
	if hooks["PreToolUse"] == nil {
		t.Error("PreToolUse was not preserved")
	}

	// UserPromptSubmit has both entries.
	ups := hooks["UserPromptSubmit"].([]interface{})
	if len(ups) != 2 {
		t.Errorf("expected 2 UserPromptSubmit entries, got %d", len(ups))
	}

	// Themisto hook present.
	present, _ := readClaudeCodeHookStatus(settingsPath)
	if !present {
		t.Error("Themisto hook not found after merge install")
	}
}

func TestInstallClaudeCodeHookIdempotent(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	scriptPath := claudeCodeTestScriptPath(dir)

	// Install twice.
	if err := installClaudeCodeHookAt(settingsPath, scriptPath); err != nil {
		t.Fatalf("first install failed: %v", err)
	}
	if err := installClaudeCodeHookAt(settingsPath, scriptPath); err != nil {
		t.Fatalf("second install failed: %v", err)
	}

	// Should have exactly one Themisto entry.
	data, _ := os.ReadFile(settingsPath)
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	hooks := result["hooks"].(map[string]interface{})
	ups := hooks["UserPromptSubmit"].([]interface{})

	themistoCount := 0
	for _, entry := range ups {
		if entryContainsThemistoHook(entry) {
			themistoCount++
		}
	}
	if themistoCount != 1 {
		t.Errorf("expected exactly 1 Themisto entry, got %d", themistoCount)
	}
}

func TestRemoveClaudeCodeHook(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	scriptPath := claudeCodeTestScriptPath(dir)

	// Install then remove.
	if err := installClaudeCodeHookAt(settingsPath, scriptPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if err := removeClaudeCodeHookAt(settingsPath); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	present, _ := readClaudeCodeHookStatus(settingsPath)
	if present {
		t.Error("expected hook to be absent after removal")
	}
}

func TestRemoveClaudeCodeHookPreservesOthers(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	scriptPath := claudeCodeTestScriptPath(dir)

	// Install Themisto hook alongside another.
	existing := map[string]interface{}{
		"hooks": map[string]interface{}{
			"UserPromptSubmit": []interface{}{
				map[string]interface{}{
					"matcher": ".*",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "some-other-hook.sh",
						},
					},
				},
			},
		},
	}
	writeTestJSON(t, settingsPath, existing)

	if err := installClaudeCodeHookAt(settingsPath, scriptPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if err := removeClaudeCodeHookAt(settingsPath); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	// Other hook should still be there.
	data, _ := os.ReadFile(settingsPath)
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	hooks := result["hooks"].(map[string]interface{})
	ups := hooks["UserPromptSubmit"].([]interface{})
	if len(ups) != 1 {
		t.Errorf("expected 1 remaining entry, got %d", len(ups))
	}
}

func TestEnsureHookScript(t *testing.T) {
	dir := t.TempDir()
	scriptPath := claudeCodeTestScriptPath(dir)

	if err := ensureHookScript(scriptPath); err != nil {
		t.Fatalf("ensure hook script failed: %v", err)
	}

	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("could not read script: %v", err)
	}
	s := string(content)

	required := map[string]string{}
	if runtime.GOOS == "windows" {
		required = map[string]string{
			"Themisto Claude Code Hook": "expected header",
			"/v1/prompt/evaluate":       "evaluate endpoint",
			"/v1/prompt/outcome":        "outcome endpoint",
			"exit 2":                    "exit 2 for block",
			"exit 0":                    "exit 0 for allow",
			"OpenStandardInput":         "UTF-8 stdin via OpenStandardInput",
			"Write-HookLog":             "debug logging function",
			"claude-code-hook.log":      "log file path",
			"ReadToEndAsync":            "async stdin read with timeout",
			"fail-open":                 "fail-open documentation",
		}
	} else {
		required = map[string]string{
			"#!/bin/sh":             "shell shebang",
			"set -eu":               "strict shell mode",
			agentBinaryPath:         "agent binary path",
			claudeCodeHookAgentFlag: "Claude Code hook flag",
		}
	}
	for marker, desc := range required {
		if !strings.Contains(s, marker) {
			t.Errorf("script missing %s (%s)", marker, desc)
		}
	}
}

func TestCheckHooksDisabled(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user.json")
	managedPath := filepath.Join(dir, "managed.json")

	// Managed settings disable all hooks.
	writeTestJSON(t, managedPath, map[string]interface{}{
		"disableAllHooks": true,
	})
	writeTestJSON(t, userPath, map[string]interface{}{})

	info := checkHooksDisabledOrOverridden(userPath, managedPath)
	if !info.HooksDisabled {
		t.Error("expected HooksDisabled to be true")
	}
}

func TestCheckManagedOverride(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user.json")
	managedPath := filepath.Join(dir, "managed.json")

	writeTestJSON(t, managedPath, map[string]interface{}{
		"allowManagedHooksOnly": true,
	})
	writeTestJSON(t, userPath, map[string]interface{}{})

	info := checkHooksDisabledOrOverridden(userPath, managedPath)
	if !info.ManagedOverride {
		t.Error("expected ManagedOverride to be true")
	}
}

func TestCheckUserDisableAllHooks(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user.json")
	managedPath := filepath.Join(dir, "managed.json") // does not exist

	writeTestJSON(t, userPath, map[string]interface{}{
		"disableAllHooks": true,
	})

	info := checkHooksDisabledOrOverridden(userPath, managedPath)
	if !info.HooksDisabled {
		t.Error("expected HooksDisabled from user settings")
	}
}

func TestHookPresentButDisabled(t *testing.T) {
	dir := t.TempDir()
	userSettingsPath := filepath.Join(dir, "settings.json")
	managedSettingsPath := filepath.Join(dir, "managed.json")
	scriptPath := claudeCodeTestScriptPath(dir)

	// Install hook.
	if err := installClaudeCodeHookAt(userSettingsPath, scriptPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	// Managed settings disable hooks.
	writeTestJSON(t, managedSettingsPath, map[string]interface{}{
		"disableAllHooks": true,
	})

	stubClaudeCodeAgentHookSupport(t, true, "")
	status := detectClaudeCodeIntegrationAt(userSettingsPath, managedSettingsPath, scriptPath)
	if status.IntegrationState != "hook_present_but_disabled" {
		t.Errorf("expected hook_present_but_disabled, got %s", status.IntegrationState)
	}
	if !status.HooksDisabled {
		t.Error("expected HooksDisabled to be true")
	}
	if !status.HookConfigured {
		t.Error("expected HookConfigured to be true")
	}
}

func TestDetectIntegrationConfiguredFromManagedSettings(t *testing.T) {
	dir := t.TempDir()
	userSettingsPath := filepath.Join(dir, "settings.json") // absent on purpose
	managedSettingsPath := filepath.Join(dir, "managed.json")
	scriptPath := claudeCodeTestScriptPath(dir)

	if err := ensureHookScript(scriptPath); err != nil {
		t.Fatalf("ensure hook script failed: %v", err)
	}

	writeTestJSON(t, managedSettingsPath, map[string]interface{}{
		"hooks": map[string]interface{}{
			"UserPromptSubmit": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe -ExecutionPolicy Bypass -NoProfile -File "C:\Users\test\AppData\Local\Themisto\hooks\claude-code-hook.ps1"`,
						},
					},
				},
			},
		},
	})

	stubClaudeCodeAgentHookSupport(t, true, "")
	status := detectClaudeCodeIntegrationAt(userSettingsPath, managedSettingsPath, scriptPath)
	if status.IntegrationState != "configured" {
		t.Errorf("expected configured, got %s", status.IntegrationState)
	}
	if status.HookSource != "managed" {
		t.Errorf("expected managed hook source, got %s", status.HookSource)
	}
	if !status.HookConfigured {
		t.Error("expected HookConfigured true")
	}
}

func TestManagedOnlyOverrideAllowsManagedHook(t *testing.T) {
	dir := t.TempDir()
	userSettingsPath := filepath.Join(dir, "settings.json")
	managedSettingsPath := filepath.Join(dir, "managed.json")
	scriptPath := claudeCodeTestScriptPath(dir)

	if err := installClaudeCodeHookAt(userSettingsPath, scriptPath); err != nil {
		t.Fatalf("install user hook failed: %v", err)
	}

	writeTestJSON(t, managedSettingsPath, map[string]interface{}{
		"allowManagedHooksOnly": true,
		"hooks": map[string]interface{}{
			"UserPromptSubmit": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe -ExecutionPolicy Bypass -NoProfile -File "C:\Users\test\AppData\Local\Themisto\hooks\claude-code-hook.ps1"`,
						},
					},
				},
			},
		},
	})

	stubClaudeCodeAgentHookSupport(t, true, "")
	status := detectClaudeCodeIntegrationAt(userSettingsPath, managedSettingsPath, scriptPath)
	if status.IntegrationState != "configured" {
		t.Errorf("expected configured when managed hook is present, got %s", status.IntegrationState)
	}
	if status.HookSource != "user_and_managed" {
		t.Errorf("expected user_and_managed hook source, got %s", status.HookSource)
	}
}

func TestDetectIntegrationConfigured(t *testing.T) {
	dir := t.TempDir()
	userSettingsPath := filepath.Join(dir, "settings.json")
	managedSettingsPath := filepath.Join(dir, "managed.json") // does not exist
	scriptPath := claudeCodeTestScriptPath(dir)

	if err := installClaudeCodeHookAt(userSettingsPath, scriptPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	stubClaudeCodeAgentHookSupport(t, true, "")
	status := detectClaudeCodeIntegrationAt(userSettingsPath, managedSettingsPath, scriptPath)
	if status.IntegrationState != "configured" {
		t.Errorf("expected configured, got %s", status.IntegrationState)
	}
	if !status.HookConfigured {
		t.Error("expected HookConfigured true")
	}
	if !status.HookScriptExists {
		t.Error("expected HookScriptExists true")
	}
}

func TestChoosePreferredClaudeCodeStatus(t *testing.T) {
	targets := []ClaudeCodeIntegrationStatus{
		{Target: "windows", DisplayName: "Windows", EnvironmentAvailable: true, IntegrationState: "missing_hook"},
		{Target: "wsl", DisplayName: "WSL", EnvironmentAvailable: true, CLIInstalled: true, HookConfigured: true, HookScriptExists: true, IntegrationState: "configured"},
	}

	preferred := choosePreferredClaudeCodeStatus(targets)
	if preferred.Target != "wsl" {
		t.Fatalf("expected WSL to be preferred, got %s", preferred.Target)
	}
}

func TestBuildWSLClaudeCodeHookCommand(t *testing.T) {
	fakeRun := func(args ...string) (string, error) {
		if len(args) == 4 && args[0] == "wslpath" {
			return "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe", nil
		}
		t.Fatalf("unexpected args: %v", args)
		return "", nil
	}

	command, err := buildWSLClaudeCodeHookCommand(fakeRun, `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, `C:\Users\demo\AppData\Local\Themisto\hooks\claude-code-hook.ps1`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(command, "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe") {
		t.Fatalf("expected WSL command to use translated PowerShell path, got %s", command)
	}
	if !strings.Contains(command, `C:\Users\demo\AppData\Local\Themisto\hooks\claude-code-hook.ps1`) {
		t.Fatalf("expected WSL command to keep Windows hook script path, got %s", command)
	}
}

func TestSanitizeWSLTextRemovesProxyNoise(t *testing.T) {
	raw := strings.Join([]string{
		"wsl: A localhost proxy configuration was detected but not mirrored into WSL.",
		"WSL in NAT mode does not support localhost proxies.",
		"real error text",
	}, "\n")

	got := sanitizeWSLText(raw)
	if got != "real error text" {
		t.Fatalf("expected proxy noise to be removed, got %q", got)
	}
}

func TestSanitizeWSLTextDropsNoiseOnlyOutput(t *testing.T) {
	raw := strings.Join([]string{
		"wsl: A localhost proxy configuration was detected but not mirrored into WSL.",
		"WSL in NAT mode does not support localhost proxies.",
	}, "\n")

	got := sanitizeWSLText(raw)
	if got != "" {
		t.Fatalf("expected proxy-only stderr to be dropped, got %q", got)
	}
}

func TestSanitizeWSLTextPreservesRealOutputAfterProxyNoise(t *testing.T) {
	raw := strings.Join([]string{
		"wsl: A localhost proxy configuration was detected but not mirrored into WSL.",
		"WSL in NAT mode does not support localhost proxies.",
		"/root/.claude/settings.json",
	}, "\n")

	got := sanitizeWSLText(raw)
	if got != "/root/.claude/settings.json" {
		t.Fatalf("expected real output to survive proxy noise filtering, got %q", got)
	}
}

func TestDetectIntegrationMissingScript(t *testing.T) {
	dir := t.TempDir()
	userSettingsPath := filepath.Join(dir, "settings.json")
	managedSettingsPath := filepath.Join(dir, "managed.json")
	scriptPath := claudeCodeTestScriptPath(dir)

	// Install, then delete the script.
	if err := installClaudeCodeHookAt(userSettingsPath, scriptPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	os.Remove(scriptPath)

	status := detectClaudeCodeIntegrationAt(userSettingsPath, managedSettingsPath, scriptPath)
	if status.IntegrationState != "missing_script" {
		t.Errorf("expected missing_script, got %s", status.IntegrationState)
	}
}

func TestRemoveClaudeCodeHookNonexistent(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "nonexistent.json")

	// Should not error on nonexistent file.
	if err := removeClaudeCodeHookAt(settingsPath); err != nil {
		t.Errorf("unexpected error removing from nonexistent file: %v", err)
	}
}

func TestDetectIntegrationMissingScriptWhenAgentLacksClaudeHookSupport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell hook validation only applies off Windows")
	}

	dir := t.TempDir()
	userSettingsPath := filepath.Join(dir, "settings.json")
	managedSettingsPath := filepath.Join(dir, "managed.json")
	scriptPath := claudeCodeTestScriptPath(dir)

	if err := installClaudeCodeHookAt(userSettingsPath, scriptPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	stubClaudeCodeAgentHookSupport(t, false, "flag provided but not defined")
	status := detectClaudeCodeIntegrationAt(userSettingsPath, managedSettingsPath, scriptPath)
	if status.IntegrationState != "missing_script" {
		t.Fatalf("expected missing_script, got %s", status.IntegrationState)
	}
	if !strings.Contains(status.Detail, "does not support Claude Code hook execution") {
		t.Fatalf("unexpected detail: %s", status.Detail)
	}
}

func writeTestJSON(t *testing.T, path string, data interface{}) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func claudeCodeTestScriptPath(root string) string {
	name := "claude-code-hook.sh"
	if runtime.GOOS == "windows" {
		name = "claude-code-hook.ps1"
	}
	return filepath.Join(root, "hooks", name)
}

func stubClaudeCodeAgentHookSupport(t *testing.T, supported bool, detail string) {
	t.Helper()

	agentHookSupportMu.Lock()
	oldFn := agentHookSupportFn
	oldCache := agentHookSupportCache
	agentHookSupportFn = func(flag string) (bool, string) {
		if flag != claudeCodeHookAgentFlag {
			return oldFn(flag)
		}
		return supported, detail
	}
	agentHookSupportCache = map[string]agentHookSupportResult{}
	agentHookSupportMu.Unlock()

	t.Cleanup(func() {
		agentHookSupportMu.Lock()
		agentHookSupportFn = oldFn
		agentHookSupportCache = oldCache
		agentHookSupportMu.Unlock()
	})
}
