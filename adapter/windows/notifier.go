//go:build windows

package windows

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/themisto/agent/pkg/log"
)

type winNotifier struct {
	log log.Logger
}

func newNotifier(logger log.Logger) *winNotifier {
	return &winNotifier{log: logger}
}

func (n *winNotifier) Notify(ctx context.Context, title, message string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Themisto"
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}

	// Best-effort popup; falls back to eventless success when UI session is unavailable.
	script := fmt.Sprintf(`[void][Reflection.Assembly]::LoadWithPartialName('PresentationFramework'); [System.Windows.MessageBox]::Show('%s','%s')`,
		escapePowerShellSingleQuoted(message),
		escapePowerShellSingleQuoted(title),
	)
	if err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script).Run(); err != nil {
		n.log.Debug("windows notifier failed", "error", err)
	}
	return nil
}

func escapePowerShellSingleQuoted(v string) string {
	v = strings.ReplaceAll(v, "'", "''")
	v = strings.ReplaceAll(v, "\n", " ")
	return v
}
