package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDarwinClaudeCodeManagedSettingsPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-specific test")
	}

	const want = "/Library/Application Support/ClaudeCode/managed-settings.json"
	if got := claudeCodeManagedSettingsPath(); got != want {
		t.Fatalf("managed settings path = %q, want %q", got, want)
	}
	if !strings.Contains(claudeCodeManagedSettingsPath(), "Application Support") {
		t.Fatal("macOS managed settings path must live under Application Support so enterprise-deployed hooks are discoverable")
	}
}

func TestDarwinClaudeCodeHookUsesShellWrapper(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-specific test")
	}

	home := setDarwinTestHome(t)
	scriptPath := claudeCodeHookScriptPath()
	wantPath := filepath.Join(home, ".themisto", "hooks", "claude-code-hook.sh")
	if scriptPath != wantPath {
		t.Fatalf("script path = %q, want %q", scriptPath, wantPath)
	}

	if err := ensureHookScript(scriptPath); err != nil {
		t.Fatalf("ensure hook script: %v", err)
	}

	assertExecutableScript(t, scriptPath, []string{
		"#!/bin/sh",
		"exec '/usr/local/bin/themisto-agent' -claude-code-hook",
	})

	command := buildClaudeCodeHookCommand(scriptPath)
	if command != scriptPath {
		t.Fatalf("command = %q, want %q", command, scriptPath)
	}
	if strings.Contains(strings.ToLower(command), "powershell") {
		t.Fatalf("macOS command should not invoke PowerShell: %q", command)
	}
}

func TestDarwinCursorHookUsesShellWrapper(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-specific test")
	}

	home := setDarwinTestHome(t)
	scriptPath := cursorHookScriptPath()
	wantPath := filepath.Join(home, ".themisto", "hooks", "cursor-hook.sh")
	if scriptPath != wantPath {
		t.Fatalf("script path = %q, want %q", scriptPath, wantPath)
	}

	if err := ensureCursorHookScript(scriptPath); err != nil {
		t.Fatalf("ensure cursor hook script: %v", err)
	}

	assertExecutableScript(t, scriptPath, []string{
		"#!/bin/sh",
		"exec '/usr/local/bin/themisto-agent' -cursor-hook",
	})

	command := buildCursorHookCommand(scriptPath)
	if command != scriptPath {
		t.Fatalf("command = %q, want %q", command, scriptPath)
	}
	if strings.Contains(strings.ToLower(command), "powershell") {
		t.Fatalf("macOS command should not invoke PowerShell: %q", command)
	}
}

func TestDarwinGitHubCopilotInstallUsesMacPaths(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-specific test")
	}

	home := setDarwinTestHome(t)
	t.Setenv("APPDATA", "")
	t.Setenv("LOCALAPPDATA", "")

	settingsPath := gitHubCopilotVSCodeSettingsPath()
	hooksDir := gitHubCopilotHooksDir()
	scriptPath := gitHubCopilotHookScriptPath()
	configPath := gitHubCopilotHookConfigPath()

	if want := filepath.Join(home, "Library", "Application Support", "Code", "User", "settings.json"); settingsPath != want {
		t.Fatalf("settings path = %q, want %q", settingsPath, want)
	}
	if want := filepath.Join(home, ".themisto", "hooks", "copilot"); hooksDir != want {
		t.Fatalf("hooks dir = %q, want %q", hooksDir, want)
	}
	if want := filepath.Join(hooksDir, "copilot-hook.sh"); scriptPath != want {
		t.Fatalf("script path = %q, want %q", scriptPath, want)
	}

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

	locations, ok := settings[gitHubCopilotHookDirSetting].(map[string]interface{})
	if !ok {
		t.Fatalf("expected %q setting to be an object", gitHubCopilotHookDirSetting)
	}
	wantLocation := "~/.themisto/hooks/copilot"
	if value, ok := locations[wantLocation].(bool); !ok || !value {
		t.Fatalf("expected hook location %q to be enabled, got %#v", wantLocation, locations)
	}
	for key := range locations {
		if strings.Contains(key, "AppData") {
			t.Fatalf("macOS settings should not reference AppData paths: %#v", locations)
		}
	}

	assertExecutableScript(t, scriptPath, []string{
		"#!/bin/sh",
		"export THEMISTO_COPILOT_STATE_DIR='" + filepath.Join(hooksDir, copilotHookStateDirName) + "'",
		"exec '/usr/local/bin/themisto-agent' -copilot-hook",
	})

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
		if command, _ := entry["command"].(string); command != scriptPath {
			t.Fatalf("%s command = %q, want %q", event, command, scriptPath)
		}
		if _, exists := entry["windows"]; exists {
			t.Fatalf("%s entry should not have a windows override on macOS: %#v", event, entry)
		}
	}
}

