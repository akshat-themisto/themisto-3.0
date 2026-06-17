package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCursorDetectNotInstalled(t *testing.T) {
	dir := t.TempDir()
	status := detectCursorIntegrationWithInstallDetector(
		filepath.Join(dir, ".cursor", "hooks.json"),
		filepath.Join(dir, "enterprise", "hooks.json"),
		filepath.Join(dir, "hooks", "cursor-hook.ps1"),
		func() (bool, string) { return false, "" },
	)

	if status.IntegrationState != "not_installed" {
		t.Fatalf("expected not_installed, got %s", status.IntegrationState)
	}
	if status.Installed {
		t.Fatal("expected Installed to be false")
	}
}

func TestCursorDetectMissingHook(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".cursor", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(userHooksPath), 0755); err != nil {
		t.Fatalf("create cursor dir: %v", err)
	}

	status := detectCursorIntegrationWithInstallDetector(
		userHooksPath,
		filepath.Join(dir, "enterprise", "hooks.json"),
		filepath.Join(dir, "hooks", "cursor-hook.ps1"),
		func() (bool, string) { return false, "" },
	)

	if status.IntegrationState != "missing_hook" {
		t.Fatalf("expected missing_hook, got %s", status.IntegrationState)
	}
	if !status.Installed {
		t.Fatal("expected Installed to be true from .cursor directory")
	}
}

func TestCursorDetectConfigured(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".cursor", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "cursor-hook.ps1")

	if err := installCursorHookAt(userHooksPath, scriptPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	status := detectCursorIntegrationAt(userHooksPath, filepath.Join(dir, "enterprise", "hooks.json"), scriptPath)
	if status.IntegrationState != "configured" {
		t.Fatalf("expected configured, got %s", status.IntegrationState)
	}
	if !status.HookConfigured || !status.HookScriptExists {
		t.Fatal("expected hook and script to be detected")
	}
}

func TestCursorDetectMissingScript(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".cursor", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "cursor-hook.ps1")

	if err := installCursorHookAt(userHooksPath, scriptPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if err := os.Remove(scriptPath); err != nil {
		t.Fatalf("remove script: %v", err)
	}

	status := detectCursorIntegrationAt(userHooksPath, filepath.Join(dir, "enterprise", "hooks.json"), scriptPath)
	if status.IntegrationState != "missing_script" {
		t.Fatalf("expected missing_script, got %s", status.IntegrationState)
	}
}

func TestCursorDetectPartial(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".cursor", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "cursor-hook.ps1")

	if err := os.MkdirAll(filepath.Dir(userHooksPath), 0755); err != nil {
		t.Fatalf("create cursor dir: %v", err)
	}
	if err := ensureCursorHookScript(scriptPath); err != nil {
		t.Fatalf("write script: %v", err)
	}

	status := detectCursorIntegrationAt(userHooksPath, filepath.Join(dir, "enterprise", "hooks.json"), scriptPath)
	if status.IntegrationState != "partial" {
		t.Fatalf("expected partial, got %s", status.IntegrationState)
	}
}

func TestCursorInstallCreatesHooksJSON(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".cursor", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "cursor-hook.ps1")

	if err := installCursorHookAt(userHooksPath, scriptPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	config, exists, err := readCursorHooksConfig(userHooksPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !exists {
		t.Fatal("expected hooks.json to exist")
	}
	if !cursorHooksContainThemisto(config) {
		t.Fatal("expected hooks.json to contain Themisto hook")
	}
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("expected script on disk: %v", err)
	}
}

