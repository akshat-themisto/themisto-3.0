package handler

import "testing"

func TestNormalizePolicyDecision(t *testing.T) {
	tests := []struct {
		in   string
		out  string
		want bool
	}{
		{"allow", "allow", true},
		{"forward", "allow", true},
		{"bypass", "allow", true},
		{"block", "block", true},
		{"deny", "block", true},
		{"log_only", "log_only", true},
		{"log-only", "log_only", true},
		{"unknown", "", false},
	}

	for _, tc := range tests {
		got, ok := normalizePolicyDecision(tc.in)
		if ok != tc.want {
			t.Fatalf("normalizePolicyDecision(%q) ok=%v want=%v", tc.in, ok, tc.want)
		}
		if got != tc.out {
			t.Fatalf("normalizePolicyDecision(%q)=%q want=%q", tc.in, got, tc.out)
		}
	}
}

func TestSanitizedRuleID(t *testing.T) {
	valid := "a0000000-0000-0000-0000-000000000001"
	got := sanitizedRuleID(valid)
	if got == nil || *got != valid {
		t.Fatalf("expected valid UUID, got %v", got)
	}

	invalid := sanitizedRuleID("block-youtube")
	if invalid != nil {
		t.Fatalf("expected nil for non-UUID rule id, got %v", *invalid)
	}
}
