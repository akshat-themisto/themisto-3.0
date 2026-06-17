package domain

import (
	"encoding/json"
	"testing"
)

func TestDecision_JSONRoundtrip(t *testing.T) {
	cases := []struct {
		decision Decision
		jsonStr  string
	}{
		{DecisionForward, `"forward"`},
		{DecisionBypass, `"bypass"`},
		{DecisionBlock, `"block"`},
		{DecisionAlert, `"alert"`},
	}

	for _, tc := range cases {
		b, err := json.Marshal(tc.decision)
		if err != nil {
			t.Fatalf("marshal %v: %v", tc.decision, err)
		}
		if string(b) != tc.jsonStr {
			t.Errorf("marshal %v = %s, want %s", tc.decision, b, tc.jsonStr)
		}

		var d Decision
		if err := json.Unmarshal([]byte(tc.jsonStr), &d); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.jsonStr, err)
		}
		if d != tc.decision {
			t.Errorf("unmarshal %s = %v, want %v", tc.jsonStr, d, tc.decision)
		}
	}
}

// TestDecision_UnmarshalAliases ensures the gateway's vocabulary ("allow", "log_only")
// maps correctly to agent decisions. This was the root cause of the policy fetch crash.
func TestDecision_UnmarshalAliases(t *testing.T) {
	cases := []struct {
		input    string
		expected Decision
	}{
		{`"forward"`, DecisionForward},
		{`"allow"`, DecisionForward}, // backend uses "allow"
		{`"bypass"`, DecisionBypass},
		{`"log_only"`, DecisionBypass}, // backend uses "log_only"
		{`"block"`, DecisionBlock},
		{`"alert"`, DecisionAlert},
		{`"unknown"`, DecisionForward}, // unknown defaults to forward (fail open)
	}

	for _, tc := range cases {
		var d Decision
		if err := json.Unmarshal([]byte(tc.input), &d); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.input, err)
		}
		if d != tc.expected {
			t.Errorf("unmarshal %s = %v, want %v", tc.input, d, tc.expected)
		}
	}
}

// TestPolicyPayload_Unmarshal verifies the exact JSON shape the gateway sends
// can be decoded without error on both macOS and Windows.
func TestPolicyPayload_Unmarshal(t *testing.T) {
	raw := `{"version":"v1","rules":[
		{"id":"abc-123","priority":10,"decision":"block","conditions":[
			{"field":"host","operator":"contains","value":"youtube.com"}
		]},
		{"id":"def-456","priority":20,"decision":"allow","conditions":[]}
	]}`

	var payload PolicyPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Version != "v1" {
		t.Errorf("version = %q, want v1", payload.Version)
	}
	if len(payload.Rules) != 2 {
		t.Fatalf("rules len = %d, want 2", len(payload.Rules))
	}
	if payload.Rules[0].Decision != DecisionBlock {
		t.Errorf("rule[0].decision = %v, want block", payload.Rules[0].Decision)
	}
	if payload.Rules[1].Decision != DecisionForward {
		t.Errorf("rule[1].decision = %v, want forward", payload.Rules[1].Decision)
	}
}

// TestEventPayload_JSONKeys ensures EventPayload serialises with lowercase snake_case
// keys so the gateway's telemetry handler can parse the "data" field correctly.
// Without json tags, Go used "Data" (uppercase) which the gateway silently ignored,
// causing all agent telemetry — including blocked requests — to be dropped.
func TestEventPayload_JSONKeys(t *testing.T) {
	payload := EventPayload{
		AgentID: "test-agent",
		Data: map[string]interface{}{
			"host":     "youtube.com",
			"decision": "block",
		},
	}

	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal map: %v", err)
	}

	if _, ok := m["data"]; !ok {
		t.Error(`EventPayload.Data must marshal as "data" (lowercase) — gateway reads "data"`)
	}
	if _, ok := m["agent_id"]; !ok {
		t.Error(`EventPayload.AgentID must marshal as "agent_id"`)
	}
	if _, ok := m["Data"]; ok {
		t.Error(`EventPayload.Data must NOT marshal as "Data" (uppercase breaks gateway parsing)`)
	}
}
