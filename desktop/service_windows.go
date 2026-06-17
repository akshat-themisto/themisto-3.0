//go:build windows

package main

import (
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"

	"github.com/themisto/agent/pkg/winsvc"
)

// isServiceInstalled returns true if the ThemistoAgent Windows Service is
// registered with SCM (regardless of whether it is currently running).
func isServiceInstalled() bool {
	state, _ := winsvc.QueryState()
	return state != "not_found"
}

// queryServiceState returns the service state and a human-readable detail
// string. Possible states: "running", "stopped", "start_pending",
// "stop_pending", "not_found", "unknown".
func queryServiceState() (string, string) {
	return winsvc.QueryState()
}

// startService starts the agent Windows Service via SCM.
func startService() error {
	state, _ := winsvc.QueryState()
	if state == "not_found" {
		return fmt.Errorf("start service: agent service is not installed; repair agent service first")
	}
	if err := winsvc.StartService(); err == nil {
		return nil
	}
	if err := runElevatedAgentCommand("-service-start"); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	return nil
}

// stopService sends a stop control to the agent Windows Service.
func stopService() error {
	if err := winsvc.StopService(); err == nil {
		return nil
	}
	if err := runElevatedAgentCommand("-service-stop"); err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
	return nil
}

// repairAgentService validates files, removes any existing service, reinstalls,
// starts, verifies running, and cleans up the legacy scheduled task.
func repairAgentService() error {
	if err := validateStartupFiles(agentBinaryPath, agentConfigPath); err != nil {
		return err
	}

	// Stop and remove existing service (repair = explicit remove + recreate).
	if err := winsvc.StopServiceAndWait(10 * time.Second); err != nil {
		return fmt.Errorf("repair agent service: stop failed: %w", err)
	}
	if err := winsvc.RemoveService(); err != nil {
		return fmt.Errorf("repair agent service: remove failed: %w", err)
	}
	time.Sleep(1 * time.Second) // SCM deletion propagation

	// Install fresh.
	if err := winsvc.InstallOrUpdateService(agentBinaryPath, agentConfigPath); err != nil {
		return fmt.Errorf("repair agent service: install failed: %w", err)
	}

	// Start the service.
	if err := winsvc.StartService(); err != nil {
		return fmt.Errorf("repair agent service: start failed: %w", err)
	}

	// Verify running.
	if err := winsvc.WaitForRunning(15 * time.Second); err != nil {
		return fmt.Errorf("repair agent service: service did not reach running state: %w", err)
	}

	// Clean up legacy scheduled task.
	winsvc.RemoveLegacyTask()

	return nil
}

func runElevatedAgentCommand(args string) error {
	if _, err := os.Stat(agentBinaryPath); err != nil {
		return fmt.Errorf("installed agent binary not found at %s", agentBinaryPath)
	}

	mod := syscall.NewLazyDLL("shell32.dll")
	proc := mod.NewProc("ShellExecuteW")

	verb, _ := syscall.UTF16PtrFromString("runas")
	exe, _ := syscall.UTF16PtrFromString(agentBinaryPath)
	params, _ := syscall.UTF16PtrFromString(args)
	dir, _ := syscall.UTF16PtrFromString(`C:\Program Files\Themisto`)

	ret, _, _ := proc.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(exe)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(dir)),
		0,
	)
	if ret <= 32 {
		return fmt.Errorf("elevated agent command failed (ShellExecute returned %d)", ret)
	}
	return nil
}
