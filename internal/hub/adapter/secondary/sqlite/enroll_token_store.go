package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/rizquuula/Constellate/internal/hub/app/enroll"
)

// EnrollTokenStore implements enroll.EnrollTokenStore against the SQLite enroll_tokens table.
type EnrollTokenStore struct {
	db *sql.DB
}

// NewEnrollTokenStore returns an EnrollTokenStore backed by db.
func NewEnrollTokenStore(db *sql.DB) *EnrollTokenStore {
	return &EnrollTokenStore{db: db}
}

// Create inserts a new enrollment token hash with its expiry.
func (s *EnrollTokenStore) Create(ctx context.Context, tokenHash string, expiresAt int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO enroll_tokens (token_hash, expires_at) VALUES (?, ?)`,
		tokenHash, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("sqlite: create enroll token: %w", err)
	}
	return nil
}

// Consume atomically marks the token as used. Returns enroll.ErrInvalidToken if
// the token is missing, expired, or already consumed.
func (s *EnrollTokenStore) Consume(ctx context.Context, tokenHash string, now int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE enroll_tokens SET used_at = ? WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
		now, tokenHash, now,
	)
	if err != nil {
		return fmt.Errorf("sqlite: consume enroll token: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: consume enroll token rows: %w", err)
	}
	if n == 0 {
		return enroll.ErrInvalidToken
	}
	return nil
}
