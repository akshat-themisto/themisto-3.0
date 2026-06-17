package store

import (
	"context"
	"database/sql"
	"time"
)

type Device struct {
	ID           string
	OrgID        string
	DeviceName   string
	OS           string
	AgentVersion string
	Status       string
	EnrolledAt   *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type DeviceWithCert struct {
	Device
	CertSerial    *string
	CertExpiresAt *time.Time
}

func (s *Store) CreateDevice(ctx context.Context, orgID, name, osType, agentVersion string) (*Device, error) {
	d := &Device{}
	err := s.DB.QueryRowContext(ctx,
		`INSERT INTO devices (org_id, device_name, os, agent_version)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, org_id, device_name, os, agent_version, status, enrolled_at, created_at, updated_at`,
		orgID, name, osType, agentVersion,
	).Scan(&d.ID, &d.OrgID, &d.DeviceName, &d.OS, &d.AgentVersion, &d.Status, &d.EnrolledAt, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Store) GetDevice(ctx context.Context, id string) (*Device, error) {
	d := &Device{}
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, org_id, device_name, os, agent_version, status, enrolled_at, created_at, updated_at
		 FROM devices WHERE id = $1`, id,
	).Scan(&d.ID, &d.OrgID, &d.DeviceName, &d.OS, &d.AgentVersion, &d.Status, &d.EnrolledAt, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Store) ActivateDevice(ctx context.Context, tx *sql.Tx, id string) error {
	now := time.Now()
	_, err := tx.ExecContext(ctx,
		`UPDATE devices SET status = 'active', enrolled_at = $1, updated_at = $2 WHERE id = $3`,
		now, now, id)
	return err
}

func (s *Store) SetRenewalTokenHash(ctx context.Context, tx *sql.Tx, deviceID, tokenHash string) error {
	q := `UPDATE devices SET renewal_token_hash = $1, updated_at = now() WHERE id = $2`
	if tx != nil {
		_, err := tx.ExecContext(ctx, q, tokenHash, deviceID)
		return err
	}
	_, err := s.DB.ExecContext(ctx, q, tokenHash, deviceID)
	return err
}

func (s *Store) GetRenewalTokenHash(ctx context.Context, deviceID string) (string, error) {
	var hash sql.NullString
	err := s.DB.QueryRowContext(ctx,
		`SELECT renewal_token_hash FROM devices WHERE id = $1`, deviceID,
	).Scan(&hash)
	if err != nil {
		return "", err
	}
	return hash.String, nil
}

func (s *Store) SupersedeActiveCerts(ctx context.Context, tx *sql.Tx, deviceID string) error {
	q := `UPDATE certificates SET status = 'superseded' WHERE device_id = $1 AND status = 'active'`
	if tx != nil {
		_, err := tx.ExecContext(ctx, q, deviceID)
		return err
	}
	_, err := s.DB.ExecContext(ctx, q, deviceID)
	return err
}

func (s *Store) ListDevices(ctx context.Context, orgID, status string) ([]DeviceWithCert, error) {
	query := `SELECT d.id, d.org_id, d.device_name, d.os, d.agent_version, d.status,
	                 d.enrolled_at, d.created_at, d.updated_at,
	                 c.serial, c.not_after
	          FROM devices d
	          LEFT JOIN certificates c ON c.device_id = d.id AND c.status = 'active'
	          WHERE d.org_id = $1`
	args := []interface{}{orgID}

	if status != "" {
		query += ` AND d.status = $2`
		args = append(args, status)
	}
	query += ` ORDER BY d.created_at DESC`

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []DeviceWithCert
	for rows.Next() {
		var d DeviceWithCert
		if err := rows.Scan(&d.ID, &d.OrgID, &d.DeviceName, &d.OS, &d.AgentVersion,
			&d.Status, &d.EnrolledAt, &d.CreatedAt, &d.UpdatedAt,
			&d.CertSerial, &d.CertExpiresAt); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

// ActiveDevicesByOrg returns the count of active (enrolled) devices grouped by org.
func (s *Store) ActiveDevicesByOrg(ctx context.Context) (map[string]int, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT org_id, COUNT(*) FROM devices WHERE status = 'active' GROUP BY org_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int)
	for rows.Next() {
		var orgID string
		var count int
		if err := rows.Scan(&orgID, &count); err != nil {
			return nil, err
		}
		result[orgID] = count
	}
	return result, rows.Err()
}
