package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectChromiumExtensionFromProfilesManifestMatch(t *testing.T) {
	base := t.TempDir()
	manifestPath := filepath.Join(base, "Profile 2", "Extensions", "real-extension-id", "1.0.0_0", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		t.Fatalf("mkdir manifest dir: %v", err)
	}
	manifest := `{
  "name": "Themisto Prompt Capture (Chromium)",
  "description": "Pre-send prompt capture and enforcement for managed AI web apps."
}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	got := detectChromiumExtensionFromProfiles(base)
	if got.err != nil {
		t.Fatalf("detectChromiumExtensionFromProfiles err = %v", got.err)
	}
	if !got.artifactDetected {
		t.Fatalf("detectChromiumExtensionFromProfiles artifactDetected = false, want true")
	}
}

func TestDetectChromiumExtensionFromProfilesPreferencesMatch(t *testing.T) {
	base := t.TempDir()
	prefsPath := filepath.Join(base, "Default", "Preferences")
	if err := os.MkdirAll(filepath.Dir(prefsPath), 0755); err != nil {
		t.Fatalf("mkdir prefs dir: %v", err)
	}
	prefs := `{"extensions":{"settings":{"abcdef":{"manifest":{"name":"Themisto Prompt Capture (Chromium)","description":"Pre-send prompt capture and enforcement for managed AI web apps."}}}}}`
	if err := os.WriteFile(prefsPath, []byte(prefs), 0644); err != nil {
		t.Fatalf("write preferences: %v", err)
	}

	got := detectChromiumExtensionFromProfiles(base)
	if got.err != nil {
		t.Fatalf("detectChromiumExtensionFromProfiles err = %v", got.err)
	}
	if !got.artifactDetected {
		t.Fatalf("detectChromiumExtensionFromProfiles artifactDetected = false, want true")
	}
}

func TestDetectChromiumExtensionFromProfilesSecurePreferencesPathMatch(t *testing.T) {
	base := t.TempDir()
	prefsPath := filepath.Join(base, "Default", "Secure Preferences")
	if err := os.MkdirAll(filepath.Dir(prefsPath), 0755); err != nil {
		t.Fatalf("mkdir prefs dir: %v", err)
	}
	prefs := `{"extensions":{"settings":{"agabpohdpjdaienfdaopfmlocmgmdoma":{"location":4,"path":"/Users/test/Documents/Themisto/adapter/prompt/browser/chromium","active_permissions":{"explicit_host":["http://127.0.0.1:17175/*"]}}}}}`
	if err := os.WriteFile(prefsPath, []byte(prefs), 0644); err != nil {
		t.Fatalf("write secure preferences: %v", err)
	}

	got := detectChromiumExtensionFromProfiles(base)
	if got.err != nil {
		t.Fatalf("detectChromiumExtensionFromProfiles err = %v", got.err)
	}
	if !got.artifactDetected {
		t.Fatalf("detectChromiumExtensionFromProfiles artifactDetected = false, want true")
	}
}

func TestDetectFirefoxExtensionFromProfilesMetadataMatch(t *testing.T) {
	base := t.TempDir()
	metadataPath := filepath.Join(base, "abc.default-release", "extensions.json")
	if err := os.MkdirAll(filepath.Dir(metadataPath), 0755); err != nil {
		t.Fatalf("mkdir firefox profile dir: %v", err)
	}
	metadata := `{"addons":[{"id":"themisto-prompt-capture@themisto.local","defaultLocale":{"name":"Themisto Prompt Capture"}}]}`
	if err := os.WriteFile(metadataPath, []byte(metadata), 0644); err != nil {
		t.Fatalf("write firefox metadata: %v", err)
	}

	got := detectFirefoxExtensionFromProfiles(base)
	if got.err != nil {
		t.Fatalf("detectFirefoxExtensionFromProfiles err = %v", got.err)
	}
	if !got.artifactDetected {
		t.Fatalf("detectFirefoxExtensionFromProfiles artifactDetected = false, want true")
	}
}
