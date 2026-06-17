package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if err := run(); err != nil {
		if errors.Is(err, errInstallerCancelled) {
			os.Exit(0)
		}
		reportInstallerError(err)
		os.Exit(1)
	}
}

func run() error {
	// Detect if running with -uninstall flag
	for _, arg := range os.Args[1:] {
		if arg == "-uninstall" || arg == "--uninstall" {
			return runUninstall()
		}
	}

	// If the binary is named ThemistoUninstall.exe, default to uninstall mode.
	exeName := strings.ToLower(filepath.Base(os.Args[0]))
	if strings.TrimSuffix(exeName, ".exe") == "themistouninstall" {
		return runUninstall()
	}

	return runInstall()
}
