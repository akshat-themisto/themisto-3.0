package handler

import (
	"testing"

	"github.com/themisto/gateway/internal/store"
)

func TestMapDecision(t *testing.T) {
	tests := []struct {
		name     string
		action   string
		decision string
		want     string
	}{
		{name: "action allow", action: "allow", decision: "block", want: "forward"},
		{name: "action block", action: "block", decision: "allow", want: "block"},
		{name: "action alert", action: "alert", decision: "allow", want: "alert"},
		{name: "legacy log_only", action: "", decision: "log_only", want: "alert"},
		{name: "legacy allow", action: "", decision: "allow", want: "forward"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapDecision(tt.action, tt.decision)
			if got != tt.want {
				t.Fatalf("mapDecision(%q,%q)=%q, want %q", tt.action, tt.decision, got, tt.want)
			}
		})
	}
}

func TestConvertRules_ConditionsPreferred(t *testing.T) {
	rules := []store.PolicyRule{
		{
			ID:       "rule-1",
			Priority: 10,
			Action:   "block",
			Conditions: []store.PolicyCondition{
				{Field: "ai_vendor", Operator: "eq", Value: "openai"},
				{Field: "body_contains_pii", Operator: "eq", Value: "true"},
			},
		},
	}

	payload := convertRules(rules)
	if len(payload.Rules) != 1 {
		t.Fatalf("rules len = %d, want 1", len(payload.Rules))
	}
	if payload.Rules[0].Decision != "block" {
		t.Fatalf("decision = %q, want block", payload.Rules[0].Decision)
	}
	if len(payload.Rules[0].Conditions) != 2 {
		t.Fatalf("conditions len = %d, want 2", len(payload.Rules[0].Conditions))
	}
	if payload.Rules[0].Conditions[0].Field != "ai_vendor" {
		t.Fatalf("first condition field = %q, want ai_vendor", payload.Rules[0].Conditions[0].Field)
	}
}

func TestConvertRules_LegacyFallback(t *testing.T) {
	host := "openai.com"
	path := "/v1/chat/completions"
	method := "POST"

	rules := []store.PolicyRule{
		{
			ID:          "rule-legacy",
			Priority:    20,
			Decision:    "block",
			MatchHost:   &host,
			MatchPath:   &path,
			MatchMethod: &method,
		},
	}

	payload := convertRules(rules)
	if len(payload.Rules) != 1 {
		t.Fatalf("rules len = %d, want 1", len(payload.Rules))
	}
	if len(payload.Rules[0].Conditions) != 3 {
		t.Fatalf("conditions len = %d, want 3", len(payload.Rules[0].Conditions))
	}
}

func TestDefaultInterceptionConfig(t *testing.T) {
	cfg := defaultInterceptionConfig([]string{"api.openai.com", "claude.ai"})
	if cfg.Enabled {
		t.Fatal("expected interception disabled")
	}
	if cfg.FailMode != "fail_open" {
		t.Fatalf("fail mode = %q, want fail_open", cfg.FailMode)
	}
	if cfg.CaptureMode != "encrypted_full_body" {
		t.Fatalf("capture mode = %q, want encrypted_full_body", cfg.CaptureMode)
	}
	if cfg.Scope != "api_only" {
		t.Fatalf("scope = %q, want api_only", cfg.Scope)
	}
	if len(cfg.Protocols) == 0 {
		t.Fatal("expected protocol defaults")
	}
	if len(cfg.Domains) != 2 {
		t.Fatalf("domains len = %d, want 2", len(cfg.Domains))
	}
}
