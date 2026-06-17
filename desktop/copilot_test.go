package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGitHubCopilotDetectNotInstalled(t *testing.T) {
	dir := t.TempDir()
	status := detectGitHubCopilotIntegrationWithInstallDetector(
		filepath.Join(dir, "Code", "User", "settings.json"),
		filepath.Join(dir, "hooks", "copilot"),
		filepath.Join(dir, "hooks", "copilot", gitHubCopilotTestScriptName()),
		filepath.Join(dir, "hooks", "copilot", copilotHookConfigMarker),
		func() (bool, string) { return false, "" },
	)
	if status.IntegrationState != "not_installed" {
		t.Fatalf("expected not_installed, got %s", status.IntegrationState)
	}
}

func TestGitHubCopilotInstallCreatesSettingsAndAssets(t *testing.T) {
	home := t.TempDir()
	configRoot := home
	hooksRoot := home
	wantLocation := "~/AppData/Local/Themisto/hooks/copilot"
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
		configRoot = home
		hooksRoot = filepath.Join(home, "AppData", "Local")
	} else {
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", "")
		t.Setenv("APPDATA", "")
		t.Setenv("LOCALAPPDATA", "")
		configRoot = filepath.Join(home, "Library", "Application Support")
		hooksRoot = configRoot
		wantLocation = "~/Library/Application Support/Themisto/hooks/copilot"
	}

	settingsPath := filepath.Join(configRoot, "Code", "User", "settings.json")
	hooksDir := filepath.Join(hooksRoot, "Themisto", "hooks", "copilot")
	scriptPath := filepath.Join(hooksDir, gitHubCopilotTestScriptName())
	configPath := filepath.Join(hooksDir, copilotHookConfigMarker)

	if err := installGitHubCopilotHookAt(settingsPath, hooksDir, scriptPath, configPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	settings, exists, err := readVSCodeSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if !exists {
		t.Fatal("expected settings.json to exist")
	}
	if !settingsContainGitHubCopilotHook(settings, hooksDir) {
		t.Fatal("expected settings to contain Themisto Copilot hook dir")
	}
	locations := settings[gitHubCopilotHookDirSetting].(map[string]interface{})
	if _, ok := locations[wantLocation]; !ok {
		t.Fatalf("expected Copilot hook location to be stored as %s, got %#v", wantLocation, locations)
	}
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("expected script on disk: %v", err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected hook config on disk: %v", err)
	}

	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	if runtime.GOOS == "windows" {
		if !strings.Contains(string(content), "Themisto GitHub Copilot Hook") {
			t.Fatalf("expected PowerShell hook content, got:\n%s", string(content))
		}
	} else {
		if !strings.Contains(string(content), "#!/bin/sh") {
			t.Fatalf("expected shell wrapper, got:\n%s", string(content))
		}
		if !strings.Contains(string(content), copilotHookAgentFlag) {
			t.Fatalf("expected wrapper to invoke %s, got:\n%s", copilotHookAgentFlag, string(content))
		}
		if !strings.Contains(string(content), "THEMISTO_COPILOT_STATE_DIR") {
			t.Fatalf("expected wrapper to export Copilot state dir, got:\n%s", string(content))
		}
	}

	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	hooks, ok := config["hooks"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected hooks object in config")
	}
	for _, event := range []string{"UserPromptSubmit", "PreToolUse"} {
		entries, ok := hooks[event].([]interface{})
		if !ok || len(entries) != 1 {
			t.Fatalf("expected one %s entry, got %#v", event, hooks[event])
		}
		entry, ok := entries[0].(map[string]interface{})
		if !ok {
			t.Fatalf("expected %s entry to be an object", event)
		}
		command, _ := entry["command"].(string)
		if runtime.GOOS == "windows" {
			if !strings.Contains(strings.ToLower(command), "powershell") {
				t.Fatalf("expected Windows config command to use PowerShell, got %q", command)
			}
		} else {
			if command != scriptPath {
				t.Fatalf("expected macOS config command to point directly at the hook script, got %q", command)
			}
			if _, exists := entry["windows"]; exists {
				t.Fatalf("%s entry should not have a windows override on macOS: %#v", event, entry)
			}
		}
	}
}

func TestGitHubCopilotHookLocationSettingValueUsesTildePath(t *testing.T) {
	home := t.TempDir()
	hooksDir := filepath.Join(home, "AppData", "Local", "Themisto", "hooks", "copilot")
	want := "~/AppData/Local/Themisto/hooks/copilot"
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	} else {
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", "")
		hooksDir = filepath.Join(home, "Library", "Application Support", "Themisto", "hooks", "copilot")
		want = "~/Library/Application Support/Themisto/hooks/copilot"
	}
	got := gitHubCopilotHookLocationSettingValue(hooksDir)
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestGitHubCopilotInstallPreservesOtherSettings(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "Code", "User", "settings.json")
	hooksDir := filepath.Join(dir, "Themisto", "hooks", "copilot")
	scriptPath := filepath.Join(hooksDir, gitHubCopilotTestScriptName())
	configPath := filepath.Join(hooksDir, copilotHookConfigMarker)

	writeTestJSON(t, settingsPath, map[string]interface{}{
		"editor.fontSize": 16,
		gitHubCopilotHookDirSetting: map[string]interface{}{
			`C:\other\hooks`: true,
		},
	})

	if err := installGitHubCopilotHookAt(settingsPath, hooksDir, scriptPath, configPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	settings, _, err := readVSCodeSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if got := settings["editor.fontSize"]; got == nil {
		t.Fatal("expected unrelated setting to be preserved")
	}
	locations := settings[gitHubCopilotHookDirSetting].(map[string]interface{})
	if len(locations) != 2 {
		t.Fatalf("expected 2 hook locations, got %d", len(locations))
	}
}

