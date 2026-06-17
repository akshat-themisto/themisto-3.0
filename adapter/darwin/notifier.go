//go:build darwin

package darwin

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/themisto/agent/pkg/log"
)

type darwinNotifier struct {
	log log.Logger
}

func newNotifier(logger log.Logger) *darwinNotifier {
	return &darwinNotifier{log: logger}
}

func (n *darwinNotifier) Notify(ctx context.Context, title, message string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Themisto"
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}

	script := fmt.Sprintf(`display notification "%s" with title "%s"`,
		escapeAppleScript(message),
		escapeAppleScript(title),
	)
	if err := exec.CommandContext(ctx, "osascript", "-e", script).Run(); err != nil {
		n.log.Debug("darwin notifier failed", "error", err)
	}
	return nil
}

func escapeAppleScript(v string) string {
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, "\"", "\\\"")
	v = strings.ReplaceAll(v, "\n", " ")
	return v
}
