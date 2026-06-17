package store

import (
	"context"
	"database/sql"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type AdminUser struct {
	ID                 string     `json:"id"`
	OrgID              string     `json:"org_id"`
	Email              string     `json:"email"`
	Name               string     `json:"name"`
	PasswordHash       string     `json:"-"`
	Role               string     `json:"role"`
	MustChangePassword bool       `json:"must_change_password"`
	TermsAcceptedAt    *time.Time `json:"terms_accepted_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type adminUserScanner interface {
	Scan(dest ...any) error
}

func scanAdminUser(scanner adminUserScanner, u *AdminUser) error {
	var termsAcceptedAt sql.NullTime
	if err := scanner.Scan(
		&u.ID,
		&u.OrgID,
		&u.Email,
		&u.Name,
		&u.PasswordHash,
		&u.Role,
		&u.MustChangePassword,
		&termsAcceptedAt,
		&u.CreatedAt,
		&u.UpdatedAt,
	); err != nil {
		return err
	}
	if termsAcceptedAt.Valid {
		acceptedAt := termsAcceptedAt.Time
		u.TermsAcceptedAt = &acceptedAt
	}
	return nil
}

func (s *Store) CreateUser(ctx context.Context, orgID, email, name, password, role string) (*AdminUser, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	u := &AdminUser{}
	row := s.DB.QueryRowContext(ctx,
		`INSERT INTO admin_users (org_id, email, name, password_hash, role, must_change_password)
		 VALUES ($1, $2, $3, $4, $5, true)
		 RETURNING id, org_id, email, name, password_hash, role, must_change_password, terms_accepted_at, created_at, updated_at`,
		orgID, email, name, string(hash), role,
	)
	err = scanAdminUser(row, u)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*AdminUser, error) {
	u := &AdminUser{}
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, org_id, email, name, password_hash, role, must_change_password, terms_accepted_at, created_at, updated_at
		 FROM admin_users WHERE email = $1`, email,
	)
	err := scanAdminUser(row, u)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) GetUserByID(ctx context.Context, id string) (*AdminUser, error) {
	u := &AdminUser{}
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, org_id, email, name, password_hash, role, must_change_password, terms_accepted_at, created_at, updated_at
		 FROM admin_users WHERE id = $1`, id,
	)
	err := scanAdminUser(row, u)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) ListUsers(ctx context.Context, orgID string) ([]AdminUser, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, org_id, email, name, password_hash, role, must_change_password, terms_accepted_at, created_at, updated_at
		 FROM admin_users WHERE org_id = $1 ORDER BY created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []AdminUser
	for rows.Next() {
		var u AdminUser
		if err := scanAdminUser(rows, &u); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM admin_users WHERE id = $1`, id)
	return err
}

func (s *Store) UpdateUserPassword(ctx context.Context, id, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx,
		`UPDATE admin_users SET password_hash = $1, must_change_password = false, updated_at = now() WHERE id = $2`,
		string(hash), id)
	return err
}

func (s *Store) AcceptUserTerms(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE admin_users
		 SET terms_accepted_at = COALESCE(terms_accepted_at, now()), updated_at = now()
		 WHERE id = $1`, id)
	return err
}

func (s *Store) RotateUserPasswordAndSessions(ctx context.Context, id, newPassword, newTokenHash string, expiresAt time.Time) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`UPDATE admin_users SET password_hash = $1, must_change_password = false, updated_at = now() WHERE id = $2`,
		string(hash), id); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, id); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		id, newTokenHash, expiresAt); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) CheckPassword(user *AdminUser, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	return err == nil
}
