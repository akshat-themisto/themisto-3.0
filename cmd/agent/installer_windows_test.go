//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalizeInstalledCredentialPathsRewritesRelativePaths(t *testing.T) {
	cfg := map[string]interface{}{
		"cert_path": "local-device/device.crt",
		"key_path":  "local-device/device.key",
		"ca_path":   "local-device/ca-chain.pem",
	}

	canonicalizeInstalledCredentialPaths(cfg)

	if got := getString(cfg, "cert_path"); got != filepath.Join(certsDir, "device.crt") {
		t.Fatalf("cert_path = %q, want canonical install path", got)
	}
	if got := getString(cfg, "key_path"); got != filepath.Join(certsDir, "device.key") {
		t.Fatalf("key_path = %q, want canonical install path", got)
	}
	if got := getString(cfg, "ca_path"); got != filepath.Join(certsDir, "ca-chain.pem") {
		t.Fatalf("ca_path = %q, want canonical install path", got)
	}
}

func TestCanonicalizeInstalledCredentialPathsPreservesAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	cfg := map[string]interface{}{
		"cert_path": filepath.Join(dir, "device.crt"),
		"key_path":  filepath.Join(dir, "device.key"),
		"ca_path":   filepath.Join(dir, "ca-chain.pem"),
	}

	wantCert := getString(cfg, "cert_path")
	wantKey := getString(cfg, "key_path")
	wantCA := getString(cfg, "ca_path")

	canonicalizeInstalledCredentialPaths(cfg)

	if got := getString(cfg, "cert_path"); got != wantCert {
		t.Fatalf("cert_path = %q, want %q", got, wantCert)
	}
	if got := getString(cfg, "key_path"); got != wantKey {
		t.Fatalf("key_path = %q, want %q", got, wantKey)
	}
	if got := getString(cfg, "ca_path"); got != wantCA {
		t.Fatalf("ca_path = %q, want %q", got, wantCA)
	}
}

func TestIsReadyToRunRequiresAllCredentialFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := map[string]interface{}{
		"agent_id":    "device-123",
		"gateway_url": "https://localhost",
		"cert_path":   filepath.Join(dir, "device.crt"),
		"key_path":    filepath.Join(dir, "device.key"),
		"ca_path":     filepath.Join(dir, "ca-chain.pem"),
	}

	if err := os.WriteFile(getString(cfg, "cert_path"), []byte("cert"), 0600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if isReadyToRun(cfg) {
		t.Fatal("expected not ready when key and ca are missing")
	}

	if err := os.WriteFile(getString(cfg, "key_path"), []byte("key"), 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if err := os.WriteFile(getString(cfg, "ca_path"), []byte("ca"), 0600); err != nil {
		t.Fatalf("write ca: %v", err)
	}
	if !isReadyToRun(cfg) {
		t.Fatal("expected ready once cert, key, and ca all exist")
	}
}
