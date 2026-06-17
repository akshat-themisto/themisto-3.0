//go:build windows

package cursorcleanup

import (
	"os"
	"path/filepath"
)

// DefaultPaths returns the platform-specific Cursor storage paths for Windows.
type DefaultPaths struct{}

func (DefaultPaths) WorkspaceStorageRoot() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home := os.Getenv("USERPROFILE")
		if home == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return "", err
			}
		}
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(appData, "Cursor", "User", "workspaceStorage"), nil
}

func (DefaultPaths) TranscriptsRoot() (string, error) {
	home := os.Getenv("USERPROFILE")
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(home, ".cursor", "projects"), nil
}
