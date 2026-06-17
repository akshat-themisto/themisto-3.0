package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/themisto/gateway/internal/config"
	"github.com/themisto/gateway/internal/mtls"
)

type SemanticHandler struct {
	cfg      config.PromptSemanticsConfig
	verifier *mtls.CertVerifier
	logger   *slog.Logger
	client   *http.Client
}

type semanticRequest struct {
	PromptText      string            `json:"prompt_text"`
	Policy          string            `json:"policy"`
	Surface         string            `json:"surface"`
	AppName         string            `json:"app_name,omitempty"`
	DestinationHost string            `json:"destination_host,omitempty"`
	DestinationPath string            `json:"destination_path,omitempty"`
	Vendor          string            `json:"vendor,omitempty"`
	ServiceCategory string            `json:"service_category,omitempty"`
	DLP             interface{}       `json:"dlp,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type semanticResult struct {
	Decision   string  `json:"decision"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason,omitempty"`
	Category   string  `json:"category,omitempty"`
	Source     string  `json:"source,omitempty"`
	Ambiguous  bool    `json:"ambiguous"`
	LatencyMs  int64   `json:"latency_ms,omitempty"`
}

func NewSemanticHandler(cfg config.PromptSemanticsConfig, verifier *mtls.CertVerifier, logger *slog.Logger) *SemanticHandler {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 900 * time.Millisecond
	}
	return &SemanticHandler{
		cfg:      cfg,
		verifier: verifier,
		logger:   logger,
		client:   &http.Client{Timeout: timeout},
	}
}

func (h *SemanticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	deviceID, orgID, ok := h.authorize(w, r)
	if !ok {
		return
	}
	if !h.cfg.Enabled {
		http.Error(w, "prompt semantic gateway classifier disabled", http.StatusServiceUnavailable)
		return
	}
	if strings.TrimSpace(h.cfg.QwenURL) == "" {
		http.Error(w, "qwen classifier URL not configured", http.StatusServiceUnavailable)
		return
	}

	var req semanticRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid semantic request", http.StatusBadRequest)
		return
	}
	req.PromptText = strings.TrimSpace(req.PromptText)
	req.Policy = strings.TrimSpace(req.Policy)
	if req.PromptText == "" || req.Policy == "" {
		http.Error(w, "prompt_text and policy are required", http.StatusBadRequest)
		return
	}
	if req.Metadata == nil {
		req.Metadata = map[string]string{}
	}
	req.Metadata["themisto_device_id"] = deviceID
	req.Metadata["themisto_org_id"] = orgID

	result, err := h.callQwen(r.Context(), req)
	if err != nil {
		h.logger.Warn("qwen prompt semantic classifier failed", "org_id", orgID, "device_id", deviceID, "error", err)
		http.Error(w, "qwen classifier unavailable", http.StatusBadGateway)
		return
	}
	if result.LatencyMs == 0 {
		result.LatencyMs = time.Since(start).Milliseconds()
	}
	normalizeSemanticResult(&result)
	if result.Source == "" {
		result.Source = "gateway_qwen"
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (h *SemanticHandler) authorize(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		http.Error(w, "client certificate required", http.StatusForbidden)
		return "", "", false
	}
	clientCert := r.TLS.PeerCertificates[0]
	serial := clientCert.SerialNumber.Text(16)
	revoked, err := h.verifier.IsRevoked(serial)
	if err != nil {
		h.logger.Warn("semantic revocation check error", "serial", serial, "error", err)
	}
	if revoked {
		http.Error(w, "certificate revoked", http.StatusForbidden)
		return "", "", false
	}
	deviceID, orgID, ok := h.verifier.LookupIdentity(serial)
	if !ok || strings.TrimSpace(deviceID) == "" || strings.TrimSpace(orgID) == "" {
		http.Error(w, "device identity unavailable", http.StatusForbidden)
		return "", "", false
	}
	return deviceID, orgID, true
}

func (h *SemanticHandler) callQwen(ctx context.Context, req semanticRequest) (semanticResult, error) {
	var out semanticResult
	body, err := json.Marshal(req)
	if err != nil {
		return out, fmt.Errorf("marshal qwen request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(h.cfg.QwenURL), bytes.NewReader(body))
	if err != nil {
		return out, fmt.Errorf("build qwen request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if token := strings.TrimSpace(h.cfg.QwenAPIKey); token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := h.client.Do(httpReq)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return out, fmt.Errorf("qwen returned %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, fmt.Errorf("decode qwen result: %w", err)
	}
	return out, nil
}

func normalizeSemanticResult(result *semanticResult) {
	result.Decision = strings.ToLower(strings.TrimSpace(result.Decision))
	switch result.Decision {
	case "allow", "allowed", "forward":
		result.Decision = "forward"
	case "block", "blocked", "deny":
		result.Decision = "block"
	case "alert", "warn", "review":
		result.Decision = "alert"
	default:
		result.Decision = "forward"
	}
	if result.Confidence < 0 {
		result.Confidence = 0
	}
	if result.Confidence > 1 {
		result.Confidence = 1
	}
	result.Reason = strings.TrimSpace(result.Reason)
	result.Category = strings.TrimSpace(result.Category)
	result.Source = strings.TrimSpace(result.Source)
}
