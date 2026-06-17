package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWindsurfDetectNotInstalled(t *testing.T) {
	dir := t.TempDir()
	status := detectWindsurfIntegrationWithInstallDetector(
		filepath.Join(dir, ".codeium", "windsurf", "hooks.json"),
		filepath.Join(dir, "system", "hooks.json"),
		filepath.Join(dir, "hooks", "windsurf-hook.ps1"),
		func() (bool, string) { return false, "" },
	)

	if status.IntegrationState != "not_installed" {
		t.Fatalf("expected not_installed, got %s", status.IntegrationState)
	}
	if status.Installed {
		t.Fatal("expected Installed to be false")
	}
}

func TestWindsurfDetectMissingHook(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".codeium", "windsurf", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(userHooksPath), 0755); err != nil {
		t.Fatalf("create windsurf dir: %v", err)
	}

	status := detectWindsurfIntegrationWithInstallDetector(
		userHooksPath,
		filepath.Join(dir, "system", "hooks.json"),
		filepath.Join(dir, "hooks", "windsurf-hook.ps1"),
		func() (bool, string) { return false, "" },
	)

	if status.IntegrationState != "missing_hook" {
		t.Fatalf("expected missing_hook, got %s", status.IntegrationState)
	}
	if !status.Installed {
		t.Fatal("expected Installed to be true from .codeium/windsurf directory")
	}
}

func TestWindsurfDetectConfigured(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".codeium", "windsurf", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "windsurf-hook.ps1")
	launcherPath := filepath.Join(dir, "hooks", windsurfHookLauncherMarker)

	if err := installWindsurfHookAt(userHooksPath, scriptPath, launcherPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	status := detectWindsurfIntegrationAt(userHooksPath, filepath.Join(dir, "system", "hooks.json"), scriptPath)
	if status.IntegrationState != "configured" {
		t.Fatalf("expected configured, got %s", status.IntegrationState)
	}
	if !status.HookConfigured || !status.HookScriptExists {
		t.Fatal("expected hook and script to be detected")
	}
}

func TestWindsurfDetectMissingScript(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".codeium", "windsurf", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "windsurf-hook.ps1")
	launcherPath := filepath.Join(dir, "hooks", windsurfHookLauncherMarker)

	if err := installWindsurfHookAt(userHooksPath, scriptPath, launcherPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if err := os.Remove(scriptPath); err != nil {
		t.Fatalf("remove script: %v", err)
	}

	status := detectWindsurfIntegrationAt(userHooksPath, filepath.Join(dir, "system", "hooks.json"), scriptPath)
	if status.IntegrationState != "missing_script" {
		t.Fatalf("expected missing_script, got %s", status.IntegrationState)
	}
}

func TestWindsurfDetectMissingLauncher(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".codeium", "windsurf", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "windsurf-hook.ps1")
	launcherPath := filepath.Join(dir, "hooks", windsurfHookLauncherMarker)

	if err := installWindsurfHookAt(userHooksPath, scriptPath, launcherPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if err := os.Remove(launcherPath); err != nil {
		t.Fatalf("remove launcher: %v", err)
	}

	status := detectWindsurfIntegrationAt(userHooksPath, filepath.Join(dir, "system", "hooks.json"), scriptPath)
	if status.IntegrationState != "missing_script" {
		t.Fatalf("expected missing_script, got %s", status.IntegrationState)
	}
}

func TestWindsurfDetectPartial(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".codeium", "windsurf", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "windsurf-hook.ps1")
	launcherPath := filepath.Join(dir, "hooks", windsurfHookLauncherMarker)

	if err := os.MkdirAll(filepath.Dir(userHooksPath), 0755); err != nil {
		t.Fatalf("create windsurf dir: %v", err)
	}
	if err := ensureWindsurfHookScript(scriptPath); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if err := ensureWindsurfHookLauncher(launcherPath); err != nil {
		t.Fatalf("write launcher: %v", err)
	}

	status := detectWindsurfIntegrationAt(userHooksPath, filepath.Join(dir, "system", "hooks.json"), scriptPath)
	if status.IntegrationState != "partial" {
		t.Fatalf("expected partial, got %s", status.IntegrationState)
	}
}

func TestWindsurfInstallCreatesHooksJSON(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".codeium", "windsurf", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "windsurf-hook.ps1")
	launcherPath := filepath.Join(dir, "hooks", windsurfHookLauncherMarker)

	if err := installWindsurfHookAt(userHooksPath, scriptPath, launcherPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	config, exists, err := readWindsurfHooksConfig(userHooksPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !exists {
		t.Fatal("expected hooks.json to exist")
	}
	if !windsurfHooksContainThemisto(config) {
		t.Fatal("expected hooks.json to contain Themisto hook")
	}
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("expected script on disk: %v", err)
	}
}

