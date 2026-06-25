package policy

import (
	"testing"

	"github.com/themisto/agent/core/domain"
)

func TestEngine_EmptyRulesUsesDefault(t *testing.T) {
	e := NewEngine(domain.DecisionBypass)
	e.Update(&domain.PolicyPayload{Version: "v0", Rules: nil})

	d, rid, err := e.Apply(&domain.RequestContext{Host: "anything.com"})
	if err != nil {
		t.Fatal(err)
	}
	if d != domain.DecisionBypass {
		t.Errorf("decision = %v, want bypass", d)
	}
	if rid != "" {
		t.Errorf("ruleID = %q, want empty", rid)
	}
}

func TestEngine_PriorityOrder(t *testing.T) {
	e := NewEngine(domain.DecisionForward)
	e.Update(&domain.PolicyPayload{
		Version: "v1",
		Rules: []domain.PolicyRule{
			{ID: "low", Priority: 100, Decision: domain.DecisionBypass, Conditions: []domain.RuleCondition{{Field: "host", Operator: "eq", Value: "x.com"}}},
			{ID: "high", Priority: 1, Decision: domain.DecisionBlock, Conditions: []domain.RuleCondition{{Field: "host", Operator: "eq", Value: "x.com"}}},
		},
	})

	d, rid, _ := e.Apply(&domain.RequestContext{Host: "x.com"})
	if d != domain.DecisionBlock {
		t.Errorf("decision = %v, want block (higher priority)", d)
	}
	if rid != "high" {
		t.Errorf("ruleID = %q, want high", rid)
	}
}

func TestEngine_Operators(t *testing.T) {
	tests := []struct {
		name     string
		op       string
		value    string
		host     string
		expected bool
	}{
		{"eq match", "eq", "example.com", "example.com", true},
		{"eq case insensitive", "eq", "Example.COM", "example.com", true},
		{"eq no match", "eq", "other.com", "example.com", false},
		{"contains match", "contains", "exam", "example.com", true},
		{"contains no match", "contains", "xyz", "example.com", false},
		{"prefix match", "prefix", "exam", "example.com", true},
		{"prefix no match", "prefix", "test", "example.com", false},
		{"suffix match", "suffix", ".com", "example.com", true},
		{"suffix no match", "suffix", ".org", "example.com", false},
		{"regex match", "regex", `^ex.*\.com$`, "example.com", true},
		{"regex no match", "regex", `^test`, "example.com", false},
		{"glob match", "glob", "*.com", "example.com", true},
		{"glob no match", "glob", "*.org", "example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEngine(domain.DecisionForward)
			e.Update(&domain.PolicyPayload{
				Version: "v1",
				Rules: []domain.PolicyRule{
					{ID: "r1", Priority: 1, Decision: domain.DecisionBlock, Conditions: []domain.RuleCondition{{Field: "host", Operator: tt.op, Value: tt.value}}},
				},
			})
			d, _, _ := e.Apply(&domain.RequestContext{Host: tt.host})
			matched := d == domain.DecisionBlock
			if matched != tt.expected {
				t.Errorf("matched = %v, want %v", matched, tt.expected)
			}
		})
	}
}

func TestEngine_NegateCondition(t *testing.T) {
	e := NewEngine(domain.DecisionForward)
	e.Update(&domain.PolicyPayload{
		Version: "v1",
		Rules: []domain.PolicyRule{
			{ID: "r1", Priority: 1, Decision: domain.DecisionBlock, Conditions: []domain.RuleCondition{
				{Field: "host", Operator: "eq", Value: "allowed.com", Negate: true},
			}},
		},
	})

	d, _, _ := e.Apply(&domain.RequestContext{Host: "other.com"})
	if d != domain.DecisionBlock {
		t.Error("negated eq should block non-matching host")
	}

	d, _, _ = e.Apply(&domain.RequestContext{Host: "allowed.com"})
	if d != domain.DecisionForward {
		t.Error("negated eq should NOT block matching host")
	}
}

