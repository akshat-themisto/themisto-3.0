package policy

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/themisto/agent/core/domain"
)

// DefaultEngine implements the Engine interface. It holds a priority-ordered
// set of rules in memory and evaluates them against incoming RequestContexts.
type DefaultEngine struct {
	mu                        sync.RWMutex
	rules                     []domain.PolicyRule
	version                   string
	interception              domain.PolicyInterception
	promptEnforcementOverride string
	defaultDecision           domain.Decision
	compiled                  []compiledRule
}

type compiledRule struct {
	rule     domain.PolicyRule
	matchers []conditionMatcher
}

type conditionMatcher struct {
	cond  domain.RuleCondition
	regex *regexp.Regexp // non-nil for "regex" operator
}

// NewEngine creates an engine with the given default decision for unmatched
// requests.
func NewEngine(defaultDecision domain.Decision) *DefaultEngine {
	return &DefaultEngine{defaultDecision: defaultDecision}
}

// Apply evaluates the request against all rules in priority order and returns
// the first matching rule's decision.
func (e *DefaultEngine) Apply(ctx *domain.RequestContext) (domain.Decision, string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, cr := range e.compiled {
		if matchAll(cr, ctx) {
			return cr.rule.Decision, cr.rule.ID, nil
		}
	}
	return e.defaultDecision, "", nil
}

// Update atomically replaces the active rule set. On compile failure the old
// rules are retained.
func (e *DefaultEngine) Update(payload *domain.PolicyPayload) error {
	if payload == nil {
		return fmt.Errorf("policy: nil payload")
	}

	sorted := make([]domain.PolicyRule, len(payload.Rules))
	copy(sorted, payload.Rules)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	compiled, err := compileRules(sorted)
	if err != nil {
		return fmt.Errorf("policy: compile rules: %w", err)
	}

	e.mu.Lock()
	e.rules = sorted
	e.compiled = compiled
	e.version = payload.Version
	e.interception = normalizePolicyInterception(payload.Interception)
	e.promptEnforcementOverride = normalizePromptEnforcementOverride(payload.PromptEnforcementOverride)
	e.mu.Unlock()
	return nil
}

// Version returns the currently loaded policy version.
func (e *DefaultEngine) Version() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.version
}

// Interception returns the latest managed interception settings from policy sync.
func (e *DefaultEngine) Interception() domain.PolicyInterception {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := e.interception
	out.Domains = append([]string(nil), out.Domains...)
	out.Protocols = append([]string(nil), out.Protocols...)
	return out
}

// PromptEnforcementOverride returns the latest remotely synced prompt
// enforcement override. Empty means the local config mode remains effective.
func (e *DefaultEngine) PromptEnforcementOverride() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.promptEnforcementOverride
}

// ---------------------------------------------------------------------------
// Rule compilation and matching
// ---------------------------------------------------------------------------

func compileRules(rules []domain.PolicyRule) ([]compiledRule, error) {
	out := make([]compiledRule, len(rules))
	for i, r := range rules {
		cr := compiledRule{rule: r, matchers: make([]conditionMatcher, len(r.Conditions))}
		for j, cond := range r.Conditions {
			cm := conditionMatcher{cond: cond}
			if cond.Operator == "regex" {
				re, err := regexp.Compile(cond.Value)
				if err != nil {
					return nil, fmt.Errorf("rule %s condition %d: bad regex %q: %w", r.ID, j, cond.Value, err)
				}
				cm.regex = re
			}
			cr.matchers[j] = cm
		}
		out[i] = cr
	}
	return out, nil
}

func matchAll(cr compiledRule, ctx *domain.RequestContext) bool {
	for _, m := range cr.matchers {
		if !matchCondition(m, ctx) {
			return false
		}
	}
	return true
}

func matchCondition(m conditionMatcher, ctx *domain.RequestContext) bool {
	fieldVal := resolveField(m.cond.Field, ctx)
	result := evalOperator(m.cond.Operator, fieldVal, m.cond.Value, m.regex)
	if m.cond.Negate {
		return !result
	}
	return result
}

