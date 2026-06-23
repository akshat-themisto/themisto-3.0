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
	})
	if got["agent_version"] != "2.0.0" || got["gateway_connected"] != true {
		t.Fatalf("operational fields were removed: %#v", got)
	}
	if _, ok := got["request_body"]; ok {
		t.Fatal("request body crossed operational telemetry boundary")
	}
	if _, ok := got["prompt_text"]; ok {
		t.Fatal("prompt text crossed operational telemetry boundary")
	}
}
