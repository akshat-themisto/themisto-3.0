//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
)

var errInstallerCancelled = errors.New("installer cancelled")

func runInstall() error {
	return fmt.Errorf("the Themisto installer is only supported on Windows")
}

func runUninstall() error {
	return fmt.Errorf("the Themisto uninstaller is only supported on Windows")
}

func reportInstallerError(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
}
