//go:build darwin

package darwin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/themisto/agent/core/adapter/iface"
	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/pkg/log"
)

const (
	serviceLabel    = "com.themisto.agent"
	plistDir        = "/Library/LaunchDaemons"
	plistFilename   = serviceLabel + ".plist"
	agentBinary     = "/usr/local/bin/themisto-agent"
	agentConfigPath = "/etc/themisto/agent.json"
	logDir          = "/var/log/themisto"
)

// darwinServiceManager implements iface.ServiceManager using launchd.
type darwinServiceManager struct {
	log log.Logger
}

func newServiceManager(logger log.Logger) *darwinServiceManager {
	return &darwinServiceManager{log: logger}
}

// Install writes the launchd plist and bootstraps the service.
func (sm *darwinServiceManager) Install(ctx context.Context) error {
	plistPath := filepath.Join(plistDir, plistFilename)

	plist := sm.generatePlist()

	// Read existing plist; if identical, skip (idempotent).
	if existing, err := os.ReadFile(plistPath); err == nil {
		if string(existing) == plist {
			sm.log.Debug("plist unchanged, skipping install")
			return nil
		}
		// Content differs — overwrite and re-bootstrap.
		sm.log.Info("plist changed, updating")
	}

	// Ensure log directory exists.
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	if err := os.WriteFile(plistPath, []byte(plist), 0644); err != nil {
		return fmt.Errorf("write plist: %w: %w", err, iface.ErrPermission)
	}

	// Bootstrap the service (load plist into launchd).
	_ = runCmd(ctx, "launchctl", "bootout", "system", plistPath) // remove if already loaded
	if err := runCmd(ctx, "launchctl", "bootstrap", "system", plistPath); err != nil {
		return fmt.Errorf("bootstrap service: %w: %w", err, iface.ErrPermission)
	}

	sm.log.Info("service installed", "plist", plistPath)
	return nil
}

// Start runs the service scaffold. If already inside launchd, it calls ready
// and blocks. Otherwise it kicks the service via launchctl.
func (sm *darwinServiceManager) Start(ctx context.Context, ready func()) error {
	// Detect if we're running inside launchd (ppid == 1 on macOS when
	// launched by launchd).
	if os.Getppid() == 1 {
		sm.log.Info("running inside launchd, signalling ready")
		ready()
		<-ctx.Done()
		return nil
	}

	// Not inside launchd — kick the service.
	if err := runCmd(ctx, "launchctl", "kickstart", "-k", "system/"+serviceLabel); err != nil {
		sm.log.Warn("kickstart failed, trying start", "error", err)
		_ = runCmd(ctx, "launchctl", "start", serviceLabel)
	}

	ready()
	<-ctx.Done()
	return nil
}

// Stop signals the service to shut down.
func (sm *darwinServiceManager) Stop(ctx context.Context) error {
	status, _ := sm.Status()
	if status == domain.ServiceStopped || status == domain.ServiceNotInstalled {
		return nil
	}

	plistPath := filepath.Join(plistDir, plistFilename)
	if err := runCmd(ctx, "launchctl", "bootout", "system", plistPath); err != nil {
		// Fallback: send SIGTERM via PID.
		if pid := sm.findPID(ctx); pid > 0 {
			proc, perr := os.FindProcess(pid)
			if perr == nil {
				_ = proc.Signal(os.Interrupt)
			}
		}
	}

	sm.log.Info("service stopped")
	return nil
}

// Uninstall removes the plist and log files.
func (sm *darwinServiceManager) Uninstall(ctx context.Context) error {
	status, _ := sm.Status()
	if status == domain.ServiceRunning || status == domain.ServiceDegraded {
		return fmt.Errorf("service is still running: %w", iface.ErrConflict)
	}

	plistPath := filepath.Join(plistDir, plistFilename)

	// Bootout if still registered.
	_ = runCmd(ctx, "launchctl", "bootout", "system", plistPath)

	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}

	// Clean up log files.
	_ = os.RemoveAll(logDir)

	sm.log.Info("service uninstalled")
	return nil
}

// Status queries launchd for the service state.
func (sm *darwinServiceManager) Status() (domain.ServiceStatus, error) {
	out, err := exec.Command("launchctl", "print", "system/"+serviceLabel).CombinedOutput()
	if err != nil {
		output := string(out)
		if strings.Contains(output, "Could not find service") {
			return domain.ServiceNotInstalled, nil
		}
		return domain.ServiceUnknown, nil
	}

	output := string(out)

	// Look for "pid = <n>" to determine if running.
	if pid := extractField(output, "pid = "); pid != "" {
		p, _ := strconv.Atoi(pid)
		if p > 0 {
			// Check last exit status for degraded detection.
			if exitStatus := extractField(output, "last exit code = "); exitStatus != "" && exitStatus != "0" {
				return domain.ServiceDegraded, nil
			}
			return domain.ServiceRunning, nil
		}
	}

	// Registered but no PID.
	return domain.ServiceStopped, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func (sm *darwinServiceManager) generatePlist() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + serviceLabel + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + agentBinary + `</string>
		<string>--config</string>
		<string>` + agentConfigPath + `</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ThrottleInterval</key>
	<integer>10</integer>
	<key>StandardOutPath</key>
	<string>` + logDir + `/agent.stdout.log</string>
	<key>StandardErrorPath</key>
	<string>` + logDir + `/agent.stderr.log</string>
	<key>UserName</key>
	<string>root</string>
	<key>GroupName</key>
	<string>wheel</string>
	<key>EnvironmentVariables</key>
	<dict>
		<key>THEMISTO_ENV</key>
		<string>production</string>
	</dict>
</dict>
</plist>
`
}

func (sm *darwinServiceManager) findPID(ctx context.Context) int {
	out, err := exec.CommandContext(ctx, "launchctl", "print", "system/"+serviceLabel).Output()
	if err != nil {
		return 0
	}
	if pid := extractField(string(out), "pid = "); pid != "" {
		p, _ := strconv.Atoi(pid)
		return p
	}
	return 0
}

func extractField(output, prefix string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
		// Handle "key = value" patterns with extra whitespace.
		if idx := strings.Index(line, prefix); idx >= 0 {
			val := strings.TrimSpace(line[idx+len(prefix):])
			// Strip trailing semicolons or braces.
			val = strings.TrimRight(val, ";")
			return strings.TrimSpace(val)
		}
	}
	return ""
}
