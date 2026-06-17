package policy

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/themisto/gateway/internal/store"
)

type Decision string

const (
	Allow   Decision = "allow"
	Block   Decision = "block"
	LogOnly Decision = "log_only"
)

type Engine struct {
	store *store.Store
	mu    sync.RWMutex
	rules []store.PolicyRule
}

func NewEngine(db *store.Store) *Engine {
	e := &Engine{store: db}
	e.Reload()
	return e
}

func (e *Engine) Reload() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rules, err := e.store.GetAllPolicyRules(ctx)
	if err != nil {
		return
	}

	e.mu.Lock()
	e.rules = rules
	e.mu.Unlock()
}

func (e *Engine) StartReloader(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				e.Reload()
			}
		}
	}()
}

func (e *Engine) Rules() []store.PolicyRule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]store.PolicyRule, len(e.rules))
	copy(out, e.rules)
	return out
}

func (e *Engine) RulesForOrg(orgID string) []store.PolicyRule {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return []store.PolicyRule{}
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	out := make([]store.PolicyRule, 0, len(e.rules))
	for _, r := range e.rules {
		if strings.EqualFold(strings.TrimSpace(r.OrgID), orgID) {
			out = append(out, r)
		}
	}
	return out
}

func (e *Engine) Evaluate(orgID, host, path, method string) (Decision, *string) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return Allow, nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, r := range e.rules {
		if !strings.EqualFold(strings.TrimSpace(r.OrgID), orgID) {
			continue
		}
		if matchRule(r, host, path, method) {
			id := r.ID
			return mapRuleDecision(r.Action, r.Decision), &id
		}
	}
	return Allow, nil
}

func mapRuleDecision(action, decision string) Decision {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "allow":
		return Allow
	case "block":
		return Block
	case "alert":
		return LogOnly
	}

	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "block":
		return Block
	case "log_only":
		return LogOnly
	default:
		return Allow
	}
}

func matchRule(r store.PolicyRule, host, path, method string) bool {
	if len(r.Conditions) > 0 {
		for _, c := range r.Conditions {
			fieldVal, supported := resolveField(c.Field, host, path, method)
			if !supported {
				// Gateway safety-net only evaluates host/path/method scoped conditions.
				return false
			}
			matched := evalOperator(c.Operator, fieldVal, c.Value)
			if c.Negate {
				matched = !matched
			}
			if !matched {
				return false
			}
		}
		return true
	}

	if r.MatchHost != nil && *r.MatchHost != "" {
		if !strings.Contains(strings.ToLower(host), strings.ToLower(*r.MatchHost)) {
			return false
		}
	}
	if r.MatchPath != nil && *r.MatchPath != "" {
		if !strings.Contains(strings.ToLower(path), strings.ToLower(*r.MatchPath)) {
			return false
		}
	}
	if r.MatchMethod != nil && *r.MatchMethod != "" {
		if !strings.EqualFold(method, *r.MatchMethod) {
			return false
		}
	}
	return true
}

func resolveField(field, host, path, method string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "host":
		return host, true
	case "path":
		return path, true
	case "method":
		return method, true
	default:
		return "", false
	}
}

func evalOperator(op, actual, expected string) bool {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "eq":
		return strings.EqualFold(actual, expected)
	case "contains":
		return strings.Contains(strings.ToLower(actual), strings.ToLower(expected))
	case "prefix":
		return strings.HasPrefix(strings.ToLower(actual), strings.ToLower(expected))
	case "suffix":
		return strings.HasSuffix(strings.ToLower(actual), strings.ToLower(expected))
	case "glob":
		matched, _ := filepath.Match(strings.ToLower(expected), strings.ToLower(actual))
		return matched
	case "regex":
		re, err := regexp.Compile(expected)
		if err != nil {
			return false
		}
		return re.MatchString(actual)
	default:
		return false
	}
}
