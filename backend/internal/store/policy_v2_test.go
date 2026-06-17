package store

import "testing"

func ptrString(v string) *string {
	return &v
}

func TestNormalizePolicyInput_FromLegacyFields(t *testing.T) {
	in := PolicyRuleV2Input{
		Priority:    0,
		Action:      "",
		Decision:    "block",
		MatchHost:   ptrString("api.openai.com"),
		MatchPath:   ptrString("/v1/chat/completions"),
		MatchMethod: ptrString("POST"),
	}

	out, err := normalizePolicyInput(in)
	if err != nil {
		t.Fatalf("normalizePolicyInput returned error: %v", err)
	}

	if out.Priority != 10 {
		t.Fatalf("priority = %d, want 10", out.Priority)
	}
	if out.Name != "Policy rule 10" {
		t.Fatalf("name = %q, want %q", out.Name, "Policy rule 10")
	}
	if out.Action != "block" {
		t.Fatalf("action = %q, want %q", out.Action, "block")
	}
	if len(out.Conditions) != 3 {
		t.Fatalf("conditions = %d, want 3", len(out.Conditions))
	}
}

func TestNormalizePolicyInput_InvalidAction(t *testing.T) {
	_, err := normalizePolicyInput(PolicyRuleV2Input{
		Name:       "bad action",
		Priority:   10,
		Action:     "drop",
		Conditions: []PolicyCondition{{Field: "host", Operator: "contains", Value: "openai.com"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid action")
	}
}

func TestLegacyFieldsFromConditions(t *testing.T) {
	host, path, method := legacyFieldsFromConditions([]PolicyCondition{
		{Field: "host", Operator: "contains", Value: "openai.com"},
		{Field: "path", Operator: "prefix", Value: "/v1"},
		{Field: "method", Operator: "eq", Value: "post"},
		{Field: "ai_vendor", Operator: "eq", Value: "openai"},
	})

	if host == nil || *host != "openai.com" {
		t.Fatalf("host = %v, want %q", host, "openai.com")
	}
	if path == nil || *path != "/v1" {
		t.Fatalf("path = %v, want %q", path, "/v1")
	}
	if method == nil || *method != "POST" {
		t.Fatalf("method = %v, want %q", method, "POST")
	}
}
