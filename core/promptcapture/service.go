package promptcapture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/themisto/agent/core/adapter/iface"
	"github.com/themisto/agent/core/config"
	"github.com/themisto/agent/core/dlp"
	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/core/routing"
	"github.com/themisto/agent/core/telemetry"
	"github.com/themisto/agent/pkg/log"
	"github.com/themisto/agent/pkg/uid"
)

const (
	// DefaultListenAddr is the loopback endpoint for pre-send prompt decisions.
	DefaultListenAddr = "127.0.0.1:17175"

	maxEvaluateBodyBytes = 1 << 20
	evalRetention        = 15 * time.Minute
	maxEvalEntries       = 4096
)

type Deps struct {
	Config         config.Provider
	Router         routing.Router
	Semantic       SemanticEvaluator
	Metrics        *telemetry.Collector
	Notifier       iface.UserNotifier
	Logger         log.Logger
	ListenAddr     string
	OnBlockCleanup func(domain.PromptEvaluationRequest) // optional: called asynchronously after a prompt is blocked
}

type Service struct {
	cfg            config.Provider
	router         routing.Router
	semantic       SemanticEvaluator
	metrics        *telemetry.Collector
	notifier       iface.UserNotifier
	log            log.Logger
	onBlockCleanup func(domain.PromptEvaluationRequest)

	listenAddr string
	server     *http.Server

	mu          sync.Mutex
	evaluations map[string]storedEvaluation
}

type storedEvaluation struct {
	At           time.Time
	Request      domain.PromptEvaluationRequest
	Response     domain.PromptEvaluationResponse
	RequestCtx   domain.RequestContext
	DLP          domain.DLPInfo
	MatchedRule  *domain.PolicyRule
	Semantic     *domain.PromptSemanticResult
	Decision     domain.Decision
	PolicyRuleID string
}

type SemanticEvaluator interface {
	Evaluate(ctx context.Context, req domain.PromptSemanticRequest) (*domain.PromptSemanticResult, error)
}

// NewService creates the local loopback prompt decision service.
func NewService(deps Deps) *Service {
	listenAddr := strings.TrimSpace(deps.ListenAddr)
	if listenAddr == "" {
		listenAddr = DefaultListenAddr
	}
	return &Service{
		cfg:            deps.Config,
		router:         deps.Router,
		semantic:       deps.Semantic,
		metrics:        deps.Metrics,
		notifier:       deps.Notifier,
		log:            deps.Logger,
		onBlockCleanup: deps.OnBlockCleanup,
		listenAddr:     listenAddr,
		evaluations:    make(map[string]storedEvaluation),
	}
}

func (s *Service) ListenAddr() string {
	return s.listenAddr
}

func (s *Service) Start(ctx context.Context) error {
	if err := ensureLoopbackAddr(s.listenAddr); err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/prompt/evaluate", s.handleEvaluate)
	mux.HandleFunc("POST /v1/prompt/outcome", s.handleOutcome)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /extensions/chromium/updates.xml", s.handleChromeUpdatesXML)
	mux.HandleFunc("GET /extensions/chromium/themisto.crx", s.handleChromeExtensionCRX)
	mux.HandleFunc("GET /extensions/firefox/themisto.xpi", s.handleFirefoxExtensionXPI)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	})

	s.server = &http.Server{
		Addr:              s.listenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
	}

	ln, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("prompt capture listen: %w", err)
	}

	go func() {
		<-ctx.Done()
		_ = s.Stop()
	}()
	go func() {
		if err := s.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("prompt capture server exited", "error", err)
			s.emitPromptTelemetry(promptTelemetryInput{
				Stage:           "service",
				Surface:         domain.CaptureSurfaceDesktop,
				Outcome:         domain.CaptureOutcomeDegradedFailOpen,
				Decision:        domain.DecisionForward,
				DestinationHost: "local-agent",
				DestinationPath: "/prompt-capture/server-error",
				Error:           err.Error(),
				Status:          http.StatusServiceUnavailable,
			})
		}
	}()

	s.log.Info("prompt capture service listening", "addr", s.listenAddr)
	return nil
}

func (s *Service) Stop() error {
	if s.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}

func ensureLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("prompt capture listen address must be loopback, got %q", addr)
	}
	return nil
}

func (s *Service) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":   true,
		"addr": s.listenAddr,
	})
}

func (s *Service) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		if rec := recover(); rec != nil {
			s.log.Error("prompt evaluate panic", "error", rec)
			resp := domain.PromptEvaluationResponse{
				EvaluationID:  uid.New(),
				Decision:      domain.DecisionForward,
				Outcome:       domain.CaptureOutcomeDegradedFailOpen,
				Message:       "Prompt evaluation unavailable. Proceeding in degraded fail-open mode.",
				Degraded:      true,
				DegradedCause: "panic",
				ReasonCode:    "degraded_fail_open",
				Reason:        "Prompt evaluator panicked",
			}
			writeJSON(w, http.StatusOK, resp)
		}
	}()

	req, err := decodePromptEvaluationRequest(w, r)
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}

	resp, eval, err := s.evaluate(req)
	if err != nil {
		s.log.Warn("prompt evaluate failed; fail-open", "error", err)
		resp = domain.PromptEvaluationResponse{
			EvaluationID:  uid.New(),
			Decision:      domain.DecisionForward,
			Outcome:       domain.CaptureOutcomeDegradedFailOpen,
			Message:       "Prompt evaluation unavailable. Proceeding in degraded fail-open mode.",
			Degraded:      true,
			DegradedCause: "evaluate_error",
			ReasonCode:    "degraded_fail_open",
			Reason:        "Prompt evaluator unavailable",
		}
		s.emitPromptTelemetry(promptTelemetryInput{
			Stage:           "evaluate",
			Surface:         req.Surface,
			Outcome:         resp.Outcome,
			Decision:        resp.Decision,
			DestinationHost: normalizedPromptHost(req),
			DestinationPath: normalizedPromptPath(req),
			AppName:         req.AppName,
			Vendor:          req.Vendor,
			ServiceCategory: req.ServiceCategory,
			PolicyRuleID:    "",
			PromptChars:     len(req.PromptText),
			Latency:         time.Since(start),
			Error:           err.Error(),
			Status:          http.StatusOK,
		})
		writeJSON(w, http.StatusOK, resp)
		return
	}

	s.storeEvaluation(eval)
	// Respond before non-critical side effects so UI notifications can never stall
	// the browser's pre-send policy check.
	writeJSON(w, http.StatusOK, resp)

	s.emitPromptTelemetry(promptTelemetryInput{
		Stage:           "evaluate",
		Surface:         req.Surface,
		Outcome:         resp.Outcome,
		Decision:        resp.Decision,
		DestinationHost: eval.RequestCtx.Host,
		DestinationPath: eval.RequestCtx.Path,
		AppName:         req.AppName,
		Vendor:          eval.RequestCtx.AIVendor,
		ServiceCategory: eval.RequestCtx.ServiceCategory,
		PolicyRuleID:    eval.PolicyRuleID,
		PromptChars:     len(req.PromptText),
		MatchCount:      len(eval.DLP.Matches),
		Latency:         time.Since(start),
		Status:          http.StatusOK,
	})

	if resp.Decision == domain.DecisionBlock || resp.Decision == domain.DecisionAlert {
		go s.notifyUser(eval.RequestCtx.Host, resp.Decision, eval.MatchedRule)
	}

	// Trigger async cleanup of local AI tool storage after a block.
	if resp.Decision == domain.DecisionBlock && s.onBlockCleanup != nil {
		go func(req domain.PromptEvaluationRequest) {
			// Small delay to let the IDE process the block response first.
			time.Sleep(2 * time.Second)
			s.log.Info("running post-block cleanup", "surface", req.Surface, "app", req.AppName)
			s.onBlockCleanup(req)
		}(eval.Request)
	}
}

