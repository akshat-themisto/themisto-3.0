package handler

import "testing"

func TestAgentStatusEventPrivacyBoundary(t *testing.T) {
	if !isAgentStatusEvent("agent.heartbeat") || !isAgentStatusEvent("proxy.tamper_detected") {
		t.Fatal("expected operational events to be accepted")
	}
	if isAgentStatusEvent("prompt.captured") || isAgentStatusEvent("browser.form") {
		t.Fatal("content-bearing events must not enter agent status storage")
	}

	got := sanitizedAgentStatusData(map[string]interface{}{
		"agent_version":     "2.0.0",
		"request_body":      "sensitive content",
		"prompt_text":       "customer data",
		"gateway_connected": true,
		"surface_states": map[string]interface{}{
			"browser_chromium": "hard_block",
			"cursor": map[string]interface{}{
				"prompt_text": "customer secret",
			},
			"unknown_surface": "hard_block",
			"claude_code":     "send_body_to_gateway",
		},
	})
	if got["agent_version"] != "2.0.0" || got["gateway_connected"] != true {
		t.Fatalf("operational fields were removed: %#v", got)
	}
	surfaceStates, ok := got["surface_states"].(map[string]string)
	if !ok {
		t.Fatalf("surface states were not preserved as a sanitized map: %#v", got["surface_states"])
	}
	if surfaceStates["browser_chromium"] != "hard_block" {
		t.Fatalf("expected browser_chromium state to survive sanitizer: %#v", surfaceStates)
	}
	if _, ok := surfaceStates["unknown_surface"]; ok {
		t.Fatal("unknown surface crossed operational telemetry boundary")
	}
	if _, ok := surfaceStates["claude_code"]; ok {
		t.Fatal("unknown surface state crossed operational telemetry boundary")
	}
	if _, ok := surfaceStates["cursor"]; ok {
		t.Fatal("nested surface state crossed operational telemetry boundary")
	}
	if _, ok := got["request_body"]; ok {
		t.Fatal("request body crossed operational telemetry boundary")
	}
	if _, ok := got["prompt_text"]; ok {
		t.Fatal("prompt text crossed operational telemetry boundary")
	}
}
