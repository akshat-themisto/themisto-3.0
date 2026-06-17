//go:build !windows && !darwin

package main

func ensureLaunchOnLoginDefault() {}

// GetLaunchOnLogin is a no-op on unsupported platforms.
func (a *App) GetLaunchOnLogin() bool { return false }

// SetLaunchOnLogin is a no-op on unsupported platforms.
func (a *App) SetLaunchOnLogin(enabled bool, adminToken string) error { return nil }
