//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func checkWindsurfRegistry() (found bool, displayName string) {
	paths := []struct {
		root registry.Key
		path string
	}{
		{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
	}

	for _, candidate := range paths {
		key, err := registry.OpenKey(candidate.root, candidate.path, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		names, err := key.ReadSubKeyNames(-1)
		key.Close()
		if err != nil {
			continue
		}

		for _, name := range names {
			subKey, err := registry.OpenKey(candidate.root, candidate.path+`\`+name, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			value, _, err := subKey.GetStringValue("DisplayName")
			subKey.Close()
			if err == nil && isWindsurfUninstallEntry(value) {
				return true, value
			}
		}
	}

	return false, ""
}

func checkWindsurfCommonPaths() (found bool, path string) {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		home := os.Getenv("USERPROFILE")
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		localAppData = filepath.Join(home, "AppData", "Local")
	}

	candidates := []string{
		filepath.Join(localAppData, "Programs", "Windsurf", "Windsurf.exe"),
		filepath.Join(localAppData, "Programs", "windsurf", "Windsurf.exe"),
		filepath.Join(localAppData, "Windsurf", "Windsurf.exe"),
		filepath.Join(localAppData, "windsurf", "Windsurf.exe"),
	}

	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return true, candidate
		}
	}

	return false, ""
}

func isWindsurfUninstallEntry(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	return normalized == "windsurf" || strings.HasPrefix(normalized, "windsurf ")
}