func TestDarwinWindsurfInstallUsesShellWrapper(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-specific test")
	}

	home := setDarwinTestHome(t)
	userHooksPath := windsurfUserHooksPath()
	scriptPath := windsurfHookScriptPath()
	launcherPath := filepath.Join(home, ".themisto", "hooks", windsurfHookLauncherMarker)

	if want := filepath.Join(home, ".codeium", "windsurf", "hooks.json"); userHooksPath != want {
		t.Fatalf("hooks path = %q, want %q", userHooksPath, want)
	}
	if want := filepath.Join(home, ".themisto", "hooks", "windsurf-hook.sh"); scriptPath != want {
		t.Fatalf("script path = %q, want %q", scriptPath, want)
	}

	if err := installWindsurfHookAt(userHooksPath, scriptPath, launcherPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	config, exists, err := readWindsurfHooksConfig(userHooksPath)
	if err != nil {
		t.Fatalf("read hooks config: %v", err)
	}
	if !exists {
		t.Fatal("expected hooks.json to exist")
	}

	hooks, ok := config["hooks"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected hooks object in config")
	}
	entries, ok := hooks["pre_user_prompt"].([]interface{})
	if !ok || len(entries) != 1 {
		t.Fatalf("expected one pre_user_prompt entry, got %#v", hooks["pre_user_prompt"])
	}
	entry, ok := entries[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected hook entry to be an object")
	}
	if command, _ := entry["command"].(string); command != scriptPath {
		t.Fatalf("command = %q, want %q", command, scriptPath)
	}

	assertExecutableScript(t, scriptPath, []string{
		"#!/bin/sh",
		"exec '/usr/local/bin/themisto-agent' -windsurf-hook",
	})

	if _, err := os.Stat(filepath.Join(filepath.Dir(scriptPath), windsurfHookLauncherMarker)); !os.IsNotExist(err) {
		t.Fatalf("expected no Windows launcher on macOS, stat err = %v", err)
	}
}

func TestClaudeCodeDetectConfiguredRequiresHookCapableAgent(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	managedPath := filepath.Join(dir, "managed-settings.json")
	scriptPath := filepath.Join(dir, "hooks", "claude-code-hook.sh")

	if err := installClaudeCodeHookAt(settingsPath, scriptPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	stubAgentHookSupport(t, func(flag string) (bool, string) {
		if flag != "-claude-code-hook" {
			t.Fatalf("unexpected flag %q", flag)
		}
		return false, "flag provided but not defined"
	})

	status := detectClaudeCodeIntegrationAt(settingsPath, managedPath, scriptPath)
	if status.IntegrationState != "missing_script" {
		t.Fatalf("expected missing_script, got %s", status.IntegrationState)
	}
	if !strings.Contains(status.Detail, "does not support Claude Code hook execution") {
		t.Fatalf("unexpected detail: %s", status.Detail)
	}
}

func TestCursorDetectConfiguredRequiresHookCapableAgent(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".cursor", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "cursor-hook.sh")

	if err := installCursorHookAt(userHooksPath, scriptPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	stubAgentHookSupport(t, func(flag string) (bool, string) {
		if flag != "-cursor-hook" {
			t.Fatalf("unexpected flag %q", flag)
		}
		return false, "flag provided but not defined"
	})

	status := detectCursorIntegrationWithInstallDetector(
		userHooksPath,
		filepath.Join(dir, "enterprise", "hooks.json"),
		scriptPath,
		func() (bool, string) { return true, "" },
	)
	if status.IntegrationState != "missing_script" {
		t.Fatalf("expected missing_script, got %s", status.IntegrationState)
	}
	if !strings.Contains(status.Detail, "does not support Cursor hook execution") {
		t.Fatalf("unexpected detail: %s", status.Detail)
	}
}

func TestGitHubCopilotDetectConfiguredRequiresHookCapableAgent(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "Code", "User", "settings.json")
	hooksDir := filepath.Join(dir, "hooks", "copilot")
	scriptPath := filepath.Join(hooksDir, "copilot-hook.sh")
	configPath := filepath.Join(hooksDir, copilotHookConfigMarker)

	if err := installGitHubCopilotHookAt(settingsPath, hooksDir, scriptPath, configPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	stubAgentHookSupport(t, func(flag string) (bool, string) {
		if flag != "-copilot-hook" {
			t.Fatalf("unexpected flag %q", flag)
		}
		return false, "flag provided but not defined"
	})

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

func TestWindsurfDetectConfiguredRequiresHookCapableAgent(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".codeium", "windsurf", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "windsurf-hook.sh")
	launcherPath := filepath.Join(dir, "hooks", windsurfHookLauncherMarker)

	if err := installWindsurfHookAt(userHooksPath, scriptPath, launcherPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	stubAgentHookSupport(t, func(flag string) (bool, string) {
		if flag != windsurfHookAgentFlag {
			t.Fatalf("unexpected flag %q", flag)
		}
		return false, "flag provided but not defined"
	})

	status := detectWindsurfIntegrationWithInstallDetector(
		userHooksPath,
		filepath.Join(dir, "system", "hooks.json"),
		scriptPath,
		func() (bool, string) { return true, "" },
	)
	if status.IntegrationState != "missing_script" {
		t.Fatalf("expected missing_script, got %s", status.IntegrationState)
	}
	if !strings.Contains(status.Detail, "does not support Windsurf hook execution") {
		t.Fatalf("unexpected detail: %s", status.Detail)
	}
}

func setDarwinTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", "")
	return home
}

func assertExecutableScript(t *testing.T, path string, wantSubstrings []string) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode()&0111 == 0 {
		t.Fatalf("expected %s to be executable, mode=%#o", path, info.Mode().Perm())
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(content)
	for _, want := range wantSubstrings {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %s to contain %q, got:\n%s", path, want, text)
		}
	}
	if strings.Contains(strings.ToLower(text), ".ps1") {
		t.Fatalf("expected %s to be a macOS shell wrapper, got:\n%s", path, text)
	}
}

func stubAgentHookSupport(t *testing.T, fn func(string) (bool, string)) {
	t.Helper()

	agentHookSupportMu.Lock()
	oldFn := agentHookSupportFn
	oldCache := agentHookSupportCache
	agentHookSupportFn = fn
	agentHookSupportCache = map[string]agentHookSupportResult{}
	agentHookSupportMu.Unlock()

	t.Cleanup(func() {
		agentHookSupportMu.Lock()
		agentHookSupportFn = oldFn
		agentHookSupportCache = oldCache
		agentHookSupportMu.Unlock()
	})
}
