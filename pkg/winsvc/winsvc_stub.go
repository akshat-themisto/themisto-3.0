//go:build !windows

package winsvc

import (
	"fmt"
	"time"
)

const (
	ServiceName    = "ThemistoAgent"
	ServiceDisplay = "Themisto Agent"
	ServiceDesc    = "Themisto AI governance agent."
	LegacyTaskName = "ThemistoAgent"
)

func ServiceBinaryPath(exePath, configPath string) string {
	return fmt.Sprintf(`"%s" -config "%s"`, exePath, configPath)
}

func InstallOrUpdateService(exePath, configPath string) error {
	return fmt.Errorf("winsvc: not supported on this platform")
}

func RemoveService() error {
	return fmt.Errorf("winsvc: not supported on this platform")
}

func StartService() error {
	return fmt.Errorf("winsvc: not supported on this platform")
}

func StopService() error { return nil }

func StopServiceAndWait(_ time.Duration) error { return nil }

func QueryState() (string, string) { return "not_found", "not a Windows system" }

func WaitForRunning(_ time.Duration) error {
	return fmt.Errorf("winsvc: not supported on this platform")
}

func LegacyTaskExists() bool  { return false }
func StopLegacyTask()         {}
func RemoveLegacyTask()       {}
func RestartLegacyTask()      {}
