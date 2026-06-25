package promptcapture

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/core/telemetry"
	"github.com/themisto/agent/pkg/log"
)

type testConfigProvider struct {
	cfg *domain.AgentConfig
}

func (p *testConfigProvider) Get() *domain.AgentConfig        { return p.cfg }
func (p *testConfigProvider) Watch(func(*domain.AgentConfig)) {}
func (p *testConfigProvider) Validate() error                 { return nil }

type testRouter struct {
	decision domain.Decision
	ruleID   string
	err      error
	rule     *domain.PolicyRule
}

func (r *testRouter) Route(*domain.RequestContext) (domain.Decision, string, error) {
	return r.decision, r.ruleID, r.err
}

func (r *testRouter) LookupRule(id string) *domain.PolicyRule {
	if r.rule != nil && r.rule.ID == id {
		cp := *r.rule
		return &cp
	}
	return nil
}

type testLogger struct{}

func (l *testLogger) Debug(string, ...interface{})   {}
func (l *testLogger) Info(string, ...interface{})    {}
func (l *testLogger) Warn(string, ...interface{})    {}
func (l *testLogger) Error(string, ...interface{})   {}
func (l *testLogger) With(...interface{}) log.Logger { return l }

func TestServiceEvaluateAndOutcomeBlock(t *testing.T) {
	router := &testRouter{
		decision: domain.DecisionBlock,
		ruleID:   "rule-block",
		rule: &domain.PolicyRule{
			ID:          "rule-block",
			BlockReason: "credentials detected",
		},
	}
	svc := NewService(Deps{
		Config: &testConfigProvider{cfg: &domain.AgentConfig{AgentID: "agent-test"}},
		Router: router,
		Logger: &testLogger{},
	})

	evaluateReq := domain.PromptEvaluationRequest{
		PromptText:      "password=ExampleSecret1234",
		Surface:         domain.CaptureSurfaceBrowserChromium,
		AppName:         "Google Chrome",
		DestinationHost: "claude.ai",
		Vendor:          "anthropic",
	}
	raw, _ := json.Marshal(evaluateReq)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/prompt/evaluate", bytes.NewReader(raw))
	svc.handleEvaluate(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("evaluate status=%d want=%d", w.Code, http.StatusOK)
	}

	var evalResp domain.PromptEvaluationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &evalResp); err != nil {
		t.Fatalf("decode evaluate response: %v", err)
	}
	if evalResp.Decision != domain.DecisionBlock {
		t.Fatalf("decision=%s want=block", evalResp.Decision.String())
	}
	if evalResp.Outcome != domain.CaptureOutcomeBlocked {
		t.Fatalf("outcome=%s want=blocked", evalResp.Outcome)
	}
	if evalResp.EvaluationID == "" {
		t.Fatal("expected evaluation_id in response")
	}

	outcomeReq := domain.PromptOutcomeRequest{
		EvaluationID: evalResp.EvaluationID,
		Surface:      domain.CaptureSurfaceBrowserChromium,
		Outcome:      domain.CaptureOutcomeBlocked,
	}
	raw, _ = json.Marshal(outcomeReq)
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/v1/prompt/outcome", bytes.NewReader(raw))
	svc.handleOutcome(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("outcome status=%d want=%d", w.Code, http.StatusAccepted)
	}
}

func TestServiceEvaluateFailOpenOnRoutingError(t *testing.T) {
	router := &testRouter{
		decision: domain.DecisionForward,
		err:      errors.New("routing unavailable"),
	}
	svc := NewService(Deps{
		Config: &testConfigProvider{cfg: &domain.AgentConfig{AgentID: "agent-test"}},
		Router: router,
		Logger: &testLogger{},
	})

	evaluateReq := domain.PromptEvaluationRequest{
		PromptText:      "hello",
		Surface:         domain.CaptureSurfaceDesktop,
		DestinationHost: "api.anthropic.com",
	}
	raw, _ := json.Marshal(evaluateReq)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/prompt/evaluate", bytes.NewReader(raw))
	svc.handleEvaluate(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("evaluate status=%d want=%d", w.Code, http.StatusOK)
	}

	var evalResp domain.PromptEvaluationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &evalResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if evalResp.Outcome != domain.CaptureOutcomeDegradedFailOpen {
		t.Fatalf("outcome=%s want=degraded_fail_open", evalResp.Outcome)
	}
	if !evalResp.Degraded {
		t.Fatal("expected degraded=true")
	}
	if evalResp.Decision != domain.DecisionForward {
		t.Fatalf("decision=%s want=forward", evalResp.Decision.String())
	}
}