func (s *Service) handleOutcome(w http.ResponseWriter, r *http.Request) {
	req, err := decodePromptOutcomeRequest(w, r)
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}

	eval, ok := s.loadEvaluation(req.EvaluationID)
	host := strings.TrimSpace(req.DestinationHost)
	path := strings.TrimSpace(req.DestinationPath)
	vendor := strings.TrimSpace(req.Vendor)
	serviceCategory := strings.TrimSpace(req.ServiceCategory)
	appName := strings.TrimSpace(req.AppName)
	decision := domain.DecisionForward
	ruleID := ""
	matchCount := 0

	if ok {
		if host == "" {
			host = eval.RequestCtx.Host
		}
		if path == "" {
			path = eval.RequestCtx.Path
		}
		if vendor == "" {
			vendor = eval.RequestCtx.AIVendor
		}
		if serviceCategory == "" {
			serviceCategory = eval.RequestCtx.ServiceCategory
		}
		if appName == "" {
			appName = eval.RequestCtx.Process.Name
		}
		decision = eval.Decision
		ruleID = eval.PolicyRuleID
		matchCount = len(eval.DLP.Matches)
	} else {
		host = domain.NormalizeHost(host)
	}

	if host == "" {
		host = "unknown.prompt.target"
	}
	if path == "" {
		path = "/"
	}

	s.emitPromptTelemetry(promptTelemetryInput{
		Stage:           "outcome",
		Surface:         req.Surface,
		Outcome:         req.Outcome,
		Decision:        decision,
		DestinationHost: host,
		DestinationPath: path,
		AppName:         appName,
		Vendor:          vendor,
		ServiceCategory: serviceCategory,
		PolicyRuleID:    ruleID,
		MatchCount:      matchCount,
		PromptChars:     0,
		Error:           strings.TrimSpace(req.Error),
		Status:          statusForCaptureOutcome(req.Outcome, decision),
	})

	if ok && (len(eval.DLP.Matches) > 0 || eval.Decision == domain.DecisionAlert || eval.Decision == domain.DecisionBlock) {
		action := "alert"
		if req.Outcome == domain.CaptureOutcomeBlocked {
			action = "block"
		}
		reasonCode := eval.Response.ReasonCode
		reasonDetail := eval.Response.Reason
		if req.Outcome == domain.CaptureOutcomeWouldBlock {
			reasonCode = "would_block"
			reasonDetail = "Prompt matched block policy, but the adapter reported audit-only monitoring."
		} else if req.Outcome == domain.CaptureOutcomeDegradedFailOpen {
			reasonCode = "degraded_fail_open"
			reasonDetail = "Adapter reported degraded fail-open"
			if strings.TrimSpace(req.Error) != "" {
				reasonDetail = strings.TrimSpace(req.Error)
			}
		}
		s.emitPromptDLPEvent(eval, action, reasonCode, reasonDetail)
	}

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"accepted": true,
	})
}

func decodePromptEvaluationRequest(w http.ResponseWriter, r *http.Request) (domain.PromptEvaluationRequest, error) {
	var req domain.PromptEvaluationRequest
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxEvaluateBodyBytes))
	if err != nil {
		return req, fmt.Errorf("read request: %w", err)
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return req, fmt.Errorf("invalid json payload")
	}
	req.PromptText = strings.TrimSpace(req.PromptText)
	if req.PromptText == "" {
		return req, fmt.Errorf("prompt_text is required")
	}
	if !req.Surface.Valid() {
		return req, fmt.Errorf("surface must be one of browser_chromium, browser_firefox, browser_safari, desktop, claude_code, cursor, windsurf, github_copilot")
	}
	return req, nil
}

func decodePromptOutcomeRequest(w http.ResponseWriter, r *http.Request) (domain.PromptOutcomeRequest, error) {
	var req domain.PromptOutcomeRequest
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxEvaluateBodyBytes))
	if err != nil {
		return req, fmt.Errorf("read request: %w", err)
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return req, fmt.Errorf("invalid json payload")
	}
	if !req.Surface.Valid() {
		return req, fmt.Errorf("surface must be one of browser_chromium, browser_firefox, browser_safari, desktop, claude_code, cursor, windsurf, github_copilot")
	}
	if !req.Outcome.Valid() {
		return req, fmt.Errorf("outcome must be blocked, allowed, would_block, or degraded_fail_open")
	}
	return req, nil
}

