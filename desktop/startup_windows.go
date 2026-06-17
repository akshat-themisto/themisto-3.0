//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath                       = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName                     = "ThemistoDesktop"
	desktopSettingsKeyPath           = `Software\Themisto\Desktop`
	launchOnLoginConfiguredValueName = "LaunchOnLoginConfigured"
)

// GetLaunchOnLogin reads the HKCU Run registry key to determine if
// the desktop app is registered to start on user login.
func (a *App) GetLaunchOnLogin() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	val, _, err := k.GetStringValue(runValueName)
	if err != nil {
		return false
	}
	return val != ""
}

func ensureLaunchOnLoginDefault() {
	if isLaunchOnLoginConfigured() {
		return
	}
	if err := setLaunchOnLoginEnabled(true); err == nil {
		_ = markLaunchOnLoginConfigured()
	}
}

// SetLaunchOnLogin enables or disables the desktop app's launch-on-login
// registration. This writes to HKCU (per-user, no elevation needed) and
// is admin-gated even though it uses HKCU, because this is a client-facing
// policy choice rather than an employee preference.
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
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if enabled {
		exe, err := resolveDesktopExePath()
		if err != nil {
			return err
		}
		return k.SetStringValue(runValueName, launchOnLoginCommand(exe))
	}
	_ = k.DeleteValue(runValueName)
	return nil
}

func launchOnLoginCommand(exe string) string {
	return `"` + exe + `" --background`
}

func isLaunchOnLoginConfigured() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, desktopSettingsKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetIntegerValue(launchOnLoginConfiguredValueName)
	return err == nil
}

func markLaunchOnLoginConfigured() error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, desktopSettingsKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetDWordValue(launchOnLoginConfiguredValueName, 1)
}

// resolveDesktopExePath returns the path to the currently running executable.
// Falls back to the canonical installed path if os.Executable fails.
func resolveDesktopExePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return desktopBinaryPath, nil
	}
	return exe, nil
}
