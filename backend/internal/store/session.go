package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"
)

type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	TokenHash string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

func GenerateSessionToken() (raw string, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = hex.EncodeToString(b)
	h := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(h[:])
	return raw, hash, nil
}

func HashSessionToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func (s *Store) CreateSession(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (*Session, error) {
	sess := &Session{}
	err := s.DB.QueryRowContext(ctx,
		`INSERT INTO sessions (user_id, token_hash, expires_at)
		 VALUES ($1, $2, $3)
		 RETURNING id, user_id, token_hash, expires_at, created_at`,
		userID, tokenHash, expiresAt,
	).Scan(&sess.ID, &sess.UserID, &sess.TokenHash, &sess.ExpiresAt, &sess.CreatedAt)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) GetSessionByToken(ctx context.Context, tokenHash string) (*Session, error) {
	sess := &Session{}
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, user_id, token_hash, expires_at, created_at
		 FROM sessions WHERE token_hash = $1 AND expires_at > now()`,
		tokenHash,
	).Scan(&sess.ID, &sess.UserID, &sess.TokenHash, &sess.ExpiresAt, &sess.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}

func (s *Store) CleanExpiredSessions(ctx context.Context) (int64, error) {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
