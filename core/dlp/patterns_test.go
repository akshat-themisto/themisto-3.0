package dlp

import (
	"strings"
	"testing"
)

func TestRedactionPatternsReturnsAll(t *testing.T) {
	patterns := RedactionPatterns()
	if len(patterns) == 0 {
		t.Fatal("RedactionPatterns() returned empty")
	}

	hasPII := false
	hasCred := false
	for _, p := range patterns {
		if p.Name == "" {
			t.Error("pattern has empty Name")
		}
		if p.Re == nil {
			t.Errorf("pattern %q has nil regex", p.Name)
		}
		if p.Type != "pii" && p.Type != "credential" {
			t.Errorf("pattern %q has unexpected type %q", p.Name, p.Type)
		}
		if p.Type == "pii" {
			hasPII = true
		}
		if p.Type == "credential" {
			hasCred = true
		}
	}
	if !hasPII {
		t.Error("no PII patterns returned")
	}
	if !hasCred {
		t.Error("no credential patterns returned")
	}
}

func TestRedactionPatternsMatchSSN(t *testing.T) {
	patterns := RedactionPatterns()
	for _, p := range patterns {
		if p.Name == "ssn" {
			if !p.Re.MatchString("my ssn is 123-45-6789 ok") {
				t.Error("SSN pattern did not match formatted SSN")
			}
			return
		}
	}
	t.Error("ssn pattern not found in RedactionPatterns()")
}

func TestReplacementConstant(t *testing.T) {
	if !strings.HasPrefix(Replacement, "[") || !strings.HasSuffix(Replacement, "]") {
		t.Errorf("Replacement %q should be bracketed", Replacement)
	}
}
