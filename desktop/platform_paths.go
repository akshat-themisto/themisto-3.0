package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func currentUserHomeDir() string {
	switch runtime.GOOS {
	case "windows":
		if home := strings.TrimSpace(os.Getenv("USERPROFILE")); home != "" {
			return home
		}
	default:
		if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
			return home
		}
	}
	home, _ := os.UserHomeDir()
	return home
}

func userConfigRootDir() string {
	home := currentUserHomeDir()
	switch runtime.GOOS {
	case "windows":
		if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
			return appData
		}
		return filepath.Join(home, "AppData", "Roaming")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support")
	default:
		if cfg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); cfg != "" {
			return cfg
		}
		return filepath.Join(home, ".config")
	}
}

func themistoUserDataDir() string {
	home := currentUserHomeDir()
	switch runtime.GOOS {
	case "windows":
		if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
			return filepath.Join(localAppData, "Themisto")
		}
		return filepath.Join(home, "AppData", "Local", "Themisto")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Themisto")
	default:
		if dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dataHome != "" {
			return filepath.Join(dataHome, "themisto")
		}
		return filepath.Join(home, ".local", "share", "themisto")
	}
}

func themistoHookDir() string {
	// Use a space-free path on macOS so hook commands written into settings.json
	// are not split by the shell when executed. ~/Library/Application Support has
	// a space which causes "/bin/sh: .../Application: No such file or directory".
	if runtime.GOOS == "darwin" {
		return filepath.Join(currentUserHomeDir(), ".themisto", "hooks")
	}
	return filepath.Join(themistoUserDataDir(), "hooks")
}

func platformDisplayName(goos string) string {
	switch goos {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	default:
		return strings.ToUpper(goos)
	}
}
