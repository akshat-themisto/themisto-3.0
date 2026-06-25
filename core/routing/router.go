package routing

import (
	"github.com/themisto/agent/core/domain"
	"github.com/themisto/agent/core/policy"
	"github.com/themisto/agent/pkg/log"
)

type policyRuleLookup interface {
	RuleByID(id string) *domain.PolicyRule
}

type promptEnforcementOverrideProvider interface {
	PromptEnforcementOverride() string
}

type policyVersionProvider interface {
	Version() string
}

// DefaultRouter implements the Router interface by delegating to the policy
// Engine. If the engine returns an error, the configured default decision is
// used as a fallback.
type DefaultRouter struct {
	engine          policy.Engine
	defaultDecision domain.Decision
	log             log.Logger
}

// NewRouter creates a router that evaluates requests against the policy engine.
func NewRouter(engine policy.Engine, defaultDecision domain.Decision, logger log.Logger) *DefaultRouter {
	return &DefaultRouter{
		engine:          engine,
		defaultDecision: defaultDecision,
		log:             logger,
	}
}

// Route evaluates the request context against the active policy and returns
// the resulting decision and matched rule ID.
func (r *DefaultRouter) Route(ctx *domain.RequestContext) (domain.Decision, string, error) {
	decision, ruleID, err := r.engine.Apply(ctx)
	if err != nil {
		r.log.Warn("routing engine error, using default decision",
			"host", ctx.Host,
			"error", err,
			"default", r.defaultDecision.String(),
		)
		return r.defaultDecision, "", nil
	}
	return decision, ruleID, nil
}

// LookupRule returns the loaded policy rule by ID when the underlying
// policy engine supports direct rule lookup.
func (r *DefaultRouter) LookupRule(id string) *domain.PolicyRule {
	if id == "" {
		return nil
	}
	lookup, ok := r.engine.(policyRuleLookup)
	if !ok {
		return nil
	}
	return lookup.RuleByID(id)
}

// PromptEnforcementOverride returns a remotely synced prompt enforcement
// override when the underlying policy engine supports it.
func (r *DefaultRouter) PromptEnforcementOverride() string {
	provider, ok := r.engine.(promptEnforcementOverrideProvider)
	if !ok {
		return ""
	}
	return provider.PromptEnforcementOverride()
}

// PolicyVersion returns the active policy version when available.
func (r *DefaultRouter) PolicyVersion() string {
	provider, ok := r.engine.(policyVersionProvider)
	if !ok {
		return ""
	}
	return provider.Version()
}
