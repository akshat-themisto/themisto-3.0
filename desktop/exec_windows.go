//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// newHiddenCommand starts child console tools without flashing a window in the
// desktop app.
func newHiddenCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	return cmd
}
