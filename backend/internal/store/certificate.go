package store

import (
	"context"
	"database/sql"
	"time"
)

type Certificate struct {
	ID               string
	DeviceID         string
	OrgID            string
	Serial           string
	SubjectCN        string
	SubjectO         string
	IssuerCN         string
	KeyAlgorithm     string
	NotBefore        time.Time
	NotAfter         time.Time
	Status           string
	RevokedAt        *time.Time
	RevocationReason *string
	CSRPEM           string
	CertPEM          string
	CreatedAt        time.Time
}

type CertStatus struct {
	Serial   string
	Status   string
	DeviceID string
	OrgID    string
}

func (s *Store) CreateCertificate(ctx context.Context, tx *sql.Tx, cert *Certificate) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO certificates
		 (device_id, org_id, serial, subject_cn, subject_o, issuer_cn, key_algorithm,
		  not_before, not_after, status, csr_pem, cert_pem)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		cert.DeviceID, cert.OrgID, cert.Serial, cert.SubjectCN, cert.SubjectO,
		cert.IssuerCN, cert.KeyAlgorithm, cert.NotBefore, cert.NotAfter,
		cert.Status, cert.CSRPEM, cert.CertPEM)
	return err
}

func (s *Store) GetCertStatus(ctx context.Context, serial string) (*CertStatus, error) {
	cs := &CertStatus{}
	err := s.DB.QueryRowContext(ctx,
		`SELECT serial, status, device_id, org_id FROM certificates WHERE serial = $1`, serial,
	).Scan(&cs.Serial, &cs.Status, &cs.DeviceID, &cs.OrgID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return cs, nil
}

func (s *Store) HasActiveCert(ctx context.Context, deviceID string) (bool, error) {
	var count int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM certificates WHERE device_id = $1 AND status = 'active'`, deviceID,
	).Scan(&count)
	return count > 0, err
}

func (s *Store) RevokeCertificate(ctx context.Context, tx *sql.Tx, serial, reason string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE certificates SET status = 'revoked', revoked_at = now(), revocation_reason = $1
		 WHERE serial = $2 AND status = 'active'`,
		reason, serial)
	return err
}

// CertsExpiringSoonByOrg returns the count of active certs expiring within the given number of days, grouped by org.
func (s *Store) CertsExpiringSoonByOrg(ctx context.Context, withinDays int) (map[string]int, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT org_id, COUNT(*) FROM certificates
		 WHERE status = 'active' AND not_after < now() + ($1 || ' days')::interval AND not_after > now()
		 GROUP BY org_id`, withinDays)
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

func (s *Store) GetActiveCertForDevice(ctx context.Context, deviceID string) (*Certificate, error) {
	c := &Certificate{}
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, device_id, org_id, serial, subject_cn, subject_o, issuer_cn, key_algorithm,
		        not_before, not_after, status, csr_pem, cert_pem, created_at
		 FROM certificates WHERE device_id = $1 AND status = 'active'
		 ORDER BY created_at DESC LIMIT 1`, deviceID,
	).Scan(&c.ID, &c.DeviceID, &c.OrgID, &c.Serial, &c.SubjectCN, &c.SubjectO,
		&c.IssuerCN, &c.KeyAlgorithm, &c.NotBefore, &c.NotAfter, &c.Status,
		&c.CSRPEM, &c.CertPEM, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}