func (s *Service) evaluate(req domain.PromptEvaluationRequest) (domain.PromptEvaluationResponse, storedEvaluation, error) {
	host, path := normalizePromptTarget(req)
	vendor, serviceCategory := normalizePromptVendor(req, host)
	processName := strings.TrimSpace(req.AppName)
	if processName == "" {
		processName = "prompt_capture_adapter"
	}

	ctx := domain.RequestContext{
		RequestID:            strings.TrimSpace(req.RequestID),
		Host:                 host,
		Path:                 path,
		Method:               "PROMPT",
		Scheme:               "https",
		Protocol:             domain.InterceptProtocolHTTP,
		Direction:            domain.TrafficDirectionOutbound,
		InspectionQuality:    domain.InspectionQualityFull,
		InspectionSkipReason: "",
		Process: domain.ProcessInfo{
			Name:     processName,
			Path:     strings.TrimSpace(req.AppPath),
			BundleID: strings.TrimSpace(req.AppID),
		},
		ServiceCategory: serviceCategory,
		AIVendor:        vendor,
		CaptureSurface:  string(req.Surface),
	}

	body := []byte(req.PromptText)
	scanBody := body
	bodyTruncated := false
	if len(body) > dlp.MaxBodySize {
		scanBody = body[:dlp.MaxBodySize]
		bodyTruncated = true
	}

	info := dlp.ScanRequest("text/plain", path, scanBody, nil)
	if info.BodySample == "" {
		info.BodySample = string(scanBody)
	}
	info.BodyTruncated = info.BodyTruncated || bodyTruncated
	ctx.DLP = info

	decision, ruleID, err := s.router.Route(&ctx)
	if err != nil {
		return domain.PromptEvaluationResponse{}, storedEvaluation{}, err
	}
	rule := s.lookupRule(ruleID)
	semanticResult := s.evaluateSemantics(req, ctx, info)
	decision, ruleID, rule = s.applySemanticDecision(decision, ruleID, rule, semanticResult)
	reasonCode, reasonDetail := promptReasonForDecision(decision, rule, info, semanticResult)

	outcome := domain.CaptureOutcomeAllowed
	if decision == domain.DecisionBlock {
		outcome = domain.CaptureOutcomeBlocked
	}

	resp := domain.PromptEvaluationResponse{
		EvaluationID: uid.New(),
		Decision:     decision,
		PolicyRuleID: ruleID,
		ReasonCode:   reasonCode,
		Reason:       reasonDetail,
		Message:      promptMessage(decision, req, host, rule, info),
		Outcome:      outcome,
		MatchCount:   len(info.Matches),
		MatchTypes:   uniqueMatchTypes(info.Matches),
		Severity:     info.Severity,
		Semantic:     semanticResult,
	}

	eval := storedEvaluation{
		At:           time.Now(),
		Request:      req,
		Response:     resp,
		RequestCtx:   ctx,
		DLP:          info,
		MatchedRule:  rule,
		Semantic:     semanticResult,
		Decision:     decision,
		PolicyRuleID: ruleID,
	}
	return resp, eval, nil
}

func (s *Service) evaluateSemantics(req domain.PromptEvaluationRequest, requestCtx domain.RequestContext, info domain.DLPInfo) *domain.PromptSemanticResult {
	if s.semantic == nil || s.cfg == nil {
		return nil
	}
	cfg := s.cfg.Get()
	if cfg == nil || !cfg.PromptSemanticsEnabled {
		return nil
	}

	start := time.Now()
	timeout := cfg.PromptSemanticsLocalTimeout + cfg.PromptSemanticsGatewayTimeout + 100*time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result, err := s.semantic.Evaluate(ctx, domain.PromptSemanticRequest{
		PromptText:      req.PromptText,
		Policy:          cfg.PromptSemanticsPolicy,
		Surface:         req.Surface,
		AppName:         req.AppName,
		DestinationHost: requestCtx.Host,
		DestinationPath: requestCtx.Path,
		Vendor:          requestCtx.AIVendor,
		ServiceCategory: requestCtx.ServiceCategory,
		DLP:             info,
		Metadata:        req.Metadata,
	})
	if err != nil {
		s.log.Warn("prompt semantic evaluation skipped", "error", err)
		return nil
	}
	if result == nil {
		return nil
	}
	if result.LatencyMs == 0 {
		result.LatencyMs = time.Since(start).Milliseconds()
	}
	return result
}