func TestCursorInstallPreservesOtherHooks(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".cursor", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "cursor-hook.ps1")

	writeTestJSON(t, userHooksPath, map[string]interface{}{
		"version": 1,
		"hooks": map[string]interface{}{
			"beforeSubmitPrompt": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": "some-other-hook",
				},
			},
			"beforeShellCommand": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": "shell-guard",
				},
			},
		},
	})

	if err := installCursorHookAt(userHooksPath, scriptPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	config, _, err := readCursorHooksConfig(userHooksPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	hooks := config["hooks"].(map[string]interface{})
	if hooks["beforeShellCommand"] == nil {
		t.Fatal("expected beforeShellCommand to be preserved")
	}
	beforeSubmit := hooks["beforeSubmitPrompt"].([]interface{})
	if len(beforeSubmit) != 2 {
		t.Fatalf("expected 2 beforeSubmitPrompt entries, got %d", len(beforeSubmit))
	}
}

func TestCursorInstallIdempotent(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".cursor", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "cursor-hook.ps1")

	if err := installCursorHookAt(userHooksPath, scriptPath); err != nil {
		t.Fatalf("first install failed: %v", err)
	}
	if err := installCursorHookAt(userHooksPath, scriptPath); err != nil {
		t.Fatalf("second install failed: %v", err)
	}

	config, _, err := readCursorHooksConfig(userHooksPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	entries := config["hooks"].(map[string]interface{})["beforeSubmitPrompt"].([]interface{})

	themistoCount := 0
	for _, entry := range entries {
		if cursorEntryContainsThemisto(entry) {
			themistoCount++
		}
	}
	if themistoCount != 1 {
		t.Fatalf("expected exactly one Themisto hook, got %d", themistoCount)
	}
}

func TestCursorRemovePreservesOtherHooks(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".cursor", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "cursor-hook.ps1")

	writeTestJSON(t, userHooksPath, map[string]interface{}{
		"version": 1,
		"hooks": map[string]interface{}{
			"beforeSubmitPrompt": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": "other-hook",
				},
			},
		},
	})

	if err := installCursorHookAt(userHooksPath, scriptPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if err := removeCursorHookAt(userHooksPath); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	config, _, err := readCursorHooksConfig(userHooksPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	entries := config["hooks"].(map[string]interface{})["beforeSubmitPrompt"].([]interface{})
	if len(entries) != 1 {
		t.Fatalf("expected one remaining hook, got %d", len(entries))
	}
	if cursorEntryContainsThemisto(entries[0]) {
		t.Fatal("expected Themisto hook to be removed")
	}
}

func TestCursorRemoveNonexistent(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".cursor", "hooks.json")

	if err := removeCursorHookAt(userHooksPath); err != nil {
		t.Fatalf("expected no error removing nonexistent hooks.json: %v", err)
	}
}

func TestCursorConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".cursor", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "cursor-hook.ps1")

	if err := installCursorHookAt(userHooksPath, scriptPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if err := removeCursorHookAt(userHooksPath); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	data, err := os.ReadFile(userHooksPath)
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("expected valid JSON after round trip: %v", err)
	}
	if _, ok := config["version"]; !ok {
		t.Fatal("expected version field to remain")
	}
	if hooks, ok := config["hooks"].(map[string]interface{}); ok {
		if hooks["beforeSubmitPrompt"] != nil {
			t.Fatal("expected beforeSubmitPrompt to be removed")
		}
		if len(hooks) > 0 {
			t.Fatalf("expected empty hooks map to be cleaned up, got %d keys", len(hooks))
		}
	}
	// When the only hook type was beforeSubmitPrompt, the entire "hooks" key
	// should be deleted by removeCursorThemistoHook.
	if _, ok := config["hooks"]; ok {
		t.Fatal("expected hooks key to be deleted when empty")
	}
}

func TestCursorInstallRefusesInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".cursor", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "cursor-hook.ps1")

	if err := os.MkdirAll(filepath.Dir(userHooksPath), 0755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.WriteFile(userHooksPath, []byte("{not-valid"), 0644); err != nil {
		t.Fatalf("write invalid json: %v", err)
	}

	err := installCursorHookAt(userHooksPath, scriptPath)
	if err == nil {
		t.Fatal("expected invalid JSON to fail")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("expected invalid JSON error, got %v", err)
	}
}

func TestEnsureCursorHookScript(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "hooks", "cursor-hook.ps1")

	if err := ensureCursorHookScript(scriptPath); err != nil {
		t.Fatalf("ensure script failed: %v", err)
	}

	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script failed: %v", err)
	}
	text := string(content)

	required := []string{
		"Themisto Cursor Hook",
		`{"continue":true|false}`,
		"/v1/prompt/evaluate",
		"/v1/prompt/outcome",
		`Write-CursorResult $false`,
		`Write-CursorResult $true`,
		"cursor-hook.log",
	}
	for _, marker := range required {
		if !strings.Contains(text, marker) {
			t.Fatalf("expected script to contain %q", marker)
		}
	}
}

func TestBuildWindowsCursorHookCommandUsesCmdWrapper(t *testing.T) {
	command := buildWindowsCursorHookCommand(`C:\Users\test\AppData\Local\Themisto\hooks\cursor-hook.ps1`)
	if !strings.HasPrefix(command, "cmd /c ") {
		t.Fatalf("expected command to start with cmd /c, got %s", command)
	}
	if !strings.Contains(command, "powershell.exe") {
		t.Fatalf("expected powershell.exe in command, got %s", command)
	}
	if !strings.Contains(command, `C:\Users\test\AppData\Local\Themisto\hooks\cursor-hook.ps1`) {
		t.Fatalf("expected script path in command, got %s", command)
	}
}