func TestServiceEvaluateFailClosedOnRoutingErrorForEnforcedSurface(t *testing.T) {
	router := &testRouter{
		decision: domain.DecisionForward,
		err:      errors.New("routing unavailable"),
	}
	svc := NewService(Deps{
		Config: &testConfigProvider{cfg: &domain.AgentConfig{
			AgentID:               "agent-test",
			PromptEnforcementMode: domain.PromptEnforcementModeEnforce,
			PromptFailClosedSurfaces: []domain.CaptureSurface{
				domain.CaptureSurfaceBrowserChromium,
			},
		}},
		Router: router,
		Logger: &testLogger{},
	})

	evaluateReq := domain.PromptEvaluationRequest{
		PromptText:      "hello",
		Surface:         domain.CaptureSurfaceBrowserChromium,
		DestinationHost: "chatgpt.com",
	}
	raw, _ := json.Marshal(evaluateReq)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/prompt/evaluate", bytes.NewReader(raw))
	svc.handleEvaluate(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("evaluate status=%d want=%d", w.Code, http.StatusOK)
	}

	var evalResp domain.PromptEvaluationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &evalResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if evalResp.Decision != domain.DecisionBlock {
		t.Fatalf("decision=%s want=block", evalResp.Decision.String())
	}
	if evalResp.ReasonCode != "fail_closed_evaluator_unavailable" {
		t.Fatalf("reason_code=%q want fail_closed_evaluator_unavailable", evalResp.ReasonCode)
	}
	if evalResp.SurfaceEnforcement != domain.SurfaceEnforcementHardBlock {
		t.Fatalf("surface_enforcement=%q want hard_block", evalResp.SurfaceEnforcement)
	}
}

