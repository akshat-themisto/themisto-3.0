//go:build darwin

package cursorcleanup

import (
	"os"
	"path/filepath"
)

// DefaultPaths returns the platform-specific Cursor storage paths for macOS.
type DefaultPaths struct{}

func (DefaultPaths) WorkspaceStorageRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "Cursor", "User", "workspaceStorage"), nil
}

func (DefaultPaths) TranscriptsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cursor", "projects"), nil
}
