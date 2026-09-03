package transport

import (
	"testing"

	"github.com/themisto/agent/core/domain"
)

func TestBuildWrapperHeaders_AllFields(t *testing.T) {
	h := BuildWrapperHeaders(
		"agent-x",
		domain.ProcessInfo{PID: 99, Path: "/bin/test", Name: "test", User: "admin", BundleID: "com.test", Signed: true, SignerID: "Test Inc."},
		"eth0", true, "v5", domain.DecisionForward, "ai_llm", "openai", "rule-7", "req-42",
	)

	checks := map[string]string{
		"X-Themisto-Protocol-Version":  domain.ProtocolVersion,
		"X-Themisto-Agent-ID":          "agent-x",
		"X-Themisto-Request-ID":        "req-42",
		"X-Themisto-Decision":          "forward",
		"X-Themisto-Rule-ID":           "rule-7",
		"X-Themisto-Policy-Version":    "v5",
		"X-Themisto-Service-Category":  "ai_llm",
		"X-Themisto-AI-Vendor":         "openai",
		"X-Themisto-Process-PID":       "99",
		"X-Themisto-Process-Name":      "test",
		"X-Themisto-Process-Bundle":    "com.test",
		"X-Themisto-Process-Signed":    "true",
		"X-Themisto-Process-Signer":    "Test Inc.",
		"X-Themisto-Network-Interface": "eth0",
		"X-Themisto-VPN-Active":        "true",
	}

	for key, want := range checks {
		got, ok := h[key]
		if !ok {
			t.Errorf("missing header %s", key)
			continue
		}
		if got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}

	// Timestamp should be present.
	if _, ok := h["X-Themisto-Timestamp"]; !ok {
		t.Error("missing X-Themisto-Timestamp")
	}
	for _, forbidden := range []string{"X-Themisto-Process-Path", "X-Themisto-Process-User"} {
		if _, ok := h[forbidden]; ok {
			t.Errorf("privacy-bearing header %s must be absent", forbidden)
		}
	}
}

func TestBuildWrapperHeaders_ZeroProcess(t *testing.T) {
	h := BuildWrapperHeaders(
		"agent-y", domain.ProcessInfo{}, "", false, "", domain.DecisionBypass, "", "", "", "req-1",
	)

	// Zero-value fields should be absent.
	absent := []string{
		"X-Themisto-Process-PID",
		"X-Themisto-Process-Path",
		"X-Themisto-Process-Name",
		"X-Themisto-Process-User",
		"X-Themisto-Process-Bundle",
		"X-Themisto-Process-Signed",
		"X-Themisto-Process-Signer",
		"X-Themisto-Network-Interface",
		"X-Themisto-VPN-Active",
		"X-Themisto-Rule-ID",
		"X-Themisto-Policy-Version",
		"X-Themisto-Service-Category",
		"X-Themisto-AI-Vendor",
	}
	for _, key := range absent {
		if _, ok := h[key]; ok {
			t.Errorf("header %s should be absent for zero value", key)
		}
	}

	// Required headers still present.
	if h["X-Themisto-Agent-ID"] != "agent-y" {
		t.Error("Agent-ID missing")
	}
	if h["X-Themisto-Decision"] != "bypass" {
		t.Error("Decision missing")
	}
}

func TestBuildWrapperHeaders_DecisionStrings(t *testing.T) {
	for _, tc := range []struct {
		d    domain.Decision
		want string
	}{
		{domain.DecisionForward, "forward"},
		{domain.DecisionBypass, "bypass"},
		{domain.DecisionBlock, "block"},
	} {
		h := BuildWrapperHeaders("a", domain.ProcessInfo{}, "", false, "", tc.d, "", "", "", "r")
		if h["X-Themisto-Decision"] != tc.want {
			t.Errorf("decision %d: got %q, want %q", tc.d, h["X-Themisto-Decision"], tc.want)
		}
	}
}
