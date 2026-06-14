package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// OperatorStore implements auth.OperatorStore against the SQLite operator_credentials table.
type OperatorStore struct {
	db    *sql.DB
	newID func() string
}

// NewOperatorStore returns an OperatorStore backed by db.
func NewOperatorStore(db *sql.DB, newID func() string) *OperatorStore {
	return &OperatorStore{db: db, newID: newID}
}

// HasOperator returns true if any operator credential (totp) row exists.
func (s *OperatorStore) HasOperator(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM operator_credentials WHERE kind = 'totp'`,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("sqlite: has operator: %w", err)
	}
	return count > 0, nil
}

// TOTPSecret returns the stored TOTP secret.
func (s *OperatorStore) TOTPSecret(ctx context.Context) (string, bool, error) {
	var secret string
	err := s.db.QueryRowContext(ctx,
		`SELECT data FROM operator_credentials WHERE kind = 'totp' LIMIT 1`,
	).Scan(&secret)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("sqlite: totp secret: %w", err)
	}
	return secret, true, nil
}

// SaveTOTP inserts the TOTP secret row.
func (s *OperatorStore) SaveTOTP(ctx context.Context, secret string, createdAt int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO operator_credentials (id, kind, data, created_at) VALUES (?, 'totp', ?, ?)`,
		s.newID(), secret, createdAt,
	)
	if err != nil {
		return fmt.Errorf("sqlite: save totp: %w", err)
	}
	return nil
}

// SaveRecoveryCodes inserts hashed recovery codes. Each hash is used as the row id.
func (s *OperatorStore) SaveRecoveryCodes(ctx context.Context, hashes []string, createdAt int64) error {
	for _, h := range hashes {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO operator_credentials (id, kind, data, created_at) VALUES (?, 'recovery', ?, ?)`,
			h, h, createdAt,
		)
		if err != nil {
			return fmt.Errorf("sqlite: save recovery code: %w", err)
		}
	}
	return nil
}

// ConsumeRecoveryCode deletes the recovery code row matching hash; returns true if found.
func (s *OperatorStore) ConsumeRecoveryCode(ctx context.Context, hash string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM operator_credentials WHERE kind = 'recovery' AND data = ?`, hash,
	)
	if err != nil {
		return false, fmt.Errorf("sqlite: consume recovery code: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("sqlite: consume recovery code rows affected: %w", err)
	}
	return n > 0, nil
}