func TestWindsurfInstallPreservesOtherHooks(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".codeium", "windsurf", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "windsurf-hook.ps1")
	launcherPath := filepath.Join(dir, "hooks", windsurfHookLauncherMarker)

	writeTestJSON(t, userHooksPath, map[string]interface{}{
		"hooks": map[string]interface{}{
			"pre_user_prompt": []interface{}{
				map[string]interface{}{
					"command": "some-other-hook",
				},
			},
			"post_user_prompt": []interface{}{
				map[string]interface{}{
					"command": "logging-hook",
				},
			},
		},
	})

	if err := installWindsurfHookAt(userHooksPath, scriptPath, launcherPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	config, _, err := readWindsurfHooksConfig(userHooksPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	hooks := config["hooks"].(map[string]interface{})
	if hooks["post_user_prompt"] == nil {
		t.Fatal("expected post_user_prompt to be preserved")
	}
	preUserPrompt := hooks["pre_user_prompt"].([]interface{})
	if len(preUserPrompt) != 2 {
		t.Fatalf("expected 2 pre_user_prompt entries, got %d", len(preUserPrompt))
	}
}

func TestWindsurfInstallIdempotent(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".codeium", "windsurf", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "windsurf-hook.ps1")
	launcherPath := filepath.Join(dir, "hooks", windsurfHookLauncherMarker)

	if err := installWindsurfHookAt(userHooksPath, scriptPath, launcherPath); err != nil {
		t.Fatalf("first install failed: %v", err)
	}
	if err := installWindsurfHookAt(userHooksPath, scriptPath, launcherPath); err != nil {
		t.Fatalf("second install failed: %v", err)
	}

	config, _, err := readWindsurfHooksConfig(userHooksPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	entries := config["hooks"].(map[string]interface{})["pre_user_prompt"].([]interface{})

	themistoCount := 0
	for _, entry := range entries {
		if windsurfEntryContainsThemisto(entry) {
			themistoCount++
		}
	}
	if themistoCount != 1 {
		t.Fatalf("expected exactly one Themisto hook, got %d", themistoCount)
	}
}

func TestWindsurfRemovePreservesOtherHooks(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".codeium", "windsurf", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "windsurf-hook.ps1")
	launcherPath := filepath.Join(dir, "hooks", windsurfHookLauncherMarker)

	writeTestJSON(t, userHooksPath, map[string]interface{}{
		"hooks": map[string]interface{}{
			"pre_user_prompt": []interface{}{
				map[string]interface{}{
					"command": "other-hook",
				},
			},
		},
	})

	if err := installWindsurfHookAt(userHooksPath, scriptPath, launcherPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if err := removeWindsurfHookAt(userHooksPath); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	config, _, err := readWindsurfHooksConfig(userHooksPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	entries := config["hooks"].(map[string]interface{})["pre_user_prompt"].([]interface{})
	if len(entries) != 1 {
		t.Fatalf("expected one remaining hook, got %d", len(entries))
	}
	if windsurfEntryContainsThemisto(entries[0]) {
		t.Fatal("expected Themisto hook to be removed")
	}
}

func TestWindsurfRemoveNonexistent(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".codeium", "windsurf", "hooks.json")

	if err := removeWindsurfHookAt(userHooksPath); err != nil {
		t.Fatalf("expected no error removing nonexistent hooks.json: %v", err)
	}
}

func TestWindsurfConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".codeium", "windsurf", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "windsurf-hook.ps1")
	launcherPath := filepath.Join(dir, "hooks", windsurfHookLauncherMarker)

	if err := installWindsurfHookAt(userHooksPath, scriptPath, launcherPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if err := removeWindsurfHookAt(userHooksPath); err != nil {
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
	// Windsurf hooks.json has no version field — just hooks
	if _, ok := config["hooks"]; ok {
		t.Fatal("expected hooks key to be deleted when empty")
	}
}

func TestWindsurfInstallRefusesInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".codeium", "windsurf", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "windsurf-hook.ps1")
	launcherPath := filepath.Join(dir, "hooks", windsurfHookLauncherMarker)

	if err := os.MkdirAll(filepath.Dir(userHooksPath), 0755); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	if err := os.WriteFile(userHooksPath, []byte("{not-valid"), 0644); err != nil {
		t.Fatalf("write invalid json: %v", err)
	}

	err := installWindsurfHookAt(userHooksPath, scriptPath, launcherPath)
	if err == nil {
		t.Fatal("expected invalid JSON to fail")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("expected invalid JSON error, got %v", err)
	}
}

