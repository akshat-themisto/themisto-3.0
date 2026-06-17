package store

import (
	"context"
	"database/sql"
	"time"
)

type Organization struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Slug             string     `json:"slug"`
	APIKeyHash       string     `json:"-"`
	Status           string     `json:"status"`
	PublicBackendURL string     `json:"public_backend_url"`
	PublicGatewayURL string     `json:"public_gateway_url"`
	StatusReason     string     `json:"status_reason"`
	StatusUpdatedAt  *time.Time `json:"status_updated_at,omitempty"`
	StatusUpdatedBy  string     `json:"status_updated_by"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func (s *Store) GetOrganization(ctx context.Context, id string) (*Organization, error) {
	o := &Organization{}
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, name, slug, api_key_hash, status,
		        COALESCE(public_backend_url, ''), COALESCE(public_gateway_url, ''),
		        COALESCE(status_reason, ''), status_updated_at,
		        COALESCE(status_updated_by, ''), created_at, updated_at
		 FROM organizations WHERE id = $1`, id,
	).Scan(
		&o.ID, &o.Name, &o.Slug, &o.APIKeyHash, &o.Status,
		&o.PublicBackendURL, &o.PublicGatewayURL, &o.StatusReason,
		&o.StatusUpdatedAt, &o.StatusUpdatedBy, &o.CreatedAt, &o.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (s *Store) CreateOrganization(ctx context.Context, name, slug, apiKeyHash string) (*Organization, error) {
	o := &Organization{}
	err := s.DB.QueryRowContext(ctx,
		`INSERT INTO organizations (name, slug, api_key_hash) VALUES ($1, $2, $3)
		 RETURNING id, name, slug, api_key_hash, status,
		           COALESCE(public_backend_url, ''), COALESCE(public_gateway_url, ''),
		           COALESCE(status_reason, ''), status_updated_at,
		           COALESCE(status_updated_by, ''), created_at, updated_at`,
		name, slug, apiKeyHash,
	).Scan(
		&o.ID, &o.Name, &o.Slug, &o.APIKeyHash, &o.Status,
		&o.PublicBackendURL, &o.PublicGatewayURL, &o.StatusReason,
		&o.StatusUpdatedAt, &o.StatusUpdatedBy, &o.CreatedAt, &o.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (s *Store) GetOrganizationByAPIKeyHash(ctx context.Context) ([]Organization, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, name, slug, api_key_hash, status,
		        COALESCE(public_backend_url, ''), COALESCE(public_gateway_url, ''),
		        COALESCE(status_reason, ''), status_updated_at,
		        COALESCE(status_updated_by, ''), created_at, updated_at
		 FROM organizations WHERE status = 'active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []Organization
	for rows.Next() {
		var o Organization
		if err := rows.Scan(
			&o.ID, &o.Name, &o.Slug, &o.APIKeyHash, &o.Status,
			&o.PublicBackendURL, &o.PublicGatewayURL, &o.StatusReason,
			&o.StatusUpdatedAt, &o.StatusUpdatedBy, &o.CreatedAt, &o.UpdatedAt,
		); err != nil {
			return nil, err
		}
		orgs = append(orgs, o)
	}
	return orgs, rows.Err()
}