func (s *Service) applySemanticDecision(decision domain.Decision, ruleID string, rule *domain.PolicyRule, semantic *domain.PromptSemanticResult) (domain.Decision, string, *domain.PolicyRule) {
	if semantic == nil || s.cfg == nil {
		return decision, ruleID, rule
	}
	if decision == domain.DecisionBlock {
		return decision, ruleID, rule
	}
	cfg := s.cfg.Get()
	if cfg == nil {
		return decision, ruleID, rule
	}

	switch semantic.Decision {
	case domain.DecisionBlock:
		if semantic.Confidence >= cfg.PromptSemanticsBlockThreshold {
			return domain.DecisionBlock, semanticRuleID(semantic), nil
		}
		if semantic.Confidence >= cfg.PromptSemanticsAlertThreshold {
			return domain.DecisionAlert, semanticRuleID(semantic), nil
		}
	case domain.DecisionAlert:
		if semantic.Confidence >= cfg.PromptSemanticsAlertThreshold {
			return domain.DecisionAlert, semanticRuleID(semantic), nil
		}
	}
	return decision, ruleID, rule
}

func semanticRuleID(result *domain.PromptSemanticResult) string {
	if result == nil || strings.TrimSpace(result.Source) == "" {
		return "semantic"
	}
	return "semantic:" + strings.TrimSpace(result.Source)
}

func (s *Service) storeEvaluation(eval storedEvaluation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evaluations[eval.Response.EvaluationID] = eval
	s.pruneEvaluationsLocked()
}

func (s *Service) loadEvaluation(id string) (storedEvaluation, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return storedEvaluation{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	eval, ok := s.evaluations[id]
	if !ok {
		return storedEvaluation{}, false
	}
	if time.Since(eval.At) > evalRetention {
		delete(s.evaluations, id)
		return storedEvaluation{}, false
	}
	return eval, true
}

func (s *Service) pruneEvaluationsLocked() {
	if len(s.evaluations) == 0 {
		return
	}
	cutoff := time.Now().Add(-evalRetention)
	for id, e := range s.evaluations {
		if e.At.Before(cutoff) {
			delete(s.evaluations, id)
		}
	}
	if len(s.evaluations) <= maxEvalEntries {
		return
	}
	type kv struct {
		id string
		at time.Time
	}
	rows := make([]kv, 0, len(s.evaluations))
	for id, e := range s.evaluations {
		rows = append(rows, kv{id: id, at: e.At})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].at.Before(rows[j].at) })
	toDrop := len(rows) - maxEvalEntries
	for i := 0; i < toDrop; i++ {
		delete(s.evaluations, rows[i].id)
	}
}

type lookupRuleCapable interface {
	LookupRule(id string) *domain.PolicyRule
}

func (s *Service) lookupRule(id string) *domain.PolicyRule {
	if id == "" {
		return nil
	}
	lookup, ok := s.router.(lookupRuleCapable)
	if !ok {
		return nil
	}
	return lookup.LookupRule(id)
}