func TestEnsureWindsurfHookScript(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "hooks", "windsurf-hook.ps1")

	if err := ensureWindsurfHookScript(scriptPath); err != nil {
		t.Fatalf("ensure script failed: %v", err)
	}

	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script failed: %v", err)
	}
	text := string(content)

	required := []string{
		"Themisto Windsurf Hook",
		"exit 2",
		"/v1/prompt/evaluate",
		"/v1/prompt/outcome",
		"windsurf-hook.log",
		`surface          = "windsurf"`,
		"tool_info",
	}
	for _, marker := range required {
		if !strings.Contains(text, marker) {
			t.Fatalf("expected script to contain %q", marker)
		}
	}
}

func TestBuildWindsurfHookCommand(t *testing.T) {
	launcherPath := `/tmp/test/hooks/windsurf-hook.cmd`
	command := buildWindsurfHookCommand(launcherPath)
	if command == "" {
		t.Fatal("expected non-empty command")
	}
	if runtime.GOOS == "windows" {
		if !strings.Contains(strings.ToLower(command), strings.ToLower(`c:\windows\system32\cmd.exe`)) {
			t.Fatalf("expected cmd.exe in command, got %s", command)
		}
		if !strings.Contains(strings.ToLower(command), "windsurf-hook.cmd") {
			t.Fatalf("expected launcher path in command, got %s", command)
		}
		return
	}
	if !strings.Contains(command, "windsurf-hook.cmd") {
		t.Fatalf("expected launcher path in command, got %s", command)
	}
}

func TestWindsurfEntryContainsThemisto_AgentCommand(t *testing.T) {
	entry := map[string]interface{}{
		"command": `"` + agentBinaryPath + `" ` + windsurfHookAgentFlag,
	}
	if !windsurfEntryContainsThemisto(entry) {
		t.Fatal("expected agent hook command to be detected as Themisto")
	}
}

func TestWindsurfEntryContainsThemisto_LauncherCommand(t *testing.T) {
	entry := map[string]interface{}{
		"command": `C:\Windows\System32\cmd.exe /d /c ""C:\Users\aryan\AppData\Local\Themisto\hooks\windsurf-hook.cmd""`,
	}
	if !windsurfEntryContainsThemisto(entry) {
		t.Fatal("expected launcher hook command to be detected as Themisto")
	}
}

func TestWindsurfDetectWorkspaceOnly(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".codeium", "windsurf", "hooks.json")
	systemHooksPath := filepath.Join(dir, "system", "hooks.json")
	workspacePath := filepath.Join(dir, "project", ".windsurf", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "windsurf-hook.ps1")

	// Create user-level dir so it's detected as installed
	if err := os.MkdirAll(filepath.Dir(userHooksPath), 0755); err != nil {
		t.Fatalf("create windsurf dir: %v", err)
	}

	// Write workspace-level hooks with Themisto
	writeTestJSON(t, workspacePath, map[string]interface{}{
		"hooks": map[string]interface{}{
			"pre_user_prompt": []interface{}{
				map[string]interface{}{
					"command": "cmd /c powershell.exe -File windsurf-hook.ps1",
				},
			},
		},
	})

	// No user or system hooks, no local script — workspace-only
	status := detectWindsurfIntegrationFull(
		userHooksPath, systemHooksPath, []string{workspacePath}, scriptPath,
		func() (bool, string) { return false, "" },
	)
	if status.IntegrationState != "configured" {
		t.Fatalf("expected configured, got %s", status.IntegrationState)
	}
	if !status.WorkspaceManaged {
		t.Fatal("expected WorkspaceManaged to be true")
	}
	if status.HookSource != "workspace" {
		t.Fatalf("expected hook source 'workspace', got %s", status.HookSource)
	}
	if !status.HookConfigured {
		t.Fatal("expected HookConfigured to be true")
	}
}

