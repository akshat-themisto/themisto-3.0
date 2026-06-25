package api

import "testing"

func TestBuildAgentConfig(t *testing.T) {
	cfg := buildAgentConfig(
		"http://control.example.com:8443",
		"https://gateway.example.com",
		"device-123",
		"Acme",
		"token-xyz",
	)

	assertEqual := func(name, got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s: got %q want %q", name, got, want)
		}
	}

	assertEqual("agent_id", cfg.AgentID, "device-123")
	assertEqual("backend_url", cfg.BackendURL, "http://control.example.com:8443")
	assertEqual("gateway_url", cfg.GatewayURL, "https://gateway.example.com")
	assertEqual("device_id", cfg.DeviceID, "device-123")
	assertEqual("org_name", cfg.OrgName, "Acme")
	assertEqual("enrollment_token", cfg.EnrollmentToken, "token-xyz")
	assertEqual("listen_addr", cfg.ListenAddr, "127.0.0.1:8080")
	assertEqual("default_decision", cfg.DefaultDecision, "bypass")
	assertEqual("telemetry_flush_interval", cfg.TelemetryFlushInterval, "5s")
	if !cfg.PromptCaptureEnabled || !cfg.PromptSemanticsEnabled {
		t.Fatalf("prompt capture/semantics should be enabled in generated enterprise config: %#v", cfg)
	}
	assertEqual("prompt_enforcement_mode", cfg.PromptEnforcementMode, "enforce")
	if got, want := len(cfg.PromptFailClosed), 5; got != want {
		t.Fatalf("prompt_fail_closed_surfaces length: got %d want %d", got, want)
	}
}
