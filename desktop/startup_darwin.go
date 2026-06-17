//go:build darwin

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	darwinDesktopLaunchAgentLabel     = "com.themisto.desktop"
	darwinDesktopLaunchAgentFilename  = darwinDesktopLaunchAgentLabel + ".plist"
	darwinLaunchOnLoginConfiguredFile = "desktop-launch-on-login-configured"
)

func ensureLaunchOnLoginDefault() {
	if isLaunchOnLoginConfigured() {
		return
	}
	if err := setLaunchOnLoginEnabled(true); err == nil {
		_ = markLaunchOnLoginConfigured()
	}
}

// GetLaunchOnLogin reports whether the per-user LaunchAgent plist is present.
func (a *App) GetLaunchOnLogin() bool {
	plistPath, err := darwinDesktopLaunchAgentPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(plistPath)
	return err == nil
}

// SetLaunchOnLogin enables or disables background launch at user login via a
// per-user LaunchAgent. This is admin-gated as a product policy choice.
func (a *App) SetLaunchOnLogin(enabled bool, adminToken string) error {
	if !a.auth.IsAuthorized(adminToken) {
		return fmt.Errorf("unauthorized: admin authentication required")
	}
	if err := setLaunchOnLoginEnabled(enabled); err != nil {
		return err
	}
	return markLaunchOnLoginConfigured()
}

func setLaunchOnLoginEnabled(enabled bool) error {
	plistPath, err := darwinDesktopLaunchAgentPath()
	if err != nil {
		return err
	}

	if !enabled {
		unloadDarwinDesktopLaunchAgent()
		if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	exe, err := resolveLaunchOnLoginExecutable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(plistPath), "themisto-desktop-*.plist")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(desktopLaunchAgentPlist(exe)); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), plistPath)
}

func desktopLaunchAgentPlist(exe string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>--background</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <false/>
  <key>LimitLoadToSessionType</key>
  <array>
    <string>Aqua</string>
  </array>
</dict>
</plist>
`, darwinDesktopLaunchAgentLabel, exe)
}

func resolveLaunchOnLoginExecutable() (string, error) {
	seen := map[string]struct{}{}
	candidates := []string{}

	if exe, err := os.Executable(); err == nil && exe != "" {
		candidates = append(candidates, filepath.Clean(exe))
	}
	if desktopBinaryPath != "" {
		candidates = append(candidates, filepath.Clean(desktopBinaryPath))
	}
	candidates = append(candidates, "/Applications/Themisto.app/Contents/MacOS/themisto-desktop")

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}

		if !isMacAppBundleBinary(candidate) {
			continue
		}
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("desktop launch-on-login requires a macOS app bundle build")
}

func isMacAppBundleBinary(path string) bool {
	clean := filepath.Clean(path)
	return strings.Contains(clean, ".app/Contents/MacOS/")
}

func darwinDesktopLaunchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", darwinDesktopLaunchAgentFilename), nil
}

func darwinDesktopSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "Themisto", darwinLaunchOnLoginConfiguredFile), nil
}

func isLaunchOnLoginConfigured() bool {
	path, err := darwinDesktopSettingsPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func markLaunchOnLoginConfigured() error {
	path, err := darwinDesktopSettingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("1\n"), 0644)
}

func unloadDarwinDesktopLaunchAgent() {
	domain := fmt.Sprintf("gui/%d/%s", os.Getuid(), darwinDesktopLaunchAgentLabel)
	_ = newHiddenCommand("launchctl", "bootout", domain).Run()
}