func TestWindsurfDetectUserAndWorkspace(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".codeium", "windsurf", "hooks.json")
	systemHooksPath := filepath.Join(dir, "system", "hooks.json")
	workspacePath := filepath.Join(dir, "project", ".windsurf", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "windsurf-hook.ps1")
	launcherPath := filepath.Join(dir, "hooks", windsurfHookLauncherMarker)

	// Install at user level (creates both hooks.json and script)
	if err := installWindsurfHookAt(userHooksPath, scriptPath, launcherPath); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	// Write workspace-level hooks with Themisto
	writeTestJSON(t, workspacePath, map[string]interface{}{
		"hooks": map[string]interface{}{
			"pre_user_prompt": []interface{}{
				map[string]interface{}{
					"command": "cmd /c powershell.exe -File windsurf-hook.ps1",
				},
			},
		},
	})

	status := detectWindsurfIntegrationFull(
		userHooksPath, systemHooksPath, []string{workspacePath}, scriptPath,
		func() (bool, string) { return false, "" },
	)
	if status.IntegrationState != "configured" {
		t.Fatalf("expected configured, got %s", status.IntegrationState)
	}
	if !status.WorkspaceManaged {
		t.Fatal("expected WorkspaceManaged to be true")
	}
	if status.HookSource != "user_and_workspace" {
		t.Fatalf("expected hook source 'user_and_workspace', got %s", status.HookSource)
	}
}

func TestWindsurfDetectSystemAndWorkspace(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".codeium", "windsurf", "hooks.json")
	systemHooksPath := filepath.Join(dir, "system", "hooks.json")
	workspacePath := filepath.Join(dir, "project", ".windsurf", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "windsurf-hook.ps1")
	launcherPath := filepath.Join(dir, "hooks", windsurfHookLauncherMarker)

	// Create user dir for install detection
	if err := os.MkdirAll(filepath.Dir(userHooksPath), 0755); err != nil {
		t.Fatalf("create windsurf dir: %v", err)
	}
	if err := ensureWindsurfHookScript(scriptPath); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if err := ensureWindsurfHookLauncher(launcherPath); err != nil {
		t.Fatalf("write launcher: %v", err)
	}

	themistoCmd := "cmd /c powershell.exe -File windsurf-hook.ps1"

	// System hooks
	writeTestJSON(t, systemHooksPath, map[string]interface{}{
		"hooks": map[string]interface{}{
			"pre_user_prompt": []interface{}{
				map[string]interface{}{"command": themistoCmd},
			},
		},
	})
	// Workspace hooks
	writeTestJSON(t, workspacePath, map[string]interface{}{
		"hooks": map[string]interface{}{
			"pre_user_prompt": []interface{}{
				map[string]interface{}{"command": themistoCmd},
			},
		},
	})

	status := detectWindsurfIntegrationFull(
		userHooksPath, systemHooksPath, []string{workspacePath}, scriptPath,
		func() (bool, string) { return false, "" },
	)
	if status.IntegrationState != "configured" {
		t.Fatalf("expected configured, got %s", status.IntegrationState)
	}
	if status.HookSource != "system_and_workspace" {
		t.Fatalf("expected hook source 'system_and_workspace', got %s", status.HookSource)
	}
	if !status.SystemManaged || !status.WorkspaceManaged {
		t.Fatal("expected both SystemManaged and WorkspaceManaged to be true")
	}
}

func TestWindsurfDetectSystemManaged(t *testing.T) {
	dir := t.TempDir()
	userHooksPath := filepath.Join(dir, ".codeium", "windsurf", "hooks.json")
	systemHooksPath := filepath.Join(dir, "system", "hooks.json")
	scriptPath := filepath.Join(dir, "hooks", "windsurf-hook.ps1")
	launcherPath := filepath.Join(dir, "hooks", windsurfHookLauncherMarker)

	// Create user-level dir so it's detected as installed
	if err := os.MkdirAll(filepath.Dir(userHooksPath), 0755); err != nil {
		t.Fatalf("create windsurf dir: %v", err)
	}

	// Write system-level hooks with Themisto
	writeTestJSON(t, systemHooksPath, map[string]interface{}{
		"hooks": map[string]interface{}{
			"pre_user_prompt": []interface{}{
				map[string]interface{}{
					"command": "cmd /c powershell.exe -File windsurf-hook.ps1",
				},
			},
		},
	})

	// Write the hook script
	if err := ensureWindsurfHookScript(scriptPath); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if err := ensureWindsurfHookLauncher(launcherPath); err != nil {
		t.Fatalf("write launcher: %v", err)
	}

	status := detectWindsurfIntegrationAt(userHooksPath, systemHooksPath, scriptPath)
	if !status.SystemManaged {
		t.Fatal("expected SystemManaged to be true")
	}
	if status.HookSource != "system" {
		t.Fatalf("expected hook source 'system', got %s", status.HookSource)
	}
	if status.IntegrationState != "configured" {
		t.Fatalf("expected configured, got %s", status.IntegrationState)
	}
}