func resolveField(field string, ctx *domain.RequestContext) string {
	switch field {
	case "host":
		return ctx.Host
	case "path":
		return ctx.Path
	case "method":
		return ctx.Method
	case "scheme":
		return ctx.Scheme
	case "process_name":
		return ctx.Process.Name
	case "process_path":
		return ctx.Process.Path
	case "process_user":
		return ctx.Process.User
	case "process_bundle":
		return ctx.Process.BundleID
	case "process_signer":
		return ctx.Process.SignerID
	case "process_signed":
		if ctx.Process.Signed {
			return "true"
		}
		return "false"
	case "service_category":
		return ctx.ServiceCategory
	case "ai_vendor":
		return ctx.AIVendor
	case "capture_surface":
		return ctx.CaptureSurface
	case "body_contains_pii":
		if ctx.DLP.ContainsPII {
			return "true"
		}
		return "false"
	case "body_contains_credentials":
		if ctx.DLP.ContainsCredentials {
			return "true"
		}
		return "false"
	case "body_contains_source_code":
		if ctx.DLP.ContainsSourceCode {
			return "true"
		}
		return "false"
	case "body_has_dlp_match":
		if len(ctx.DLP.Matches) > 0 {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func evalOperator(op, fieldVal, pattern string, re *regexp.Regexp) bool {
	switch op {
	case "eq":
		return strings.EqualFold(fieldVal, pattern)
	case "contains":
		return strings.Contains(strings.ToLower(fieldVal), strings.ToLower(pattern))
	case "prefix":
		return strings.HasPrefix(strings.ToLower(fieldVal), strings.ToLower(pattern))
	case "suffix":
		return strings.HasSuffix(strings.ToLower(fieldVal), strings.ToLower(pattern))
	case "regex":
		if re != nil {
			return re.MatchString(fieldVal)
		}
		return false
	case "glob":
		matched, _ := filepath.Match(strings.ToLower(pattern), strings.ToLower(fieldVal))
		return matched
	default:
		return false
	}
}

// RuleByID returns a copy of the loaded rule for the given ID, if present.
func (e *DefaultEngine) RuleByID(id string) *domain.PolicyRule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, r := range e.rules {
		if r.ID == id {
			cp := r
			return &cp
		}
	}
	return nil
}

func normalizePolicyInterception(raw domain.PolicyInterception) domain.PolicyInterception {
	out := domain.PolicyInterception{
		Enabled:     raw.Enabled,
		FailMode:    strings.ToLower(strings.TrimSpace(raw.FailMode)),
		CaptureMode: strings.ToLower(strings.TrimSpace(raw.CaptureMode)),
		Scope:       strings.ToLower(strings.TrimSpace(raw.Scope)),
	}
	if out.FailMode == "" {
		out.FailMode = domain.HTTPSInterceptFailOpen
	}
	if out.CaptureMode == "" {
		out.CaptureMode = domain.InterceptCaptureModeEncryptedFullBody
	}
	switch out.Scope {
	case domain.InterceptScopeAPIOnly, domain.InterceptScopeHybridAllowlist:
	default:
		out.Scope = domain.InterceptScopeAPIOnly
	}

	protocolSet := make(map[string]struct{}, len(raw.Protocols))
	for _, p := range raw.Protocols {
		v := strings.ToLower(strings.TrimSpace(p))
		switch v {
		case domain.InterceptProtocolHTTP, domain.InterceptProtocolWebSocket, domain.InterceptProtocolGRPC:
			protocolSet[v] = struct{}{}
		}
	}
	if len(protocolSet) == 0 {
		protocolSet[domain.InterceptProtocolHTTP] = struct{}{}
	}
	out.Protocols = make([]string, 0, len(protocolSet))
	for p := range protocolSet {
		out.Protocols = append(out.Protocols, p)
	}
	sort.Strings(out.Protocols)

	domainSet := make(map[string]struct{}, len(raw.Domains))
	for _, d := range raw.Domains {
		if n := normalizeInterceptDomain(d); n != "" {
			domainSet[n] = struct{}{}
		}
	}
	for d := range domainSet {
		for _, alias := range vendorAliasesForDomain(d) {
			if n := normalizeInterceptDomain(alias); n != "" {
				domainSet[n] = struct{}{}
			}
		}
	}
	out.Domains = make([]string, 0, len(domainSet))
	for d := range domainSet {
		out.Domains = append(out.Domains, d)
	}
	sort.Strings(out.Domains)
	return out
}

func normalizePromptEnforcementOverride(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case domain.PromptEnforcementModeMonitor:
		return domain.PromptEnforcementModeMonitor
	case domain.PromptEnforcementModeAlert:
		return domain.PromptEnforcementModeAlert
	case domain.PromptEnforcementModeEnforce:
		return domain.PromptEnforcementModeEnforce
	default:
		return ""
	}
}

func normalizeInterceptDomain(raw string) string {
	v := strings.TrimSpace(strings.ToLower(raw))
	if v == "" {
		return ""
	}
	v = strings.TrimPrefix(v, "*.")
	if strings.Contains(v, "://") {
		if parsed, err := url.Parse(v); err == nil {
			v = parsed.Host
		}
	}
	if strings.Contains(v, "/") {
		v = strings.SplitN(v, "/", 2)[0]
	}
	if host, _, err := net.SplitHostPort(v); err == nil {
		v = host
	}
	v = strings.TrimSuffix(v, ".")
	v = strings.TrimPrefix(v, ".")
	if v == "" || !strings.Contains(v, ".") {
		return ""
	}
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			continue
		}
		return ""
	}
	return v
}

func vendorAliasesForDomain(domain string) []string {
	switch {
	case domain == "api.openai.com":
		return []string{"api.openai.com"}
	case domain == "openai.com", domain == "chat.openai.com", domain == "chatgpt.com":
		return []string{"api.openai.com"}
	case strings.HasSuffix(domain, "anthropic.com") || domain == "claude.ai":
		return []string{"api.anthropic.com"}
	case strings.HasSuffix(domain, "google.com") || strings.HasSuffix(domain, "googleapis.com") || strings.Contains(domain, "gemini"):
		return []string{"generativelanguage.googleapis.com", "content-gemini.googleapis.com"}
	case strings.HasSuffix(domain, "githubcopilot.com") || strings.HasSuffix(domain, "githubusercontent.com"):
		return []string{"api.githubcopilot.com", "copilot-proxy.githubusercontent.com"}
	case strings.Contains(domain, "cursor"):
		return []string{"api2.cursor.sh"}
	default:
		return nil
	}
}
