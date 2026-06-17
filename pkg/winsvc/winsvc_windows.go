//go:build windows

// Package winsvc provides the canonical, single-source Windows Service
// definition and SCM (Service Control Manager) operations for the Themisto
// agent. Both the installer (cmd/agent) and the desktop app import this
// package; it must have zero dependencies on core/ or adapter/.
package winsvc

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	ServiceName    = "ThemistoAgent"
	ServiceDisplay = "Themisto Agent"
	ServiceDesc    = "Themisto AI governance agent - inspects and routes traffic per organizational policy."
	LegacyTaskName = "ThemistoAgent"
)

// ServiceBinaryPath returns the SCM ImagePath string for a given exe and config.
func ServiceBinaryPath(exePath, configPath string) string {
	return fmt.Sprintf(`"%s" -config "%s"`, exePath, configPath)
}

// InstallOrUpdateService creates the service if absent, or updates its config
// if it already exists. It never deletes a working service.
func InstallOrUpdateService(exePath, configPath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()

	binPath := ServiceBinaryPath(exePath, configPath)

	s, openErr := m.OpenService(ServiceName)
	if openErr == nil {
		defer s.Close()
		cfg, err := s.Config()
		if err != nil {
			return fmt.Errorf("read service config: %w", err)
		}
		cfg.BinaryPathName = binPath
		cfg.DisplayName = ServiceDisplay
		cfg.Description = ServiceDesc
		cfg.StartType = mgr.StartAutomatic
		cfg.ServiceStartName = "LocalSystem"
		cfg.DelayedAutoStart = false
		cfg.ServiceType = windows.SERVICE_WIN32_OWN_PROCESS
		cfg.ErrorControl = windows.SERVICE_ERROR_NORMAL
		if err := s.UpdateConfig(cfg); err != nil {
			return fmt.Errorf("update service config: %w", err)
		}
		return SetRecoveryActions(s)
	}
	if !serviceDoesNotExist(openErr) {
		return fmt.Errorf("open service: %w", openErr)
	}

	s, err = m.CreateService(ServiceName, exePath, mgr.Config{
		ServiceType:      windows.SERVICE_WIN32_OWN_PROCESS,
		ErrorControl:     windows.SERVICE_ERROR_NORMAL,
		DisplayName:      ServiceDisplay,
		Description:      ServiceDesc,
		StartType:        mgr.StartAutomatic,
		ServiceStartName: "LocalSystem",
		DelayedAutoStart: false,
	}, "-config", configPath)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()
	return SetRecoveryActions(s)
}

// SetRecoveryActions configures SCM restart-on-failure using the Go API.
func SetRecoveryActions(s *mgr.Service) error {
	return s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}, 86400)
}

// RemoveService deletes the service after it has been stopped.
// Idempotent when the service is not installed.
func RemoveService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		if serviceDoesNotExist(err) {
			return nil
		}
		return fmt.Errorf("open service: %w", err)
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return fmt.Errorf("query service before delete: %w", err)
	}
	if status.State != svc.Stopped {
		return fmt.Errorf("delete service: service must be stopped first (current state %s)", stateLabel(status.State))
	}

	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}

// StartService starts the installed service via SCM.
func StartService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("open service: %w", err)
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return fmt.Errorf("query service: %w", err)
	}
	if status.State == svc.Running || status.State == svc.StartPending {
		return nil
	}

	if err := s.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	return nil
}

// StopService sends a stop control to the service.
func StopService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		if serviceDoesNotExist(err) {
			return nil
		}
		return fmt.Errorf("open service: %w", err)
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return fmt.Errorf("query service: %w", err)
	}
	if status.State == svc.Stopped || status.State == svc.StopPending {
		return nil
	}

	if _, err := s.Control(svc.Stop); err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
	return nil
}