func TestEngine_MultipleConditionsAND(t *testing.T) {
	e := NewEngine(domain.DecisionForward)
	e.Update(&domain.PolicyPayload{
		Version: "v1",
		Rules: []domain.PolicyRule{
			{ID: "r1", Priority: 1, Decision: domain.DecisionBlock, Conditions: []domain.RuleCondition{
				{Field: "host", Operator: "eq", Value: "target.com"},
				{Field: "method", Operator: "eq", Value: "POST"},
			}},
		},
	})

	d, _, _ := e.Apply(&domain.RequestContext{Host: "target.com", Method: "GET"})
	if d != domain.DecisionForward {
		t.Error("should not block GET (method condition fails)")
	}

	d, _, _ = e.Apply(&domain.RequestContext{Host: "target.com", Method: "POST"})
	if d != domain.DecisionBlock {
		t.Error("should block POST to target.com")
	}
}

func TestEngine_ProcessFields(t *testing.T) {
	e := NewEngine(domain.DecisionForward)
	e.Update(&domain.PolicyPayload{
		Version: "v1",
		Rules: []domain.PolicyRule{
			{ID: "r1", Priority: 1, Decision: domain.DecisionBlock, Conditions: []domain.RuleCondition{
				{Field: "process_name", Operator: "eq", Value: "malware"},
			}},
		},
	})

	d, _, _ := e.Apply(&domain.RequestContext{Host: "any.com", Process: domain.ProcessInfo{Name: "malware"}})
	if d != domain.DecisionBlock {
		t.Error("should block process named malware")
	}

	d, _, _ = e.Apply(&domain.RequestContext{Host: "any.com", Process: domain.ProcessInfo{Name: "chrome"}})
	if d != domain.DecisionForward {
		t.Error("should not block chrome")
	}
}