func normalizePromptTarget(req domain.PromptEvaluationRequest) (string, string) {
	host := domain.NormalizeHost(req.DestinationHost)
	path := strings.TrimSpace(req.DestinationPath)

	rawURL := strings.TrimSpace(req.DestinationURL)
	if rawURL != "" {
		if parsed, err := url.Parse(rawURL); err == nil {
			if host == "" {
				host = domain.NormalizeHost(parsed.Host)
			}
			if path == "" {
				path = strings.TrimSpace(parsed.Path)
			}
		}
	}

	if host == "" {
		host = "unknown.prompt.target"
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return host, path
}

func normalizedPromptHost(req domain.PromptEvaluationRequest) string {
	host, _ := normalizePromptTarget(req)
	return host
}

func normalizedPromptPath(req domain.PromptEvaluationRequest) string {
	_, path := normalizePromptTarget(req)
	return path
}

func normalizePromptVendor(req domain.PromptEvaluationRequest, host string) (string, string) {
	vendor := strings.ToLower(strings.TrimSpace(req.Vendor))
	category := strings.ToLower(strings.TrimSpace(req.ServiceCategory))
	if info, ok := domain.InferAIService(host, req.DestinationURL); ok {
		if vendor == "" {
			vendor = info.Vendor
		}
		if category == "" {
			category = info.Category
		}
	}
	return vendor, category
}

type promptTelemetryInput struct {
	Stage           string
	Surface         domain.CaptureSurface
	Outcome         domain.CaptureOutcome
	Decision        domain.Decision
	DestinationHost string
	DestinationPath string
	AppName         string
	Vendor          string
	ServiceCategory string
	PolicyRuleID    string
	PromptChars     int
	MatchCount      int
	Error           string
	Latency         time.Duration
	Status          int
}

func (s *Service) emitPromptTelemetry(in promptTelemetryInput) {
	if s.metrics == nil || s.cfg == nil {
		return
	}
	path := strings.TrimSpace(in.DestinationPath)
	if path == "" {
		path = "/"
	}
	path = fmt.Sprintf("/prompt-capture/%s/%s/%s/%s", sanitizePathToken(in.Stage), sanitizePathToken(string(in.Surface)), sanitizePathToken(in.Decision.String()), sanitizePathToken(string(in.Outcome)))
	host := strings.TrimSpace(in.DestinationHost)
	if host == "" {
		host = "unknown.prompt.target"
	}
	data := map[string]interface{}{
		"method":          "PROMPT",
		"host":            host,
		"path":            path,
		"decision":        promptDecisionForTelemetry(in.Decision),
		"latency_ms":      in.Latency.Milliseconds(),
		"status":          in.Status,
		"protocol":        domain.InterceptProtocolHTTP,
		"direction":       domain.TrafficDirectionOutbound,
		"capture_stage":   in.Stage,
		"capture_surface": string(in.Surface),
		"capture_outcome": string(in.Outcome),
	}
	if in.PromptChars > 0 {
		data["prompt_chars"] = in.PromptChars
	}
	if in.MatchCount > 0 {
		data["match_count"] = in.MatchCount
	}
	if in.PolicyRuleID != "" {
		data["rule_id"] = in.PolicyRuleID
	}
	if v := strings.TrimSpace(in.AppName); v != "" {
		data["source_app"] = v
	}
	if v := strings.TrimSpace(in.Vendor); v != "" {
		data["ai_vendor"] = v
	}
	if v := strings.TrimSpace(in.ServiceCategory); v != "" {
		data["service_category"] = v
	}
	if v := strings.TrimSpace(in.Error); v != "" {
		data["error"] = v
	}

	cfg := s.cfg.Get()
	_ = s.metrics.Emit("proxy.request", &domain.EventPayload{
		AgentID:   cfg.AgentID,
		Timestamp: time.Now(),
		Data:      data,
	})
}

func sanitizePathToken(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return "unknown"
	}
	v = strings.ReplaceAll(v, " ", "_")
	v = strings.ReplaceAll(v, "/", "_")
	return v
}

func promptDecisionForTelemetry(decision domain.Decision) string {
	switch decision {
	case domain.DecisionBlock:
		return "block"
	case domain.DecisionAlert:
		return "alert"
	default:
		return "allow"
	}
}

func statusForCaptureOutcome(outcome domain.CaptureOutcome, decision domain.Decision) int {
	switch outcome {
	case domain.CaptureOutcomeBlocked:
		return http.StatusForbidden
	case domain.CaptureOutcomeWouldBlock:
		return http.StatusAccepted
	case domain.CaptureOutcomeDegradedFailOpen:
		return http.StatusOK
	default:
		if decision == domain.DecisionAlert {
			return http.StatusAccepted
		}
		return http.StatusOK
	}
}