func TestServiceStatusReportsEnforcementState(t *testing.T) {
	svc := NewService(Deps{
		Config: &testConfigProvider{cfg: &domain.AgentConfig{
			AgentID:               "agent-test",
			PromptEnforcementMode: domain.PromptEnforcementModeEnforce,
			PromptFailClosedSurfaces: []domain.CaptureSurface{
				domain.CaptureSurfaceBrowserChromium,
			},
		}},
		Router: &testRouter{decision: domain.DecisionForward},
		Logger: &testLogger{},
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/prompt/status", nil)
	svc.handleStatus(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d", w.Code, http.StatusOK)
	}

	var status domain.PromptStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.EffectiveEnforcementMode != domain.PromptEnforcementModeEnforce {
		t.Fatalf("effective mode=%q want enforce", status.EffectiveEnforcementMode)
	}
	if status.SurfaceStates[domain.CaptureSurfaceBrowserChromium] != domain.SurfaceEnforcementHardBlock {
		t.Fatalf("browser state=%q want hard_block", status.SurfaceStates[domain.CaptureSurfaceBrowserChromium])
	}
	if status.SurfaceStates[domain.CaptureSurfaceGitHubCopilot] != domain.SurfaceEnforcementWouldBlock {
		t.Fatalf("copilot state=%q want would_block", status.SurfaceStates[domain.CaptureSurfaceGitHubCopilot])
	}
}

func TestServiceEvaluateLocalBaselineBlocksCredentialLeakToAI(t *testing.T) {
	router := &testRouter{decision: domain.DecisionForward}
	svc := NewService(Deps{
		Config: &testConfigProvider{cfg: &domain.AgentConfig{AgentID: "agent-test"}},
		Router: router,
		Logger: &testLogger{},
	})

	evaluateReq := domain.PromptEvaluationRequest{
		PromptText:      "this is the api key sk-live-123456",
		Surface:         domain.CaptureSurfaceBrowserChromium,
		AppName:         "ChatGPT",
		DestinationHost: "chatgpt.com",
		DestinationURL:  "https://chatgpt.com/",
		Vendor:          "openai",
		ServiceCategory: "ai_llm",
	}
	raw, _ := json.Marshal(evaluateReq)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/prompt/evaluate", bytes.NewReader(raw))
	svc.handleEvaluate(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("evaluate status=%d want=%d", w.Code, http.StatusOK)
	}

	var evalResp domain.PromptEvaluationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &evalResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if evalResp.Decision != domain.DecisionBlock {
		t.Fatalf("decision=%s want=block", evalResp.Decision.String())
	}
	if evalResp.PolicyRuleID != "local-baseline:ai-credentials" {
		t.Fatalf("policy_rule_id=%q want local baseline", evalResp.PolicyRuleID)
	}
	if evalResp.MatchCount == 0 || evalResp.Severity != "critical" {
		t.Fatalf("expected critical DLP match, got match_count=%d severity=%q", evalResp.MatchCount, evalResp.Severity)
	}
	if !strings.Contains(strings.ToLower(evalResp.Message), "blocked") {
		t.Fatalf("message=%q should explain block", evalResp.Message)
	}
}

func TestServiceEvaluateClaudeCodeBlockMessageUsesAppName(t *testing.T) {
	router := &testRouter{
		decision: domain.DecisionBlock,
	}
	svc := NewService(Deps{
		Config: &testConfigProvider{cfg: &domain.AgentConfig{AgentID: "agent-test"}},
		Router: router,
		Logger: &testLogger{},
	})

	evaluateReq := domain.PromptEvaluationRequest{
		PromptText:      "My SSN is 123-45-6789",
		Surface:         domain.CaptureSurfaceClaudeCode,
		AppName:         "Claude Code",
		DestinationHost: "api.anthropic.com",
		Vendor:          "anthropic",
		ServiceCategory: "ai_code",
	}
	raw, _ := json.Marshal(evaluateReq)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/prompt/evaluate", bytes.NewReader(raw))
	svc.handleEvaluate(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("evaluate status=%d want=%d", w.Code, http.StatusOK)
	}

	var evalResp domain.PromptEvaluationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &evalResp); err != nil {
		t.Fatalf("decode evaluate response: %v", err)
	}
	if !strings.Contains(evalResp.Message, "Claude Code") {
		t.Fatalf("message=%q want app name", evalResp.Message)
	}
	if strings.Contains(evalResp.Message, "api.anthropic.com") {
		t.Fatalf("message=%q should not expose raw host", evalResp.Message)
	}
	if !strings.Contains(strings.ToLower(evalResp.Message), "sensitive information") {
		t.Fatalf("message=%q want sensitive information wording", evalResp.Message)
	}
}

func TestServiceEvaluateAcceptsCursorSurface(t *testing.T) {
	router := &testRouter{
		decision: domain.DecisionForward,
	}
	svc := NewService(Deps{
		Config: &testConfigProvider{cfg: &domain.AgentConfig{AgentID: "agent-test"}},
		Router: router,
		Logger: &testLogger{},
	})

	evaluateReq := domain.PromptEvaluationRequest{
		PromptText:      "hello from cursor",
		Surface:         domain.CaptureSurfaceCursor,
		AppName:         "Cursor",
		DestinationHost: "api2.cursor.sh",
		Vendor:          "cursor",
		ServiceCategory: "ai_code",
	}
	raw, _ := json.Marshal(evaluateReq)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/prompt/evaluate", bytes.NewReader(raw))
	svc.handleEvaluate(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("evaluate status=%d want=%d", w.Code, http.StatusOK)
	}

	var evalResp domain.PromptEvaluationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &evalResp); err != nil {
		t.Fatalf("decode evaluate response: %v", err)
	}
	if evalResp.Decision != domain.DecisionForward {
		t.Fatalf("decision=%s want=forward", evalResp.Decision.String())
	}
}

func TestServiceEvaluateAcceptsGitHubCopilotSurface(t *testing.T) {
	router := &testRouter{
		decision: domain.DecisionForward,
	}
	svc := NewService(Deps{
		Config: &testConfigProvider{cfg: &domain.AgentConfig{AgentID: "agent-test"}},
		Router: router,
		Logger: &testLogger{},
	})

	evaluateReq := domain.PromptEvaluationRequest{
		PromptText:      "hello from copilot",
		Surface:         domain.CaptureSurfaceGitHubCopilot,
		AppName:         "GitHub Copilot",
		DestinationHost: "api.githubcopilot.com",
		Vendor:          "github",
		ServiceCategory: "ai_code",
	}
	raw, _ := json.Marshal(evaluateReq)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/prompt/evaluate", bytes.NewReader(raw))
	svc.handleEvaluate(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("evaluate status=%d want=%d", w.Code, http.StatusOK)
	}

	var evalResp domain.PromptEvaluationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &evalResp); err != nil {
		t.Fatalf("decode evaluate response: %v", err)
	}
	if evalResp.Decision != domain.DecisionForward {
		t.Fatalf("decision=%s want=forward", evalResp.Decision.String())
	}
}

