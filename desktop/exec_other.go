//go:build !windows

package main

import "os/exec"

func newHiddenCommand(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
