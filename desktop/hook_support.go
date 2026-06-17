package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type agentHookSupportResult struct {
	checkedAt time.Time
	supported bool
	detail    string
}

var (
	agentHookSupportMu    sync.Mutex
	agentHookSupportCache = map[string]agentHookSupportResult{}
	agentHookSupportFn    = detectAgentHookSupport
)

func themistoHooksBaseDir() string {
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			home := os.Getenv("USERPROFILE")
			if home == "" {
				home, _ = os.UserHomeDir()
			}
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(localAppData, "Themisto", "hooks")
	}

	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		// Use a space-free path so hook command strings written into hooks.json
		// are not split by the shell. ~/Library/Application Support contains a
		// space that causes "/bin/sh: .../Application: No such file or directory".
		return filepath.Join(home, ".themisto", "hooks")
	}
	return filepath.Join(home, ".local", "share", "themisto", "hooks")
}

func isPowerShellScriptPath(scriptPath string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(scriptPath)), ".ps1")
}

func posixShellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'"'"'`) + "'"
}

func posixHookScript(flag string, extraEnv map[string]string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("set -eu\n")
	for key, value := range extraEnv {
		fmt.Fprintf(&b, "export %s=%s\n", key, posixShellQuote(value))
	}
	fmt.Fprintf(&b, "exec %s %s\n", posixShellQuote(agentBinaryPath), flag)
	return b.String()
}

func agentBinarySupportsHookFlag(flag string) (bool, string) {
	key := strings.TrimSpace(agentBinaryPath) + "|" + strings.TrimSpace(flag)

	agentHookSupportMu.Lock()
	if cached, ok := agentHookSupportCache[key]; ok && time.Since(cached.checkedAt) < 30*time.Second {
		agentHookSupportMu.Unlock()
		return cached.supported, cached.detail
	}
	agentHookSupportMu.Unlock()

	supported, detail := agentHookSupportFn(flag)

	agentHookSupportMu.Lock()
	agentHookSupportCache[key] = agentHookSupportResult{
		checkedAt: time.Now(),
		supported: supported,
		detail:    detail,
	}
	agentHookSupportMu.Unlock()
	return supported, detail
}

func detectAgentHookSupport(flag string) (bool, string) {
	path := strings.TrimSpace(agentBinaryPath)
	if path == "" {
		return false, "Themisto agent binary path is empty."
	}
	if _, err := os.Stat(path); err != nil {
		return false, fmt.Sprintf("Themisto agent binary not found at %s: %v", path, err)
	}

	cmd := exec.Command(path, flag)
	cmd.Stdin = strings.NewReader("")
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		return true, ""
	}

	detail := strings.TrimSpace(stderr.String())
	if detail == "" {
		detail = fmt.Sprintf("%s did not accept %s", path, flag)
	}
	return false, detail
}
