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

// AIVendorGovernance stores sanctioned/unsanctioned state per org+vendor.
type AIVendorGovernance struct {
	ID         int64     `json:"id"`
	OrgID      string    `json:"org_id"`
	AIVendor   string    `json:"ai_vendor"`
	Sanctioned bool      `json:"sanctioned"`
	RiskTier   string    `json:"risk_tier"`
	Notes      *string   `json:"notes,omitempty"`
	UpdatedBy  *string   `json:"updated_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (s *Store) GetAIVendorGovernance(ctx context.Context, orgID, aiVendor string) (*AIVendorGovernance, error) {
	vendor := strings.ToLower(strings.TrimSpace(aiVendor))
	if vendor == "" {
		return nil, fmt.Errorf("ai_vendor is required")
	}

	row := &AIVendorGovernance{}
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, org_id::text, ai_vendor, sanctioned, risk_tier, notes, updated_by, created_at, updated_at
		FROM ai_vendor_governance
		WHERE org_id::text = $1 AND ai_vendor = $2
		LIMIT 1`, orgID, vendor).Scan(
		&row.ID, &row.OrgID, &row.AIVendor, &row.Sanctioned, &row.RiskTier,
		&row.Notes, &row.UpdatedBy, &row.CreatedAt, &row.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Store) ListAIVendorGovernance(ctx context.Context, orgID string) ([]AIVendorGovernance, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, org_id::text, ai_vendor, sanctioned, risk_tier, notes, updated_by, created_at, updated_at
		FROM ai_vendor_governance
		WHERE org_id::text = $1
		ORDER BY ai_vendor`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AIVendorGovernance, 0)
	for rows.Next() {
		var row AIVendorGovernance
		if err := rows.Scan(
			&row.ID, &row.OrgID, &row.AIVendor, &row.Sanctioned, &row.RiskTier,
			&row.Notes, &row.UpdatedBy, &row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) UpsertAIVendorGovernance(ctx context.Context, orgID string, in AIVendorGovernance) (*AIVendorGovernance, error) {
	vendor := strings.ToLower(strings.TrimSpace(in.AIVendor))
	if vendor == "" {
		return nil, fmt.Errorf("ai_vendor is required")
	}
	riskTier := strings.ToLower(strings.TrimSpace(in.RiskTier))
	if riskTier == "" {
		riskTier = "medium"
	}
	switch riskTier {
	case "low", "medium", "high", "critical":
	default:
		return nil, fmt.Errorf("invalid risk_tier: %s", riskTier)
	}

	var notes *string
	if in.Notes != nil {
		v := strings.TrimSpace(*in.Notes)
		if v != "" {
			notes = &v
		}
	}
	var updatedBy *string
	if in.UpdatedBy != nil {
		v := strings.TrimSpace(*in.UpdatedBy)
		if v != "" {
			updatedBy = &v
		}
	}

	row := &AIVendorGovernance{}
	err := s.DB.QueryRowContext(ctx, `
		INSERT INTO ai_vendor_governance (org_id, ai_vendor, sanctioned, risk_tier, notes, updated_by)
		VALUES ($1::uuid, $2, $3, $4, $5, $6)
		ON CONFLICT (org_id, ai_vendor)
		DO UPDATE SET sanctioned = EXCLUDED.sanctioned,
		              risk_tier = EXCLUDED.risk_tier,
		              notes = EXCLUDED.notes,
		              updated_by = EXCLUDED.updated_by,
		              updated_at = now()
		RETURNING id, org_id::text, ai_vendor, sanctioned, risk_tier, notes, updated_by, created_at, updated_at`,
		orgID, vendor, in.Sanctioned, riskTier, notes, updatedBy,
	).Scan(
		&row.ID, &row.OrgID, &row.AIVendor, &row.Sanctioned, &row.RiskTier,
		&row.Notes, &row.UpdatedBy, &row.CreatedAt, &row.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Store) EnsureUnsanctionedBlockRule(ctx context.Context, orgID, aiVendor, actor string) error {
	_ = actor
	vendor := strings.ToLower(strings.TrimSpace(aiVendor))
	if vendor == "" {
		return fmt.Errorf("ai_vendor is required")
	}

	ruleName := unsanctionedBlockRuleName(vendor)
	blockReason := fmt.Sprintf("Unsanctioned AI vendor blocked (%s)", vendor)
	rawConds, _ := json.Marshal([]PolicyCondition{{Field: "ai_vendor", Operator: "eq", Value: vendor}})

	var existingID string
	err := s.DB.QueryRowContext(ctx, `
		SELECT id
		FROM policy_rules
		WHERE org_id = $1::uuid AND name = $2
		LIMIT 1`, orgID, ruleName).Scan(&existingID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	if existingID == "" {
		_, err = s.DB.ExecContext(ctx, `
			INSERT INTO policy_rules (
				org_id, name, priority, enabled, action, conditions, block_reason, decision
			)
			SELECT
				$1::uuid,
				$2,
				COALESCE(GREATEST(MAX(priority) + 1, 9000), 9000),
				true,
				'block',
				$3::jsonb,
				$4,
				'block'
			FROM policy_rules
			WHERE org_id = $1::uuid`,
			orgID, ruleName, string(rawConds), blockReason,
		)
		return err
	}

	_, err = s.DB.ExecContext(ctx, `
		UPDATE policy_rules
		SET enabled = true,
		    action = 'block',
		    conditions = $1::jsonb,
		    block_reason = $2,
		    decision = 'block',
		    updated_at = now()
	WHERE id = $3`, string(rawConds), blockReason, existingID)
	return err
}

func (s *Store) DisableUnsanctionedBlockRule(ctx context.Context, orgID, aiVendor string) error {
	vendor := strings.ToLower(strings.TrimSpace(aiVendor))
	if vendor == "" {
		return fmt.Errorf("ai_vendor is required")
	}

	ruleName := unsanctionedBlockRuleName(vendor)
	_, err := s.DB.ExecContext(ctx, `
		UPDATE policy_rules
		SET enabled = false,
		    updated_at = now()
		WHERE org_id = $1::uuid
		  AND name = $2`,
		orgID, ruleName,
	)
	return err
}

func unsanctionedBlockRuleName(vendor string) string {
	return fmt.Sprintf("Auto: Block unsanctioned AI - %s", strings.ToLower(strings.TrimSpace(vendor)))
}
