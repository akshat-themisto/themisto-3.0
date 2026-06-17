//go:build windows

package main

import "testing"

func TestResolveInstallerEnrollmentLink(t *testing.T) {
	t.Run("keeps full url", func(t *testing.T) {
		got, err := resolveInstallerEnrollmentLink("https://backend.example/enroll/abc123", map[string]interface{}{})
		if err != nil {
			t.Fatalf("resolveInstallerEnrollmentLink() unexpected error: %v", err)
		}
		if got != "https://backend.example/enroll/abc123" {
			t.Fatalf("resolveInstallerEnrollmentLink() = %q", got)
		}
	})

	t.Run("builds from short code when backend exists", func(t *testing.T) {
		got, err := resolveInstallerEnrollmentLink("abc123", map[string]interface{}{
			"backend_url": "http://localhost:8443",
		})
		if err != nil {
			t.Fatalf("resolveInstallerEnrollmentLink() unexpected error: %v", err)
		}
		if got != "http://localhost:8443/enroll/abc123" {
			t.Fatalf("resolveInstallerEnrollmentLink() = %q", got)
		}
	})

	t.Run("requires full url when backend missing", func(t *testing.T) {
		_, err := resolveInstallerEnrollmentLink("abc123", map[string]interface{}{})
		if err == nil {
			t.Fatal("expected error when backend url is missing")
		}
	})
}

func TestMergeInstallerEnrollmentConfig(t *testing.T) {
	merged := mergeInstallerEnrollmentConfig(map[string]interface{}{
		"default_decision": "forward",
		"custom_key":       "keep-me",
	}, map[string]interface{}{
		"device_id":         "device-123",
		"org_name":          "Themisto Dev Org",
		"backend_url":       "http://localhost:8443",
		"enrollment_token":  "token-123",
		"gateway_url":       "https://localhost",
	})

	if got := getString(merged, "agent_id"); got != "device-123" {
		t.Fatalf("agent_id = %q, want device-123", got)
	}
	if got := getString(merged, "org_name"); got != "Themisto Dev Org" {
		t.Fatalf("org_name = %q", got)
	}
	if got := getString(merged, "custom_key"); got != "keep-me" {
		t.Fatalf("custom_key = %q, want existing value preserved", got)
	}
	if got := getString(merged, "cert_path"); got == "" {
		t.Fatal("expected cert_path default to be populated")
	}
	if got := getString(merged, "key_path"); got == "" {
		t.Fatal("expected key_path default to be populated")
	}
	if got := getString(merged, "ca_path"); got == "" {
		t.Fatal("expected ca_path default to be populated")
	}
}
