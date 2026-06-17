package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/themisto/backend/internal/store"
)

type policyRuleRequest struct {
	Name        string                  `json:"name"`
	Priority    int                     `json:"priority"`
	Enabled     *bool                   `json:"enabled,omitempty"`
	Action      string                  `json:"action"`
	Conditions  []store.PolicyCondition `json:"conditions"`
	BlockReason *string                 `json:"block_reason,omitempty"`

	// Legacy fields accepted for transition.
	MatchHost   *string `json:"match_host"`
	MatchPath   *string `json:"match_path"`
	MatchMethod *string `json:"match_method"`
	Decision    string  `json:"decision"`
}

func (s *Server) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}
	if err := s.store.EnsureManagedAIDLPPolicies(r.Context(), user.OrgID); err != nil {
		s.logger.Warn("ensure managed ai dlp policies", "org_id", user.OrgID, "error", err)
	}

	rules, err := s.store.ListPolicyRulesV2(r.Context(), user.OrgID)
	if err != nil {
		s.logger.Error("list policies", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	if rules == nil {
		rules = []store.PolicyRuleV2{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rules": rules,
		"total": len(rules),
	})
}

func (s *Server) handleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	var req policyRuleRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	rule, err := s.store.CreatePolicyRuleV2(r.Context(), user.OrgID, store.PolicyRuleV2Input{
		Name:        req.Name,
		Priority:    req.Priority,
		Enabled:     enabled,
		Action:      req.Action,
		Conditions:  req.Conditions,
		BlockReason: req.BlockReason,
		MatchHost:   req.MatchHost,
		MatchPath:   req.MatchPath,
		MatchMethod: req.MatchMethod,
		Decision:    req.Decision,
	})
	if err != nil {
		s.logger.Error("create policy", "error", err)
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) handleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	policyID := r.PathValue("id")

	var req policyRuleRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	rule, err := s.store.UpdatePolicyRuleV2(r.Context(), user.OrgID, policyID, store.PolicyRuleV2Input{
		Name:        req.Name,
		Priority:    req.Priority,
		Enabled:     enabled,
		Action:      req.Action,
		Conditions:  req.Conditions,
		BlockReason: req.BlockReason,
		MatchHost:   req.MatchHost,
		MatchPath:   req.MatchPath,
		MatchMethod: req.MatchMethod,
		Decision:    req.Decision,
	})
	if err != nil {
		s.logger.Error("update policy", "error", err)
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if rule == nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "policy rule not found")
		return
	}

	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) handleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	policyID := r.PathValue("id")

	if err := s.store.DeletePolicyRuleV2(r.Context(), user.OrgID, policyID); err != nil {
		s.logger.Error("delete policy", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type policyTestContext struct {
	Host                    string `json:"host"`
	Path                    string `json:"path"`
	Method                  string `json:"method"`
	Scheme                  string `json:"scheme"`
	ProcessName             string `json:"process_name"`
	ProcessPath             string `json:"process_path"`
	ProcessUser             string `json:"process_user"`
	ProcessBundle           string `json:"process_bundle"`
	ProcessSigner           string `json:"process_signer"`
	ProcessSigned           bool   `json:"process_signed"`
	ServiceCategory         string `json:"service_category"`
	AIVendor                string `json:"ai_vendor"`
	BodyContainsPII         bool   `json:"body_contains_pii"`
	BodyContainsCredentials bool   `json:"body_contains_credentials"`
	BodyContainsSourceCode  bool   `json:"body_contains_source_code"`
	BodyHasDLPMatch         bool   `json:"body_has_dlp_match"`
}

type testPolicyRequest struct {
	Rule    policyRuleRequest `json:"rule"`
	Context policyTestContext `json:"request_context"`
}

func (s *Server) handleTestPolicy(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated")
		return
	}

	var req testPolicyRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	conds := req.Rule.Conditions
	if len(conds) == 0 {
		if req.Rule.MatchHost != nil && strings.TrimSpace(*req.Rule.MatchHost) != "" {
			conds = append(conds, store.PolicyCondition{Field: "host", Operator: "contains", Value: strings.TrimSpace(*req.Rule.MatchHost)})
		}
		if req.Rule.MatchPath != nil && strings.TrimSpace(*req.Rule.MatchPath) != "" {
			conds = append(conds, store.PolicyCondition{Field: "path", Operator: "contains", Value: strings.TrimSpace(*req.Rule.MatchPath)})
		}
		if req.Rule.MatchMethod != nil && strings.TrimSpace(*req.Rule.MatchMethod) != "" {
			conds = append(conds, store.PolicyCondition{Field: "method", Operator: "eq", Value: strings.TrimSpace(*req.Rule.MatchMethod)})
		}
	}

	results := make([]map[string]interface{}, 0, len(conds))
	allMatched := true
	for _, c := range conds {
		fieldVal := resolvePolicyTestField(c.Field, req.Context)
		matched := evaluatePolicyOperator(c.Operator, fieldVal, c.Value)
		if c.Negate {
			matched = !matched
		}
		if !matched {
			allMatched = false
		}
		results = append(results, map[string]interface{}{
			"field":       c.Field,
			"operator":    c.Operator,
			"value":       c.Value,
			"negate":      c.Negate,
			"field_value": fieldVal,
			"matched":     matched,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"matches":              allMatched,
		"evaluated_conditions": results,
		"action":               normalizePolicyAction(req.Rule.Action, req.Rule.Decision),
	})
}

func resolvePolicyTestField(field string, ctx policyTestContext) string {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "host":
		return ctx.Host
	case "path":
		return ctx.Path
	case "method":
		return ctx.Method
	case "scheme":
		return ctx.Scheme
	case "process_name":
		return ctx.ProcessName
	case "process_path":
		return ctx.ProcessPath
	case "process_user":
		return ctx.ProcessUser
	case "process_bundle":
		return ctx.ProcessBundle
	case "process_signer":
		return ctx.ProcessSigner
	case "process_signed":
		if ctx.ProcessSigned {
			return "true"
		}
		return "false"
	case "service_category":
		return ctx.ServiceCategory
	case "ai_vendor":
		return ctx.AIVendor
	case "body_contains_pii":
		if ctx.BodyContainsPII {
			return "true"
		}
		return "false"
	case "body_contains_credentials":
		if ctx.BodyContainsCredentials {
			return "true"
		}
		return "false"
	case "body_contains_source_code":
		if ctx.BodyContainsSourceCode {
			return "true"
		}
		return "false"
	case "body_has_dlp_match":
		if ctx.BodyHasDLPMatch {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func evaluatePolicyOperator(op, fieldVal, expected string) bool {
	op = strings.ToLower(strings.TrimSpace(op))
	fieldValLower := strings.ToLower(fieldVal)
	expectedLower := strings.ToLower(expected)

	switch op {
	case "eq":
		return strings.EqualFold(fieldVal, expected)
	case "contains":
		return strings.Contains(fieldValLower, expectedLower)
	case "prefix":
		return strings.HasPrefix(fieldValLower, expectedLower)
	case "suffix":
		return strings.HasSuffix(fieldValLower, expectedLower)
	case "glob":
		matched, _ := filepath.Match(expectedLower, fieldValLower)
		return matched
	case "regex":
		re, err := regexp.Compile(expected)
		if err != nil {
			return false
		}
		return re.MatchString(fieldVal)
	default:
		return false
	}
}

func normalizePolicyAction(action, decision string) string {
	a := strings.ToLower(strings.TrimSpace(action))
	switch a {
	case "allow", "alert", "block":
		return a
	}

	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "allow", "forward":
		return "allow"
	case "block", "deny":
		return "block"
	default:
		return "alert"
	}
}
