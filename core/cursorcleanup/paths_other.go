//go:build !windows && !darwin

package cursorcleanup

import (
	"os"
	"path/filepath"
)

// DefaultPaths returns the platform-specific Cursor storage paths for Linux.
type DefaultPaths struct{}

func (DefaultPaths) WorkspaceStorageRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "Cursor", "User", "workspaceStorage"), nil
}

func (DefaultPaths) TranscriptsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cursor", "projects"), nil
}
