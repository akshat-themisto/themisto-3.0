//go:build !windows && !darwin

package main

import "fmt"

func runInstall(_ string, logger *stdLogger) error {
	return fmt.Errorf("-install is only supported on Windows; on macOS use the native installer")
}

func runUninstall(logger *stdLogger) error {
	return fmt.Errorf("-uninstall is only supported on Windows; on macOS use the native uninstaller")
}

func runServiceStart(logger *stdLogger) error {
	return fmt.Errorf("-service-start is only supported on Windows")
}

func runServiceStop(logger *stdLogger) error {
	return fmt.Errorf("-service-stop is only supported on Windows")
}

func runServiceStatus(logger *stdLogger) error {
	return fmt.Errorf("-service-status is only supported on Windows")
}

func defaultConfigPath() string { return "agent.json" }

func isRunningAsService() bool { return false }

func runAsService(_ string) error { return nil }

func runProxyOff(logger *stdLogger) error {
	return fmt.Errorf("-proxy-off is only supported on Windows")
}