func TestGitHubCopilotRemovePreservesOtherHookLocations(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "Code", "User", "settings.json")
	hooksDir := filepath.Join(dir, "Themisto", "hooks", "copilot")
	scriptPath := filepath.Join(hooksDir, gitHubCopilotTestScriptName())
	configPath := filepath.Join(hooksDir, copilotHookConfigMarker)

	if err := installGitHubCopilotHookAt(settingsPath, hooksDir, scriptPath, configPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	settings, _, err := readVSCodeSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	locations := settings[gitHubCopilotHookDirSetting].(map[string]interface{})
	locations[`C:\other\hooks`] = true
	settings[gitHubCopilotHookDirSetting] = locations
	if err := writeJSONAtomic(settingsPath, settings); err != nil {
		t.Fatalf("rewrite settings: %v", err)
	}

	if err := removeGitHubCopilotHookAt(settingsPath, hooksDir, scriptPath, configPath); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	updated, _, err := readVSCodeSettings(settingsPath)
	if err != nil {
		t.Fatalf("read updated settings: %v", err)
	}
	if settingsContainGitHubCopilotHook(updated, hooksDir) {
		t.Fatal("expected Themisto Copilot hook dir to be removed")
	}
	if updated[gitHubCopilotHookDirSetting] == nil {
		t.Fatal("expected other hook location to remain")
	}
}

func TestGitHubCopilotDetectConfigured(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "Code", "User", "settings.json")
	hooksDir := filepath.Join(dir, "Themisto", "hooks", "copilot")
	scriptPath := filepath.Join(hooksDir, gitHubCopilotTestScriptName())
	configPath := filepath.Join(hooksDir, copilotHookConfigMarker)

	if err := installGitHubCopilotHookAt(settingsPath, hooksDir, scriptPath, configPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	stubGitHubCopilotAgentHookSupport(t, true, "")
	status := detectGitHubCopilotIntegrationWithInstallDetector(
		settingsPath,
		hooksDir,
		scriptPath,
		configPath,
		func() (bool, string) { return true, `C:\Users\test\AppData\Local\Programs\Microsoft VS Code\Code.exe` },
	)
	if status.IntegrationState != "configured" {
		t.Fatalf("expected configured, got %s", status.IntegrationState)
	}
	if !status.HookConfigured || !status.HookScriptExists {
		t.Fatal("expected configured assets to be detected")
	}
}

func TestGitHubCopilotDetectMissingScriptWhenAgentLacksHookSupport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell hook validation only applies off Windows")
	}

	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "Code", "User", "settings.json")
	hooksDir := filepath.Join(dir, "hooks", "copilot")
	scriptPath := filepath.Join(hooksDir, gitHubCopilotTestScriptName())
	configPath := filepath.Join(hooksDir, copilotHookConfigMarker)

	if err := installGitHubCopilotHookAt(settingsPath, hooksDir, scriptPath, configPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	stubGitHubCopilotAgentHookSupport(t, false, "flag provided but not defined")
	status := detectGitHubCopilotIntegrationWithInstallDetector(
		settingsPath,
		hooksDir,
		scriptPath,
		configPath,
		func() (bool, string) { return true, "" },
	)
	if status.IntegrationState != "missing_script" {
		t.Fatalf("expected missing_script, got %s", status.IntegrationState)
	}
	if !strings.Contains(status.Detail, "does not support GitHub Copilot hook execution") {
		t.Fatalf("unexpected detail: %s", status.Detail)
	}
}

func TestReadVSCodeSettingsAcceptsJSONC(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "Code", "User", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "{\n  // comment\n  \"editor.fontSize\": 16,\n  \"chat.hookFilesLocations\": {\n    \"C:/tmp/hooks\": true,\n  },\n}\n"
	if err := os.WriteFile(settingsPath, []byte(content), 0644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	settings, exists, err := readVSCodeSettings(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if !exists {
		t.Fatal("expected settings to exist")
	}
	if settings["editor.fontSize"] == nil {
		t.Fatal("expected JSONC setting to parse")
	}
}

func gitHubCopilotTestScriptName() string {
	if runtime.GOOS == "windows" {
		return copilotHookScriptMarker
	}
	return copilotHookScriptMarkerUnix
}

func stubGitHubCopilotAgentHookSupport(t *testing.T, supported bool, detail string) {
	t.Helper()

	agentHookSupportMu.Lock()
	oldFn := agentHookSupportFn
	oldCache := agentHookSupportCache
	agentHookSupportFn = func(flag string) (bool, string) {
		if flag != copilotHookAgentFlag {
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
