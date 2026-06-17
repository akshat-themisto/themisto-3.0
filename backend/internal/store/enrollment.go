package store

import (
	"context"
	"database/sql"
	"time"
)

type EnrollmentToken struct {
	ID        string
	DeviceID  string
	TokenHash string
	Used      bool
	ExpiresAt time.Time
	CreatedAt time.Time
}

func (s *Store) CreateEnrollmentToken(ctx context.Context, deviceID, tokenHash string, expiresAt time.Time) (*EnrollmentToken, error) {
	t := &EnrollmentToken{}
	err := s.DB.QueryRowContext(ctx,
		`INSERT INTO enrollment_tokens (device_id, token_hash, expires_at)
		 VALUES ($1, $2, $3)
		 RETURNING id, device_id, token_hash, used, expires_at, created_at`,
		deviceID, tokenHash, expiresAt,
	).Scan(&t.ID, &t.DeviceID, &t.TokenHash, &t.Used, &t.ExpiresAt, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// --- Enrollment short codes ---

type EnrollmentShortCode struct {
	Code       string
	DeviceID   string
	OrgID      string
	ConfigJSON []byte
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

func (s *Store) CreateEnrollmentShortCode(ctx context.Context, code, deviceID, orgID string, configJSON []byte, expiresAt time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO enrollment_short_codes (code, device_id, org_id, config_json, expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		code, deviceID, orgID, configJSON, expiresAt,
	)
	return err
}

func (s *Store) GetEnrollmentShortCode(ctx context.Context, code string) (*EnrollmentShortCode, error) {
	sc := &EnrollmentShortCode{}
	err := s.DB.QueryRowContext(ctx,
		`SELECT code, device_id, org_id, config_json, expires_at, created_at
		 FROM enrollment_short_codes
		 WHERE code = $1 AND expires_at > now()`,
		code,
	).Scan(&sc.Code, &sc.DeviceID, &sc.OrgID, &sc.ConfigJSON, &sc.ExpiresAt, &sc.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return sc, nil
}

func (s *Store) ValidateAndConsumeToken(ctx context.Context, tx *sql.Tx, tokenHash string) (*EnrollmentToken, error) {
	t := &EnrollmentToken{}
	err := tx.QueryRowContext(ctx,
		`UPDATE enrollment_tokens
		 SET used = true
		 WHERE token_hash = $1 AND used = false AND expires_at > now()
		 RETURNING id, device_id, token_hash, used, expires_at, created_at`,
		tokenHash,
	).Scan(&t.ID, &t.DeviceID, &t.TokenHash, &t.Used, &t.ExpiresAt, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}