// StopServiceAndWait stops the service and waits for the stopped state.
func StopServiceAndWait(timeout time.Duration) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		if serviceDoesNotExist(err) {
			return nil
		}
		return fmt.Errorf("open service: %w", err)
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return fmt.Errorf("query service: %w", err)
	}
	if status.State == svc.Stopped {
		return nil
	}

	if status.State != svc.StopPending {
		if _, err := s.Control(svc.Stop); err != nil {
			return fmt.Errorf("stop service: %w", err)
		}
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		status, err = s.Query()
		if err != nil {
			return fmt.Errorf("query service: %w", err)
		}
		if status.State == svc.Stopped {
			return nil
		}
	}
	return fmt.Errorf("timeout waiting for service to stop")
}

// QueryState returns the current service state and a human-readable detail.
func QueryState() (state, detail string) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return "unknown", "SCM connect error: " + err.Error()
	}
	defer windows.CloseServiceHandle(scm)

	namePtr, err := syscall.UTF16PtrFromString(ServiceName)
	if err != nil {
		return "unknown", "Service name error: " + err.Error()
	}

	svcHandle, err := windows.OpenService(scm, namePtr, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		if serviceDoesNotExist(err) {
			return "not_found", "Service not registered"
		}
		return "unknown", "Open service error: " + err.Error()
	}
	defer windows.CloseServiceHandle(svcHandle)

	var status windows.SERVICE_STATUS_PROCESS
	var needed uint32
	err = windows.QueryServiceStatusEx(
		svcHandle,
		windows.SC_STATUS_PROCESS_INFO,
		(*byte)(unsafe.Pointer(&status)),
		uint32(unsafe.Sizeof(status)),
		&needed,
	)
	if err != nil {
		return "unknown", "Query error: " + err.Error()
	}

	return MapServiceState(svc.State(status.CurrentState), status.ProcessId)
}

// MapServiceState converts a svc.State and PID into a (state, detail) pair.
func MapServiceState(st svc.State, pid uint32) (state, detail string) {
	switch st {
	case svc.Running:
		return "running", "Service is running (PID " + strconv.FormatUint(uint64(pid), 10) + ")"
	case svc.Stopped:
		return "stopped", "Service is stopped"
	case svc.StartPending:
		return "start_pending", "Service is starting"
	case svc.StopPending:
		return "stop_pending", "Service is stopping"
	case svc.Paused:
		return "stopped", "Service is paused"
	default:
		return "unknown", "Unrecognized service state"
	}
}

// WaitForRunning polls until the service reaches Running or timeout.
func WaitForRunning(timeout time.Duration) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		if serviceDoesNotExist(err) {
			return fmt.Errorf("service not installed")
		}
		return fmt.Errorf("open service: %w", err)
	}
	defer s.Close()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := s.Query()
		if err != nil {
			return fmt.Errorf("query service: %w", err)
		}
		if status.State == svc.Running {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for service to reach running state")
}

// LegacyTaskExists checks whether the old ThemistoAgent scheduled task exists.
func LegacyTaskExists() bool {
	err := exec.Command("schtasks.exe", "/Query", "/TN", LegacyTaskName).Run()
	return err == nil
}

// StopLegacyTask stops the old scheduled task (best-effort).
func StopLegacyTask() {
	_ = exec.Command("schtasks.exe", "/End", "/TN", LegacyTaskName).Run()
}

// RemoveLegacyTask deletes the old scheduled task (best-effort).
func RemoveLegacyTask() {
	_ = exec.Command("schtasks.exe", "/End", "/TN", LegacyTaskName).Run()
	_ = exec.Command("schtasks.exe", "/Delete", "/TN", LegacyTaskName, "/F").Run()
}

// RestartLegacyTask re-starts the old scheduled task (fallback on migration failure).
func RestartLegacyTask() {
	_ = exec.Command("schtasks.exe", "/Run", "/TN", LegacyTaskName).Run()
}

func serviceDoesNotExist(err error) bool {
	return errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST)
}

func stateLabel(st svc.State) string {
	switch st {
	case svc.Running:
		return "running"
	case svc.Stopped:
		return "stopped"
	case svc.StartPending:
		return "start_pending"
	case svc.StopPending:
		return "stop_pending"
	default:
		return "unknown"
	}
}
