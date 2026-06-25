package handler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/themisto/gateway/internal/mtls"
	"github.com/themisto/gateway/internal/policy"
	"github.com/themisto/gateway/internal/store"
)

type PolicyHandler struct {
	engine   *policy.Engine
	store    *store.Store
	verifier *mtls.CertVerifier
	logger   *slog.Logger
}

func NewPolicyHandler(engine *policy.Engine, st *store.Store, verifier *mtls.CertVerifier, logger *slog.Logger) *PolicyHandler {
	return &PolicyHandler{engine: engine, store: st, verifier: verifier, logger: logger}
}

type policyPayload struct {
	Version                   string             `json:"version"`
	Rules                     []policyRule       `json:"rules"`
	Interception              policyInterception `json:"interception"`
	PromptEnforcementOverride string             `json:"prompt_enforcement_override,omitempty"`
}

type policyRule struct {
	ID          string          `json:"id"`
	Priority    int             `json:"priority"`
	Decision    string          `json:"decision"`
	Conditions  []ruleCondition `json:"conditions"`
	BlockReason string          `json:"block_reason,omitempty"`
}

type ruleCondition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
	Negate   bool   `json:"negate,omitempty"`
}

type policyInterception struct {
	Enabled     bool     `json:"enabled"`
	Domains     []string `json:"domains"`
	Protocols   []string `json:"protocols"`
	FailMode    string   `json:"fail_mode"`
	CaptureMode string   `json:"capture_mode"`
	Scope       string   `json:"scope"`
}

func (h *PolicyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		http.Error(w, "client certificate required", http.StatusForbidden)
		return
	}

	serial := r.TLS.PeerCertificates[0].SerialNumber.Text(16)
	revoked, err := h.verifier.IsRevoked(serial)
	if err != nil {
		h.logger.Warn("policy sync revocation check error", "serial", serial, "error", err)
	}
	if revoked {
		http.Error(w, "certificate revoked", http.StatusForbidden)
		return
	}

	_, orgID, ok := h.verifier.LookupIdentity(serial)
	if !ok || strings.TrimSpace(orgID) == "" {
		http.Error(w, "device identity unavailable", http.StatusForbidden)
		return
	}

	rules := h.engine.RulesForOrg(orgID)
	payload := convertRules(rules)
	if override, err := h.store.GetPromptEnforcementOverride(r.Context(), orgID); err != nil {
		h.logger.Warn("get prompt enforcement override failed", "org_id", orgID, "error", err)
	} else if override = normalizePromptEnforcementOverride(override); override != "" {
		payload.PromptEnforcementOverride = override
	}
	if domains, err := h.store.ListEffectiveAIInterceptDomains(r.Context(), orgID); err != nil {
		h.logger.Warn("list intercept domains failed, using defaults", "org_id", orgID, "error", err)
		payload.Interception = defaultInterceptionConfig(nil)
	} else {
		payload.Interception = defaultInterceptionConfig(domains)
	}

	etag := computeETag(payload)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", etag)
	w.Header().Set("X-Themisto-Poll-Interval", "30")
	_ = json.NewEncoder(w).Encode(payload)
}

func convertRules(dbRules []store.PolicyRule) policyPayload {
	rules := make([]policyRule, 0, len(dbRules))
	for _, r := range dbRules {
		conds := make([]ruleCondition, 0, len(r.Conditions)+3)
		if len(r.Conditions) > 0 {
			for _, c := range r.Conditions {
				conds = append(conds, ruleCondition{
					Field:    strings.TrimSpace(c.Field),
					Operator: strings.TrimSpace(c.Operator),
					Value:    strings.TrimSpace(c.Value),
					Negate:   c.Negate,
				})
			}
		} else {
			if r.MatchHost != nil && *r.MatchHost != "" {
				conds = append(conds, ruleCondition{Field: "host", Operator: "contains", Value: *r.MatchHost})
			}
			if r.MatchPath != nil && *r.MatchPath != "" {
				conds = append(conds, ruleCondition{Field: "path", Operator: "contains", Value: *r.MatchPath})
			}
			if r.MatchMethod != nil && *r.MatchMethod != "" {
				conds = append(conds, ruleCondition{Field: "method", Operator: "eq", Value: *r.MatchMethod})
			}
		}

		pr := policyRule{
			ID:         r.ID,
			Priority:   r.Priority,
			Decision:   mapDecision(r.Action, r.Decision),
			Conditions: conds,
		}
		if r.BlockReason != nil {
			pr.BlockReason = *r.BlockReason
		}
		rules = append(rules, pr)
	}

	return policyPayload{
		Version: fmt.Sprintf("v%d", len(dbRules)),
		Rules:   rules,
	}
}

func defaultInterceptionConfig(domains []string) policyInterception {
	if len(domains) == 0 {
		domains = []string{}
	}
	return policyInterception{
		Enabled:     false,
		Domains:     domains,
		Protocols:   []string{"http", "websocket", "grpc"},
		FailMode:    "fail_open",
		CaptureMode: "encrypted_full_body",
		Scope:       "api_only",
	}
}

func mapDecision(action, decision string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "allow":
		return "forward"
	case "block":
		return "block"
	case "alert":
		return "alert"
	}

	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "block":
		return "block"
	case "log_only":
		return "alert"
	default:
		return "forward"
	}
}

func normalizePromptEnforcementOverride(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "monitor":
		return "monitor"
	case "alert":
		return "alert"
	case "enforce":
		return "enforce"
	default:
		return ""
	}
}

func computeETag(p policyPayload) string {
	data, _ := json.Marshal(p)
	h := sha256.Sum256(data)
	return fmt.Sprintf(`"%x"`, h[:8])
}