func TestEngine_InvalidRegex(t *testing.T) {
	e := NewEngine(domain.DecisionForward)
	err := e.Update(&domain.PolicyPayload{
		Version: "v1",
		Rules: []domain.PolicyRule{
			{ID: "r1", Priority: 1, Decision: domain.DecisionBlock, Conditions: []domain.RuleCondition{
				{Field: "host", Operator: "regex", Value: "[invalid"},
			}},
		},
	})
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestEngine_NilPayload(t *testing.T) {
	e := NewEngine(domain.DecisionForward)
	err := e.Update(nil)
	if err == nil {
		t.Error("expected error for nil payload")
	}
}

func TestEngine_AtomicUpdate(t *testing.T) {
	e := NewEngine(domain.DecisionForward)
	e.Update(&domain.PolicyPayload{Version: "v1", Rules: []domain.PolicyRule{
		{ID: "r1", Priority: 1, Decision: domain.DecisionBlock, Conditions: []domain.RuleCondition{{Field: "host", Operator: "eq", Value: "a.com"}}},
	}})

	// Bad update should keep v1.
	e.Update(&domain.PolicyPayload{Version: "v2", Rules: []domain.PolicyRule{
		{ID: "r2", Priority: 1, Decision: domain.DecisionBlock, Conditions: []domain.RuleCondition{{Field: "host", Operator: "regex", Value: "[bad"}}},
	}})

	if e.Version() != "v1" {
		t.Errorf("version = %q, want v1 after failed update", e.Version())
	}
}

func TestEngine_BodyConditionFields(t *testing.T) {
	e := NewEngine(domain.DecisionForward)
	err := e.Update(&domain.PolicyPayload{
		Version: "v1",
		Rules: []domain.PolicyRule{
			{
				ID:       "r-body-cred",
				Priority: 1,
				Decision: domain.DecisionBlock,
				Conditions: []domain.RuleCondition{
					{Field: "body_contains_credentials", Operator: "eq", Value: "true"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("update policy: %v", err)
	}

	d, _, _ := e.Apply(&domain.RequestContext{DLP: domain.DLPInfo{ContainsCredentials: true}})
	if d != domain.DecisionBlock {
		t.Errorf("decision = %v, want block when credentials present", d)
	}

	d, _, _ = e.Apply(&domain.RequestContext{DLP: domain.DLPInfo{ContainsCredentials: false}})
	if d != domain.DecisionForward {
		t.Errorf("decision = %v, want forward when credentials absent", d)
	}
}

func TestEngine_AIServiceFields(t *testing.T) {
	e := NewEngine(domain.DecisionForward)
	err := e.Update(&domain.PolicyPayload{
		Version: "v1",
		Rules: []domain.PolicyRule{
			{
				ID:       "r-ai-openai",
				Priority: 1,
				Decision: domain.DecisionBlock,
				Conditions: []domain.RuleCondition{
					{Field: "service_category", Operator: "eq", Value: "ai_llm"},
					{Field: "ai_vendor", Operator: "eq", Value: "openai"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("update policy: %v", err)
	}

	d, _, _ := e.Apply(&domain.RequestContext{ServiceCategory: "ai_llm", AIVendor: "openai"})
	if d != domain.DecisionBlock {
		t.Errorf("decision = %v, want block for openai ai_llm", d)
	}

	d, _, _ = e.Apply(&domain.RequestContext{ServiceCategory: "ai_llm", AIVendor: "anthropic"})
	if d != domain.DecisionForward {
		t.Errorf("decision = %v, want forward for non-matching vendor", d)
	}
}

func TestEngine_CaptureSurfaceField(t *testing.T) {
	e := NewEngine(domain.DecisionForward)
	err := e.Update(&domain.PolicyPayload{
		Version: "v1",
		Rules: []domain.PolicyRule{
			{
				ID:       "r-surface",
				Priority: 1,
				Decision: domain.DecisionBlock,
				Conditions: []domain.RuleCondition{
					{Field: "capture_surface", Operator: "eq", Value: "browser_chromium"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("update policy: %v", err)
	}

	d, _, _ := e.Apply(&domain.RequestContext{CaptureSurface: "browser_chromium"})
	if d != domain.DecisionBlock {
		t.Errorf("decision = %v, want block for browser_chromium", d)
	}

	d, _, _ = e.Apply(&domain.RequestContext{CaptureSurface: "desktop"})
	if d != domain.DecisionForward {
		t.Errorf("decision = %v, want forward for desktop", d)
	}
}

func TestEngine_InterceptionNormalization(t *testing.T) {
	e := NewEngine(domain.DecisionForward)
	err := e.Update(&domain.PolicyPayload{
		Version: "v2",
		Interception: domain.PolicyInterception{
			Enabled:     true,
			Domains:     []string{"API.OpenAI.com:443", "claude.ai.", "gemini.google.com"},
			Protocols:   []string{"HTTP", "websocket", "bad"},
			FailMode:    "",
			CaptureMode: "",
			Scope:       "",
		},
	})
	if err != nil {
		t.Fatalf("update policy: %v", err)
	}
	cfg := e.Interception()
	if !cfg.Enabled {
		t.Fatal("expected managed interception enabled")
	}
	if cfg.FailMode != domain.HTTPSInterceptFailOpen {
		t.Fatalf("fail mode = %q, want %q", cfg.FailMode, domain.HTTPSInterceptFailOpen)
	}
	if cfg.Scope != domain.InterceptScopeAPIOnly {
		t.Fatalf("scope = %q, want %q", cfg.Scope, domain.InterceptScopeAPIOnly)
	}
	if len(cfg.Protocols) != 2 {
		t.Fatalf("protocols len = %d, want 2", len(cfg.Protocols))
	}
	foundOpenAI := false
	for _, d := range cfg.Domains {
		if d == "api.openai.com" {
			foundOpenAI = true
			break
		}
	}
	if !foundOpenAI {
		t.Fatalf("expected normalized domain api.openai.com in %#v", cfg.Domains)
	}
	for _, d := range cfg.Domains {
		if d == "chat.openai.com" || d == "chatgpt.com" {
			t.Fatalf("did not expect ChatGPT domain alias in %#v", cfg.Domains)
		}
	}
}

func TestEngine_PromptEnforcementOverride(t *testing.T) {
	e := NewEngine(domain.DecisionForward)
	if err := e.Update(&domain.PolicyPayload{
		Version:                   "v1",
		PromptEnforcementOverride: "MONITOR",
	}); err != nil {
		t.Fatalf("update policy: %v", err)
	}
	if got := e.PromptEnforcementOverride(); got != domain.PromptEnforcementModeMonitor {
		t.Fatalf("override = %q, want monitor", got)
	}

	if err := e.Update(&domain.PolicyPayload{
		Version:                   "v2",
		PromptEnforcementOverride: "invalid",
	}); err != nil {
		t.Fatalf("update policy: %v", err)
	}
	if got := e.PromptEnforcementOverride(); got != "" {
		t.Fatalf("invalid override should clear to empty, got %q", got)
	}
}
