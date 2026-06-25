//go:build windows

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareInstalledConfigCanonicalizesRelativeCredentialPaths(t *testing.T) {
	cfg := prepareInstalledConfig(nil, map[string]interface{}{
		"agent_id":    "device-1",
		"gateway_url": "https://localhost",
		"cert_path":   "local-device/device.crt",
		"key_path":    "local-device/device.key",
		"ca_path":     "local-device/ca-chain.pem",
	})

	if got := getString(cfg, "cert_path"); got != filepath.Join(certsDir, "device.crt") {
		t.Fatalf("cert_path = %q, want canonical ProgramData path", got)
	}
	if got := getString(cfg, "key_path"); got != filepath.Join(certsDir, "device.key") {
		t.Fatalf("key_path = %q, want canonical ProgramData path", got)
	}
	if got := getString(cfg, "ca_path"); got != filepath.Join(certsDir, "ca-chain.pem") {
		t.Fatalf("ca_path = %q, want canonical ProgramData path", got)
	}
}

func TestPrepareInstalledConfigAdoptsEmbeddedBootstrapWhenInstalledStateIsIncomplete(t *testing.T) {
	installed := map[string]interface{}{
		"agent_id": "stale-device",
	}
	embedded := map[string]interface{}{
		"agent_id":         "device-42",
		"device_id":        "device-42",
		"gateway_url":      "https://localhost",
		"backend_url":      "http://localhost:8443",
		"org_name":         "Themisto Dev Org",
		"enrollment_token": "token-123",
	}

	cfg := prepareInstalledConfig(installed, embedded)

	for _, key := range []string{"agent_id", "device_id", "gateway_url", "backend_url", "org_name", "enrollment_token"} {
		if got, want := getString(cfg, key), getString(embedded, key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestPrepareInstalledConfigPreservesInstalledBootstrapWhenCredentialFilesExist(t *testing.T) {
	tempDir := t.TempDir()
	certPath := filepath.Join(tempDir, "device.crt")
	keyPath := filepath.Join(tempDir, "device.key")
	caPath := filepath.Join(tempDir, "ca-chain.pem")
	for _, p := range []string{certPath, keyPath, caPath} {
		if err := os.WriteFile(p, []byte("placeholder"), 0644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	installed := map[string]interface{}{
		"agent_id":    "existing-device",
		"device_id":   "existing-device",
		"gateway_url": "https://existing-gateway",
		"cert_path":   certPath,
		"key_path":    keyPath,
		"ca_path":     caPath,
	}
	embedded := map[string]interface{}{
		"agent_id":         "new-device",
		"device_id":        "new-device",
		"gateway_url":      "https://new-gateway",
		"backend_url":      "http://localhost:8443",
		"org_name":         "New Org",
		"enrollment_token": "token-123",
	}

	cfg := prepareInstalledConfig(installed, embedded)

	if got := getString(cfg, "agent_id"); got != "existing-device" {
		t.Fatalf("agent_id = %q, want installed value", got)
	}
	if got := getString(cfg, "gateway_url"); got != "https://existing-gateway" {
		t.Fatalf("gateway_url = %q, want installed value", got)
	}
}

func TestClassifierEmbedExcludesPythonCaches(t *testing.T) {
	err := fs.WalkDir(classifierFS, "assets/classifier", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Name() == "__pycache__" {
			t.Fatalf("classifier embed includes Python cache directory %s", path)
		}
		if strings.HasSuffix(entry.Name(), ".pyc") {
			t.Fatalf("classifier embed includes Python bytecode %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk classifier embed: %v", err)
	}
}