func (s *Service) emitPromptDLPEvent(eval storedEvaluation, action, reasonCode, reasonDetail string) {
	if s.metrics == nil || s.cfg == nil {
		return
	}
	matchTypes := uniqueMatchTypes(eval.DLP.Matches)
	patterns := make([]string, 0, len(eval.DLP.Matches))
	excerpts := make([]string, 0, len(eval.DLP.Matches))
	for _, m := range eval.DLP.Matches {
		patterns = append(patterns, m.Pattern)
		excerpts = append(excerpts, m.Excerpt)
	}

	data := map[string]interface{}{
		"host":                   eval.RequestCtx.Host,
		"path":                   eval.RequestCtx.Path,
		"method":                 "PROMPT",
		"request_id":             eval.Response.EvaluationID,
		"match_types":            matchTypes,
		"matched_patterns":       patterns,
		"matched_fields":         eval.DLP.MatchedFields,
		"matched_excerpts":       excerpts,
		"match_count":            len(eval.DLP.Matches),
		"action_taken":           action,
		"severity":               eval.DLP.Severity,
		"classification_reason":  eval.DLP.ClassificationReason,
		"content_type":           "text/plain",
		"file_count":             0,
		"request_body":           eval.DLP.BodySample,
		"request_body_truncated": eval.DLP.BodyTruncated,
		"protocol":               domain.InterceptProtocolHTTP,
		"direction":              domain.TrafficDirectionOutbound,
		"inspection_quality":     domain.InspectionQualityFull,
	}
	if eval.PolicyRuleID != "" {
		data["policy_rule_id"] = eval.PolicyRuleID
	}
	if eval.Semantic != nil {
		data["semantic_source"] = eval.Semantic.Source
		data["semantic_category"] = eval.Semantic.Category
		data["semantic_confidence"] = eval.Semantic.Confidence
		data["semantic_ambiguous"] = eval.Semantic.Ambiguous
		data["semantic_reason"] = eval.Semantic.Reason
	}
	if reasonCode != "" {
		data["reason_code"] = reasonCode
	}
	if reasonDetail != "" {
		data["reason_detail"] = reasonDetail
	}
	if eval.RequestCtx.AIVendor != "" {
		data["ai_vendor"] = eval.RequestCtx.AIVendor
	}
	if eval.RequestCtx.ServiceCategory != "" {
		data["service_category"] = eval.RequestCtx.ServiceCategory
	}
	if eval.RequestCtx.Process.Name != "" {
		data["source_app"] = eval.RequestCtx.Process.Name
	}

	cfg := s.cfg.Get()
	_ = s.metrics.Emit("dlp.match", &domain.EventPayload{
		AgentID:   cfg.AgentID,
		Timestamp: time.Now(),
		Data:      data,
	})
}