func TestServiceAlertPolicyTriggeredEmitsDLPEvent(t *testing.T) {
	router := &testRouter{
		decision: domain.DecisionAlert,
		ruleID:   "rule-alert",
		rule: &domain.PolicyRule{
			ID:          "rule-alert",
			BlockReason: "policy alert: no AI code sharing",
		},
	}
	collector := telemetry.NewCollector(64)
	svc := NewService(Deps{
		Config:  &testConfigProvider{cfg: &domain.AgentConfig{AgentID: "agent-test"}},
		Router:  router,
		Logger:  &testLogger{},
		Metrics: collector,
	})

	evaluateReq := domain.PromptEvaluationRequest{
		PromptText:      "here is my implementation",
		Surface:         domain.CaptureSurfaceClaudeCode,
		AppName:         "Claude Code",
		DestinationHost: "api.anthropic.com",
		Vendor:          "anthropic",
		ServiceCategory: "ai_code",
	}
	raw, _ := json.Marshal(evaluateReq)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/prompt/evaluate", bytes.NewReader(raw))
	svc.handleEvaluate(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("evaluate status=%d want=%d", w.Code, http.StatusOK)
	}

	var evalResp domain.PromptEvaluationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &evalResp); err != nil {
		t.Fatalf("decode evaluate response: %v", err)
	}
	if evalResp.Decision != domain.DecisionAlert {
		t.Fatalf("decision=%s want=alert", evalResp.Decision.String())
	}

	// Drain events emitted by handleEvaluate so only handleOutcome events remain.
	collector.Drain()

	outcomeReq := domain.PromptOutcomeRequest{
		EvaluationID: evalResp.EvaluationID,
		Surface:      domain.CaptureSurfaceClaudeCode,
		Outcome:      domain.CaptureOutcomeAllowed,
		AppName:      "Claude Code",
	}
	raw, _ = json.Marshal(outcomeReq)
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/v1/prompt/outcome", bytes.NewReader(raw))
	svc.handleOutcome(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("outcome status=%d want=%d", w.Code, http.StatusAccepted)
	}

	// handleOutcome must emit two events: one proxy.request telemetry event and
	// one dlp.match event for the policy-triggered alert (no DLP matches).
	if got := collector.Size(); got < 2 {
		t.Fatalf("expected >=2 events from handleOutcome (proxy.request + dlp.match), got %d", got)
	}
}

func TestServiceOutcomeAcceptsWouldBlock(t *testing.T) {
	router := &testRouter{
		decision: domain.DecisionBlock,
		ruleID:   "rule-block",
	}
	svc := NewService(Deps{
		Config: &testConfigProvider{cfg: &domain.AgentConfig{AgentID: "agent-test"}},
		Router: router,
		Logger: &testLogger{},
	})

	evaluateReq := domain.PromptEvaluationRequest{
		PromptText:      "secret value",
		Surface:         domain.CaptureSurfaceWindsurf,
		AppName:         "Windsurf",
		DestinationHost: "windsurf.ai",
		Vendor:          "windsurf",
		ServiceCategory: "ai_code",
	}
	raw, _ := json.Marshal(evaluateReq)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/prompt/evaluate", bytes.NewReader(raw))
	svc.handleEvaluate(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("evaluate status=%d want=%d", w.Code, http.StatusOK)
	}

	var evalResp domain.PromptEvaluationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &evalResp); err != nil {
		t.Fatalf("decode evaluate response: %v", err)
	}

	outcomeReq := domain.PromptOutcomeRequest{
		EvaluationID: evalResp.EvaluationID,
		Surface:      domain.CaptureSurfaceWindsurf,
		Outcome:      domain.CaptureOutcomeWouldBlock,
	}
	raw, _ = json.Marshal(outcomeReq)
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/v1/prompt/outcome", bytes.NewReader(raw))
	svc.handleOutcome(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("outcome status=%d want=%d", w.Code, http.StatusAccepted)
	}
}
