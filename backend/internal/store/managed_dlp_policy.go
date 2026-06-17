package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type managedPolicyDefinition struct {
	Name        string
	Priority    int
	Action      string
	BlockReason string
	Conditions  []PolicyCondition
}

var managedAIDLPPolicyDefinitions = []managedPolicyDefinition{
	{
		Name:        "Managed: AI Credentials Block",
		Priority:    8000,
		Action:      "block",
		BlockReason: "Sensitive credentials were detected in AI traffic.",
		Conditions: []PolicyCondition{
			{Field: "service_category", Operator: "prefix", Value: "ai_"},
			{Field: "body_contains_credentials", Operator: "eq", Value: "true"},
		},
	},
	{
		Name:        "Managed: AI PII Alert",
		Priority:    8100,
		Action:      "alert",
		BlockReason: "Potentially sensitive personal data was detected in AI traffic.",
		Conditions: []PolicyCondition{
			{Field: "service_category", Operator: "prefix", Value: "ai_"},
			{Field: "body_contains_pii", Operator: "eq", Value: "true"},
		},
	},
	{
		Name:        "Managed: AI Source Code Alert",
		Priority:    8110,
		Action:      "alert",
		BlockReason: "Potentially sensitive source code was detected in AI traffic.",
		Conditions: []PolicyCondition{
			{Field: "service_category", Operator: "prefix", Value: "ai_"},
			{Field: "body_contains_source_code", Operator: "eq", Value: "true"},
		},
	},
	{
		Name:        "Managed: AI DLP Match Alert",
		Priority:    8120,
		Action:      "alert",
		BlockReason: "Policy keyword or custom DLP rule matched in AI traffic.",
		Conditions: []PolicyCondition{
			{Field: "service_category", Operator: "prefix", Value: "ai_"},
			{Field: "body_has_dlp_match", Operator: "eq", Value: "true"},
		},
	},
}

// EnsureManagedAIDLPPolicies guarantees baseline tiered enforcement defaults for an org.
func (s *Store) EnsureManagedAIDLPPolicies(ctx context.Context, orgID string) error {
	for _, def := range managedAIDLPPolicyDefinitions {
		if err := s.upsertManagedPolicy(ctx, orgID, def); err != nil {
			return err
		}
	}
	return nil
}

// EnsureManagedAIDLPPoliciesForAllOrgs applies baseline defaults for every active org.
func (s *Store) EnsureManagedAIDLPPoliciesForAllOrgs(ctx context.Context) error {
	orgs, err := s.GetOrganizationByAPIKeyHash(ctx)
	if err != nil {
		return err
	}
	for _, org := range orgs {
		if err := s.EnsureManagedAIDLPPolicies(ctx, org.ID); err != nil {
			return fmt.Errorf("org %s: %w", org.ID, err)
		}
	}
	return nil
}

func (s *Store) upsertManagedPolicy(ctx context.Context, orgID string, def managedPolicyDefinition) error {
	rawConds, err := json.Marshal(def.Conditions)
	if err != nil {
		return fmt.Errorf("marshal managed policy conditions: %w", err)
	}
	blockReason := def.BlockReason

	var existingID string
	err = s.DB.QueryRowContext(ctx, `
		SELECT id
		FROM policy_rules
		WHERE org_id = $1::uuid AND name = $2
		LIMIT 1`, orgID, def.Name).Scan(&existingID)
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	if existingID == "" {
		_, err = s.DB.ExecContext(ctx, `
			INSERT INTO policy_rules (
				org_id, name, priority, enabled, action, conditions, block_reason, decision
			) VALUES ($1::uuid, $2, $3, true, $4, $5::jsonb, $6, $7)`,
			orgID, def.Name, def.Priority, def.Action, string(rawConds), blockReason, actionToLegacyDecision(def.Action),
		)
		return err
	}

	_, err = s.DB.ExecContext(ctx, `
		UPDATE policy_rules
		SET priority = $1,
		    enabled = true,
		    action = $2,
		    conditions = $3::jsonb,
		    block_reason = $4,
		    decision = $5,
		    updated_at = now()
		WHERE id = $6`,
		def.Priority, def.Action, string(rawConds), blockReason, actionToLegacyDecision(def.Action), existingID,
	)
	return err
}
