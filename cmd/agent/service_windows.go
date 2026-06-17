//go:build windows

package main

import (
	"context"

	"golang.org/x/sys/windows/svc"

	agentcore "github.com/themisto/agent/core"
	"github.com/themisto/agent/pkg/winsvc"
)

// isRunningAsService detects whether the process was started by the Windows
// Service Control Manager. Must be called early in main().
func isRunningAsService() bool {
	is, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return is
}

// runAsService is the service entrypoint invoked from main() when the binary
// is running under SCM. It delegates to svc.Run with an agentServiceHandler.
func runAsService(cfgPath string) error {
	return svc.Run(winsvc.ServiceName, &agentServiceHandler{
		configPath: cfgPath,
		logger:     &stdLogger{},
	})
}

// agentServiceHandler implements svc.Handler, starting the agent lifecycle
// with an SCM-controlled context.
type agentServiceHandler struct {
	configPath string
	logger     *stdLogger
}

func (h *agentServiceHandler) Execute(args []string, changes <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const acceptedCmds = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}

	// Auto-enrollment if config contains an enrollment token.
	if err := maybeActivateFromConfig(h.configPath, h.logger); err != nil {
		h.logger.Warn("automatic activation failed under service", "error", err)
	}

	// Build the agent using the same path as interactive mode.
	src := &fileSource{path: h.configPath}
	adapter := newPlatformAdapter(h.logger)

	agent, err := agentcore.NewAgent(agentcore.Deps{
		Adapter: adapter,
		Source:  src,
		Logger:  h.logger,
	})
	if err != nil {
		h.logger.Error("agent init failed under SCM", "error", err)
		status <- svc.Status{State: svc.StopPending}
		return false, 1
	}

	// SCM-controlled context — no signal.NotifyContext.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agentErrCh := make(chan error, 1)
	go func() {
		agentErrCh <- agent.Start(ctx)
	}()

	// Report running to SCM.
	status <- svc.Status{State: svc.Running, Accepts: acceptedCmds}
	h.logger.Info("agent service running")

	// Wait for SCM commands or agent exit.
	for {
		select {
		case cr := <-changes:
			switch cr.Cmd {
			case svc.Stop, svc.Shutdown:
				h.logger.Info("service stop/shutdown requested")
				status <- svc.Status{State: svc.StopPending}
				cancel()
				<-agentErrCh
				return false, 0
			case svc.Interrogate:
				status <- cr.CurrentStatus
			}
		case err := <-agentErrCh:
			if err != nil {
				h.logger.Error("agent exited with error", "error", err)
				return false, 1
			}
			return false, 0
		}
	}
}
