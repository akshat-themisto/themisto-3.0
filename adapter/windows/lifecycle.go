//go:build windows

package windows

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/pkg/log"
	"github.com/themisto/agent/pkg/winsvc"
)

// winServiceManager implements iface.ServiceManager using the Windows
// Service Control Manager. SCM create/update/delete/query/recovery operations
// are delegated to the shared pkg/winsvc package.
type winServiceManager struct {
	log     log.Logger
	readyFn func()          // set during Start, called by the service handler
	ctx     context.Context
	cancel  context.CancelFunc
}

func newServiceManager(logger log.Logger) *winServiceManager {
	return &winServiceManager{log: logger}
}

// ---------------------------------------------------------------------------
// ServiceManager interface
// ---------------------------------------------------------------------------

func (sm *winServiceManager) Install(ctx context.Context) error {
	exePath := `C:\Program Files\Themisto\themisto-agent.exe`
	configPath := `C:\ProgramData\Themisto\agent.json`
	if err := winsvc.InstallOrUpdateService(exePath, configPath); err != nil {
		return fmt.Errorf("install service: %w", err)
	}
	sm.log.Info("service installed", "name", winsvc.ServiceName)
	return nil
}

func (sm *winServiceManager) Start(ctx context.Context, ready func()) error {
	// Detect if running as a Windows Service.
	isService, err := svc.IsWindowsService()
	if err != nil {
		sm.log.Warn("cannot detect service mode, assuming console", "error", err)
		isService = false
	}

	if isService {
		sm.readyFn = ready
		sm.ctx, sm.cancel = context.WithCancel(ctx)
		return svc.Run(winsvc.ServiceName, sm)
	}

	// Console mode (development/testing).
	ready()
	<-ctx.Done()
	return nil
}

func (sm *winServiceManager) Stop(ctx context.Context) error {
	timeout := 30 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}
	if err := winsvc.StopServiceAndWait(timeout); err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
	sm.log.Info("service stopped")
	return nil
}

func (sm *winServiceManager) Uninstall(ctx context.Context) error {
	if err := winsvc.RemoveService(); err != nil {
		return fmt.Errorf("uninstall service: %w", err)
	}
	sm.log.Info("service uninstalled")
	return nil
}

func (sm *winServiceManager) Status() (domain.ServiceStatus, error) {
	state, _ := winsvc.QueryState()
	switch state {
	case "running":
		return domain.ServiceRunning, nil
	case "stopped":
		return domain.ServiceStopped, nil
	case "not_found":
		return domain.ServiceNotInstalled, nil
	case "start_pending", "stop_pending":
		return domain.ServiceRunning, nil // transitioning
	default:
		return domain.ServiceUnknown, nil
	}
}

// ---------------------------------------------------------------------------
// svc.Handler implementation — called by svc.Run when running as a service
// in dev/interactive mode via the adapter's Start() contract.
// ---------------------------------------------------------------------------

func (sm *winServiceManager) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}

	if sm.readyFn != nil {
		sm.readyFn()
	}

	changes <- svc.Status{
		State:   svc.Running,
		Accepts: svc.AcceptStop | svc.AcceptShutdown,
	}

	sm.log.Info("windows service running")

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				sm.log.Info("service stop/shutdown requested")
				if sm.cancel != nil {
					sm.cancel()
				}
				return false, 0
			case svc.Interrogate:
				changes <- c.CurrentStatus
			}
		case <-sm.ctx.Done():
			changes <- svc.Status{State: svc.StopPending}
			return false, 0
		}
	}
}

// runCmd executes a command and returns an error if it fails.
func runCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return nil
}
