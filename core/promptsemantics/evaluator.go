package promptsemantics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/pkg/log"
)

type GatewayDoer interface {
	Do(ctx context.Context, req *domain.HTTPRequest) (*domain.HTTPResponse, error)
}

type Evaluator interface {
	Evaluate(ctx context.Context, req domain.PromptSemanticRequest) (*domain.PromptSemanticResult, error)
}

type CascadeEvaluator struct {
	localURL           string
	gatewayURL         string
	gateway            GatewayDoer
	httpClient         *http.Client
	localTimeout       time.Duration
	gatewayTimeout     time.Duration
	ambiguousThreshold float64
	gatewayEnabled     bool
	log                log.Logger
}

type CascadeOptions struct {
	LocalURL           string
	GatewayURL         string
	Gateway            GatewayDoer
	LocalTimeout       time.Duration
	GatewayTimeout     time.Duration
	AmbiguousThreshold float64
	GatewayEnabled     bool
	Logger             log.Logger
}

func NewCascadeEvaluator(opts CascadeOptions) *CascadeEvaluator {
	localTimeout := opts.LocalTimeout
	if localTimeout == 0 {
		localTimeout = 250 * time.Millisecond
	}
	gatewayTimeout := opts.GatewayTimeout
	if gatewayTimeout == 0 {
		gatewayTimeout = 900 * time.Millisecond
	}
	ambiguousThreshold := opts.AmbiguousThreshold
	if ambiguousThreshold == 0 {
		ambiguousThreshold = 0.58
	}
	return &CascadeEvaluator{
		localURL:           strings.TrimSpace(opts.LocalURL),
		gatewayURL:         strings.TrimRight(strings.TrimSpace(opts.GatewayURL), "/"),
		gateway:            opts.Gateway,
		httpClient:         &http.Client{Timeout: localTimeout},
		localTimeout:       localTimeout,
		gatewayTimeout:     gatewayTimeout,
		ambiguousThreshold: ambiguousThreshold,
		gatewayEnabled:     opts.GatewayEnabled,
		log:                opts.Logger,
	}
}

func (e *CascadeEvaluator) Evaluate(ctx context.Context, req domain.PromptSemanticRequest) (*domain.PromptSemanticResult, error) {
	local, err := e.evaluateLocal(ctx, req)
	if err != nil && e.log != nil {
		e.log.Warn("local prompt semantic classifier unavailable", "error", err)
	}
	if local != nil && !e.shouldEscalate(local) {
		return local, nil
	}

	if e.gatewayEnabled && e.gateway != nil && e.gatewayURL != "" {
		remote, remoteErr := e.evaluateGateway(ctx, req)
		if remoteErr == nil && remote != nil {
			return remote, nil
		}
		if remoteErr != nil && e.log != nil {
			e.log.Warn("gateway prompt semantic classifier unavailable", "error", remoteErr)
		}
	}

	if local != nil {
		return local, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("prompt semantic classifier unavailable")
}

func (e *CascadeEvaluator) shouldEscalate(result *domain.PromptSemanticResult) bool {
	if result == nil {
		return true
	}
	return result.Ambiguous || result.Confidence < e.ambiguousThreshold
}

func (e *CascadeEvaluator) evaluateLocal(ctx context.Context, req domain.PromptSemanticRequest) (*domain.PromptSemanticResult, error) {
	if e.localURL == "" {
		return nil, fmt.Errorf("local classifier URL is empty")
	}
	ctx, cancel := context.WithTimeout(ctx, e.localTimeout)
	defer cancel()

	var result domain.PromptSemanticResult
	if err := postJSON(ctx, e.httpClient, e.localURL, req, &result); err != nil {
		return nil, err
	}
	if result.Source == "" {
		result.Source = "local_deberta"
	}
	normalizeResult(&result)
	return &result, nil
}

func (e *CascadeEvaluator) evaluateGateway(ctx context.Context, req domain.PromptSemanticRequest) (*domain.PromptSemanticResult, error) {
	ctx, cancel := context.WithTimeout(ctx, e.gatewayTimeout)
	defer cancel()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal semantic request: %w", err)
	}
	resp, err := e.gateway.Do(ctx, &domain.HTTPRequest{
		Method: "POST",
		URL:    e.gatewayURL + "/v1/prompt/semantic-evaluate",
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
			"Accept":       {"application/json"},
		},
		Body: body,
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("gateway semantic classifier returned %d", resp.StatusCode)
	}
	var result domain.PromptSemanticResult
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("decode gateway semantic result: %w", err)
	}
	if result.Source == "" {
		result.Source = "gateway_qwen"
	}
	normalizeResult(&result)
	return &result, nil
}

func postJSON(ctx context.Context, client *http.Client, url string, in interface{}, out interface{}) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("classifier returned %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func normalizeResult(result *domain.PromptSemanticResult) {
	if result.Confidence < 0 {
		result.Confidence = 0
	}
	if result.Confidence > 1 {
		result.Confidence = 1
	}
	switch result.Decision {
	case domain.DecisionBlock, domain.DecisionAlert, domain.DecisionForward:
	default:
		result.Decision = domain.DecisionForward
	}
	result.Reason = strings.TrimSpace(result.Reason)
	result.Category = strings.TrimSpace(result.Category)
	result.Source = strings.TrimSpace(result.Source)
}