func uniqueMatchTypes(matches []domain.DLPMatch) []string {
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		v := strings.TrimSpace(string(m.Type))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func promptReasonForDecision(decision domain.Decision, rule *domain.PolicyRule, info domain.DLPInfo, semantic *domain.PromptSemanticResult) (string, string) {
	if semantic != nil {
		if decision == domain.DecisionBlock && semantic.Decision == domain.DecisionBlock {
			return "semantic_policy_block", semanticReason(semantic, "Prompt matched semantic corporate DLP policy")
		}
		if decision == domain.DecisionAlert && (semantic.Decision == domain.DecisionBlock || semantic.Decision == domain.DecisionAlert) {
			return "semantic_policy_alert", semanticReason(semantic, "Prompt may match semantic corporate DLP policy")
		}
	}
	if decision == domain.DecisionBlock {
		if rule != nil && strings.TrimSpace(rule.BlockReason) != "" {
			return "policy_block", strings.TrimSpace(rule.BlockReason)
		}
		return "policy_block", "Blocked by organization policy"
	}
	if decision == domain.DecisionAlert {
		if rule != nil && strings.TrimSpace(rule.BlockReason) != "" {
			return "policy_alert", strings.TrimSpace(rule.BlockReason)
		}
		return "policy_alert", "Matched alert policy"
	}
	if len(info.Matches) > 0 {
		return "dlp_match", "Sensitive content detected by DLP scanner"
	}
	return "policy_allow", "Allowed by organization policy"
}

func semanticReason(result *domain.PromptSemanticResult, fallback string) string {
	if result == nil {
		return fallback
	}
	reason := strings.TrimSpace(result.Reason)
	if reason == "" {
		reason = fallback
	}
	if category := strings.TrimSpace(result.Category); category != "" {
		return fmt.Sprintf("%s (%s, confidence %.2f)", reason, category, result.Confidence)
	}
	return fmt.Sprintf("%s (confidence %.2f)", reason, result.Confidence)
}

func promptMessage(decision domain.Decision, req domain.PromptEvaluationRequest, host string, rule *domain.PolicyRule, info domain.DLPInfo) string {
	target := promptTargetLabel(req, host)
	switch decision {
	case domain.DecisionBlock:
		reason := "it violates active organization policy"
		if rule != nil && strings.TrimSpace(rule.BlockReason) != "" {
			reason = strings.TrimSpace(rule.BlockReason)
		} else if len(info.Matches) > 0 {
			reason = "it appears to contain sensitive information"
		}
		if strings.HasPrefix(strings.ToLower(reason), "it ") {
			return fmt.Sprintf("This prompt was blocked in %s because %s.", target, reason)
		}
		return fmt.Sprintf("This prompt was blocked in %s: %s", target, reason)
	case domain.DecisionAlert:
		if len(info.Matches) > 0 {
			return fmt.Sprintf("This prompt in %s may contain sensitive information. Sending is allowed, but Themisto will alert on it.", target)
		}
		return fmt.Sprintf("This prompt in %s matched an alert policy. Sending is allowed, but Themisto will alert on it.", target)
	default:
		return "Prompt allowed by policy."
	}
}

func promptTargetLabel(req domain.PromptEvaluationRequest, host string) string {
	if app := strings.TrimSpace(req.AppName); app != "" {
		return app
	}
	if info, ok := domain.InferAIService(host, req.DestinationURL); ok {
		if name := strings.TrimSpace(info.Name); name != "" {
			return name
		}
	}
	host = strings.TrimSpace(host)
	if host == "" || host == "unknown.prompt.target" {
		return "this destination"
	}
	return host
}

// --- Extension serving handlers ---

func (s *Service) handleChromeUpdatesXML(w http.ResponseWriter, r *http.Request) {
	// appid must match the 32-char a-p Chromium extension ID.
	xml := `<?xml version='1.0' encoding='UTF-8'?>
<gupdate xmlns='http://www.google.com/update2/response' protocol='2.0'>
  <app appid='bgijehgoebfjapkdoopgbmkfhganpmna'>
    <updatecheck codebase='http://127.0.0.1:17175/extensions/chromium/themisto.crx' version='1.0.0' />
  </app>
</gupdate>`
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml))
}

func (s *Service) handleChromeExtensionCRX(w http.ResponseWriter, r *http.Request) {
	s.serveExtensionFile(w, `C:\ProgramData\Themisto\extensions\chromium\themisto.crx`, "application/x-chrome-extension")
}

func (s *Service) handleFirefoxExtensionXPI(w http.ResponseWriter, r *http.Request) {
	s.serveExtensionFile(w, `C:\ProgramData\Themisto\extensions\firefox\themisto.xpi`, "application/x-xpinstall")
}

func (s *Service) serveExtensionFile(w http.ResponseWriter, path, contentType string) {
	data, err := os.ReadFile(path)
	if err != nil {
		writeErrorJSON(w, http.StatusNotFound, "extension file not found")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Service) notifyUser(host string, decision domain.Decision, rule *domain.PolicyRule) {
	if s.notifier == nil {
		return
	}
	host = strings.TrimSpace(host)
	title := "Themisto Prompt Warning"
	message := fmt.Sprintf("Potentially sensitive prompt detected for %s.", host)
	if decision == domain.DecisionBlock {
		title = "Themisto Blocked Prompt"
		reason := "active policy"
		if rule != nil && strings.TrimSpace(rule.BlockReason) != "" {
			reason = strings.TrimSpace(rule.BlockReason)
		}
		message = fmt.Sprintf("A prompt to %s was blocked because %s", host, reason)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.notifier.Notify(ctx, title, message)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	setCORSHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeErrorJSON(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]interface{}{
		"error": strings.TrimSpace(message),
	})
}

func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}
