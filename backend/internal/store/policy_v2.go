package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PolicyCondition describes a match predicate for policy rule v2.
type PolicyCondition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
	Negate   bool   `json:"negate,omitempty"`
}

// PolicyRuleV2 is the centralized policy model used by dashboard + agent sync.
type PolicyRuleV2 struct {
	ID          string            `json:"id"`
	OrgID       string            `json:"org_id"`
	Name        string            `json:"name"`
	Priority    int               `json:"priority"`
	Enabled     bool              `json:"enabled"`
	Action      string            `json:"action"`
	Conditions  []PolicyCondition `json:"conditions"`
	BlockReason *string           `json:"block_reason,omitempty"`

	// Legacy compatibility fields still accepted/exposed during migration.
	MatchHost   *string   `json:"match_host,omitempty"`
	MatchPath   *string   `json:"match_path,omitempty"`
	MatchMethod *string   `json:"match_method,omitempty"`
	Decision    string    `json:"decision"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PolicyRuleV2Input is used by create/update API handlers.
type PolicyRuleV2Input struct {
	Name        string
	Priority    int
	Enabled     bool
	Action      string
	Conditions  []PolicyCondition
	BlockReason *string

	// Legacy inputs accepted during transition.
	MatchHost   *string
	MatchPath   *string
	MatchMethod *string
	Decision    string
}

func (s *Store) ListPolicyRulesV2(ctx context.Context, orgID string) ([]PolicyRuleV2, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, org_id,
		       COALESCE(NULLIF(name, ''), 'Policy rule ' || priority::text) AS name,
		       priority, enabled,
		       COALESCE(NULLIF(action, ''), CASE decision WHEN 'allow' THEN 'allow' WHEN 'block' THEN 'block' ELSE 'alert' END) AS action,
		       conditions, block_reason,
		       match_host, match_path, match_method, decision,
		       created_at, updated_at
		FROM policy_rules
		WHERE org_id = $1
		ORDER BY priority`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []PolicyRuleV2
	for rows.Next() {
		var r PolicyRuleV2
		var rawConds []byte
		if err := rows.Scan(
			&r.ID, &r.OrgID, &r.Name,
			&r.Priority, &r.Enabled,
			&r.Action,
			&rawConds, &r.BlockReason,
			&r.MatchHost, &r.MatchPath, &r.MatchMethod, &r.Decision,
			&r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, err
		}

		r.Action = normalizePolicyAction(r.Action, r.Decision)
		if err := decodeConditions(rawConds, &r.Conditions); err != nil {
			return nil, err
		}
		if len(r.Conditions) == 0 {
			r.Conditions = legacyConditionsFromFields(r.MatchHost, r.MatchPath, r.MatchMethod)
		}
		r.Decision = actionToLegacyDecision(r.Action)
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func (s *Store) CreatePolicyRuleV2(ctx context.Context, orgID string, in PolicyRuleV2Input) (*PolicyRuleV2, error) {
	norm, err := normalizePolicyInput(in)
	if err != nil {
		return nil, err
	}

	rawConds, err := json.Marshal(norm.Conditions)
	if err != nil {
		return nil, fmt.Errorf("marshal conditions: %w", err)
	}

	matchHost, matchPath, matchMethod := legacyFieldsFromConditions(norm.Conditions)
	if matchHost == nil {
		matchHost = norm.MatchHost
	}
	if matchPath == nil {
		matchPath = norm.MatchPath
	}
	if matchMethod == nil {
		matchMethod = norm.MatchMethod
	}

	rule := &PolicyRuleV2{}
	var storedConds []byte
	err = s.DB.QueryRowContext(ctx, `
		INSERT INTO policy_rules (
			org_id, name, priority, enabled, action, conditions, block_reason,
			match_host, match_path, match_method, decision
		)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10,$11)
		RETURNING id, org_id, name, priority, enabled, action, conditions, block_reason,
		          match_host, match_path, match_method, decision, created_at, updated_at`,
		orgID, norm.Name, norm.Priority, norm.Enabled, norm.Action, string(rawConds), norm.BlockReason,
		matchHost, matchPath, matchMethod, actionToLegacyDecision(norm.Action),
	).Scan(
		&rule.ID, &rule.OrgID, &rule.Name, &rule.Priority, &rule.Enabled, &rule.Action,
		&storedConds, &rule.BlockReason,
		&rule.MatchHost, &rule.MatchPath, &rule.MatchMethod, &rule.Decision,
		&rule.CreatedAt, &rule.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if err := decodeConditions(storedConds, &rule.Conditions); err != nil {
		return nil, err
	}
	if len(rule.Conditions) == 0 {
		rule.Conditions = legacyConditionsFromFields(rule.MatchHost, rule.MatchPath, rule.MatchMethod)
	}
	rule.Decision = actionToLegacyDecision(rule.Action)
	return rule, nil
}

func (s *Store) UpdatePolicyRuleV2(ctx context.Context, orgID, id string, in PolicyRuleV2Input) (*PolicyRuleV2, error) {
	norm, err := normalizePolicyInput(in)
	if err != nil {
		return nil, err
	}

	rawConds, err := json.Marshal(norm.Conditions)
	if err != nil {
		return nil, fmt.Errorf("marshal conditions: %w", err)
	}

	matchHost, matchPath, matchMethod := legacyFieldsFromConditions(norm.Conditions)
	if matchHost == nil {
		matchHost = norm.MatchHost
	}
	if matchPath == nil {
		matchPath = norm.MatchPath
	}
	if matchMethod == nil {
		matchMethod = norm.MatchMethod
	}

	rule := &PolicyRuleV2{}
	var storedConds []byte
	err = s.DB.QueryRowContext(ctx, `
		UPDATE policy_rules
		SET name = $1,
		    priority = $2,
		    enabled = $3,
		    action = $4,
		    conditions = $5::jsonb,
		    block_reason = $6,
		    match_host = $7,
		    match_path = $8,
		    match_method = $9,
		    decision = $10,
		    updated_at = now()
		WHERE id = $11 AND org_id = $12
		RETURNING id, org_id, name, priority, enabled, action, conditions, block_reason,
		          match_host, match_path, match_method, decision, created_at, updated_at`,
		norm.Name, norm.Priority, norm.Enabled, norm.Action, string(rawConds), norm.BlockReason,
		matchHost, matchPath, matchMethod, actionToLegacyDecision(norm.Action), id, orgID,
	).Scan(
		&rule.ID, &rule.OrgID, &rule.Name, &rule.Priority, &rule.Enabled, &rule.Action,
		&storedConds, &rule.BlockReason,
		&rule.MatchHost, &rule.MatchPath, &rule.MatchMethod, &rule.Decision,
		&rule.CreatedAt, &rule.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := decodeConditions(storedConds, &rule.Conditions); err != nil {
		return nil, err
	}
	if len(rule.Conditions) == 0 {
		rule.Conditions = legacyConditionsFromFields(rule.MatchHost, rule.MatchPath, rule.MatchMethod)
	}
	rule.Decision = actionToLegacyDecision(rule.Action)
	return rule, nil
}

func (s *Store) DeletePolicyRuleV2(ctx context.Context, orgID, id string) error {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM policy_rules WHERE id = $1 AND org_id = $2`, id, orgID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func normalizePolicyInput(in PolicyRuleV2Input) (PolicyRuleV2Input, error) {
	out := in

	if out.Priority < 1 {
		out.Priority = 10
	}
	if strings.TrimSpace(out.Name) == "" {
		out.Name = fmt.Sprintf("Policy rule %d", out.Priority)
	} else {
		out.Name = strings.TrimSpace(out.Name)
	}

	if len(out.Conditions) == 0 {
		out.Conditions = legacyConditionsFromFields(out.MatchHost, out.MatchPath, out.MatchMethod)
	}
	for i := range out.Conditions {
		out.Conditions[i].Field = strings.TrimSpace(out.Conditions[i].Field)
		out.Conditions[i].Operator = strings.TrimSpace(out.Conditions[i].Operator)
		out.Conditions[i].Value = strings.TrimSpace(out.Conditions[i].Value)
		if out.Conditions[i].Field == "" || out.Conditions[i].Operator == "" {
			return out, fmt.Errorf("policy condition %d missing field/operator", i)
		}
	}

	rawAction := strings.ToLower(strings.TrimSpace(out.Action))
	switch rawAction {
	case "":
		out.Action = normalizePolicyAction("", out.Decision)
	case "allow", "alert", "block":
		out.Action = rawAction
	default:
		return out, fmt.Errorf("invalid action %q", out.Action)
	}

	if out.BlockReason != nil {
		v := strings.TrimSpace(*out.BlockReason)
		if v == "" {
			out.BlockReason = nil
		} else {
			out.BlockReason = &v
		}
	}

	return out, nil
}

func normalizePolicyAction(action, decision string) string {
	a := strings.ToLower(strings.TrimSpace(action))
	if a != "" {
		switch a {
		case "allow", "alert", "block":
			return a
		}
	}
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "allow", "forward":
		return "allow"
	case "block", "deny":
		return "block"
	case "log_only", "bypass", "alert":
		return "alert"
	default:
		return "alert"
	}
}

func actionToLegacyDecision(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "allow":
		return "allow"
	case "block":
		return "block"
	default:
		return "log_only"
	}
}

func decodeConditions(raw []byte, out *[]PolicyCondition) error {
	if len(raw) == 0 {
		*out = []PolicyCondition{}
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode conditions: %w", err)
	}
	if *out == nil {
		*out = []PolicyCondition{}
	}
	return nil
}

func legacyConditionsFromFields(matchHost, matchPath, matchMethod *string) []PolicyCondition {
	conds := make([]PolicyCondition, 0, 3)
	if matchHost != nil && strings.TrimSpace(*matchHost) != "" {
		conds = append(conds, PolicyCondition{Field: "host", Operator: "contains", Value: strings.TrimSpace(*matchHost)})
	}
	if matchPath != nil && strings.TrimSpace(*matchPath) != "" {
		conds = append(conds, PolicyCondition{Field: "path", Operator: "contains", Value: strings.TrimSpace(*matchPath)})
	}
	if matchMethod != nil && strings.TrimSpace(*matchMethod) != "" {
		conds = append(conds, PolicyCondition{Field: "method", Operator: "eq", Value: strings.TrimSpace(*matchMethod)})
	}
	return conds
}

func legacyFieldsFromConditions(conditions []PolicyCondition) (matchHost, matchPath, matchMethod *string) {
	for _, c := range conditions {
		field := strings.ToLower(strings.TrimSpace(c.Field))
		op := strings.ToLower(strings.TrimSpace(c.Operator))
		val := strings.TrimSpace(c.Value)
		if val == "" || c.Negate {
			continue
		}

		switch field {
		case "host":
			if matchHost == nil && (op == "contains" || op == "eq" || op == "suffix") {
				v := val
				matchHost = &v
			}
		case "path":
			if matchPath == nil && (op == "contains" || op == "eq" || op == "prefix") {
				v := val
				matchPath = &v
			}
		case "method":
			if matchMethod == nil && op == "eq" {
				v := strings.ToUpper(val)
				matchMethod = &v
			}
		}
	}
	return
}
