package store

import (
	"context"
	"database/sql"
	"time"
)

func (s *Store) EnsureOperatorControlSchema(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `
		ALTER TABLE organizations
		    ADD COLUMN IF NOT EXISTS public_backend_url TEXT NOT NULL DEFAULT '',
		    ADD COLUMN IF NOT EXISTS public_gateway_url TEXT NOT NULL DEFAULT '',
		    ADD COLUMN IF NOT EXISTS status_reason TEXT NOT NULL DEFAULT '',
		    ADD COLUMN IF NOT EXISTS status_updated_at TIMESTAMPTZ,
		    ADD COLUMN IF NOT EXISTS status_updated_by TEXT NOT NULL DEFAULT '';

		CREATE INDEX IF NOT EXISTS idx_organizations_status
		    ON organizations(status);`)
	return err
}

type OperatorOrganization struct {
	Organization
	TotalDevices  int        `json:"total_devices"`
	ActiveDevices int        `json:"active_devices"`
	ActiveCerts   int        `json:"active_certs"`
	LastSeenAt    *time.Time `json:"last_seen_at,omitempty"`
}

func (s *Store) ListOperatorOrganizations(ctx context.Context) ([]OperatorOrganization, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT
			o.id, o.name, o.slug, o.api_key_hash, o.status,
			COALESCE(o.public_backend_url, ''), COALESCE(o.public_gateway_url, ''),
			COALESCE(o.status_reason, ''), o.status_updated_at,
			COALESCE(o.status_updated_by, ''), o.created_at, o.updated_at,
			COUNT(DISTINCT d.id),
			COUNT(DISTINCT d.id) FILTER (WHERE d.status = 'active'),
			COUNT(DISTINCT c.serial) FILTER (WHERE c.status = 'active'),
			MAX(t.timestamp)
		FROM organizations o
		LEFT JOIN devices d ON d.org_id = o.id
		LEFT JOIN certificates c ON c.org_id = o.id
		LEFT JOIN telemetry_events t ON t.org_id = o.id::text
		GROUP BY o.id
		ORDER BY o.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orgs := []OperatorOrganization{}
	for rows.Next() {
		var o OperatorOrganization
		if err := scanOperatorOrganization(rows, &o); err != nil {
			return nil, err
		}
		orgs = append(orgs, o)
	}
	return orgs, rows.Err()
}

func (s *Store) GetOperatorOrganization(ctx context.Context, id string) (*OperatorOrganization, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT
			o.id, o.name, o.slug, o.api_key_hash, o.status,
			COALESCE(o.public_backend_url, ''), COALESCE(o.public_gateway_url, ''),
			COALESCE(o.status_reason, ''), o.status_updated_at,
			COALESCE(o.status_updated_by, ''), o.created_at, o.updated_at,
			COUNT(DISTINCT d.id),
			COUNT(DISTINCT d.id) FILTER (WHERE d.status = 'active'),
			COUNT(DISTINCT c.serial) FILTER (WHERE c.status = 'active'),
			MAX(t.timestamp)
		FROM organizations o
		LEFT JOIN devices d ON d.org_id = o.id
		LEFT JOIN certificates c ON c.org_id = o.id
		LEFT JOIN telemetry_events t ON t.org_id = o.id::text
		WHERE o.id = $1
		GROUP BY o.id`, id)

	var o OperatorOrganization
	if err := scanOperatorOrganization(row, &o); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &o, nil
}

type operatorOrgScanner interface {
	Scan(dest ...interface{}) error
}

func scanOperatorOrganization(row operatorOrgScanner, o *OperatorOrganization) error {
	return row.Scan(
		&o.ID, &o.Name, &o.Slug, &o.APIKeyHash, &o.Status,
		&o.PublicBackendURL, &o.PublicGatewayURL, &o.StatusReason,
		&o.StatusUpdatedAt, &o.StatusUpdatedBy, &o.CreatedAt, &o.UpdatedAt,
		&o.TotalDevices, &o.ActiveDevices, &o.ActiveCerts, &o.LastSeenAt,
	)
}

func (s *Store) UpdateOrganizationProvisioning(ctx context.Context, id, backendURL, gatewayURL string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE organizations
		SET public_backend_url = $1,
		    public_gateway_url = $2,
		    updated_at = now()
		WHERE id = $3`, backendURL, gatewayURL, id)
	return err
}

func (s *Store) UpdateOrganizationStatus(ctx context.Context, tx *sql.Tx, id, status, reason, actor string) error {
	q := `
		UPDATE organizations
		SET status = $1,
		    status_reason = $2,
		    status_updated_at = now(),
		    status_updated_by = $3,
		    updated_at = now()
		WHERE id = $4`
	if tx != nil {
		_, err := tx.ExecContext(ctx, q, status, reason, actor, id)
		return err
	}
	_, err := s.DB.ExecContext(ctx, q, status, reason, actor, id)
	return err
}

func (s *Store) SetOrgDevicesStatus(ctx context.Context, tx *sql.Tx, orgID, status string) (int64, error) {
	q := `UPDATE devices SET status = $1, updated_at = now() WHERE org_id = $2`
	var (
		res sql.Result
		err error
	)
	if tx != nil {
		res, err = tx.ExecContext(ctx, q, status, orgID)
	} else {
		res, err = s.DB.ExecContext(ctx, q, status, orgID)
	}
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) RevokeActiveCertificatesForOrg(ctx context.Context, tx *sql.Tx, orgID, reason string) (int64, error) {
	q := `
		UPDATE certificates
		SET status = 'revoked',
		    revoked_at = now(),
		    revocation_reason = $1
		WHERE org_id = $2 AND status = 'active'`
	var (
		res sql.Result
		err error
	)
	if tx != nil {
		res, err = tx.ExecContext(ctx, q, reason, orgID)
	} else {
		res, err = s.DB.ExecContext(ctx, q, reason, orgID)
	}
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) ExpireEnrollmentForOrg(ctx context.Context, tx *sql.Tx, orgID string) (int64, int64, error) {
	tokenQuery := `
		UPDATE enrollment_tokens et
		SET expires_at = now()
		FROM devices d
		WHERE et.device_id = d.id
		  AND d.org_id = $1
		  AND et.used = false
		  AND et.expires_at > now()`
	shortCodeQuery := `
		UPDATE enrollment_short_codes
		SET expires_at = now()
		WHERE org_id = $1
		  AND expires_at > now()`

	exec := func(query string) (sql.Result, error) {
		if tx != nil {
			return tx.ExecContext(ctx, query, orgID)
		}
		return s.DB.ExecContext(ctx, query, orgID)
	}

	tokenRes, err := exec(tokenQuery)
	if err != nil {
		return 0, 0, err
	}
	shortCodeRes, err := exec(shortCodeQuery)
	if err != nil {
		return 0, 0, err
	}
	tokens, _ := tokenRes.RowsAffected()
	shortCodes, _ := shortCodeRes.RowsAffected()
	return tokens, shortCodes, nil
}
