package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// OperatorSessionStore implements auth.SessionStore against the SQLite operator_sessions table.
type OperatorSessionStore struct {
	db *sql.DB
}

// NewOperatorSessionStore returns an OperatorSessionStore backed by db.
func NewOperatorSessionStore(db *sql.DB) *OperatorSessionStore {
	return &OperatorSessionStore{db: db}
}

// Create inserts a new operator session.
func (s *OperatorSessionStore) Create(ctx context.Context, id string, createdAt, expiresAt int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO operator_sessions (id, created_at, expires_at) VALUES (?, ?, ?)`,
		id, createdAt, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("sqlite: create operator session: %w", err)
	}
	return nil
}

// Validate returns true if the session exists and has not expired. Updates last_seen_at on success.
func (s *OperatorSessionStore) Validate(ctx context.Context, id string, now int64) (bool, error) {
	var expiresAt int64
	err := s.db.QueryRowContext(ctx,
		`SELECT expires_at FROM operator_sessions WHERE id = ?`, id,
	).Scan(&expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("sqlite: validate operator session: %w", err)
	}
	if expiresAt <= now {
		return false, nil
	}
	_, _ = s.db.ExecContext(ctx,
		`UPDATE operator_sessions SET last_seen_at = ? WHERE id = ?`, now, id,
	)
	return true, nil
}

// Delete removes the operator session.
func (s *OperatorSessionStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM operator_sessions WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite: delete operator session: %w", err)
	}
	return nil
}
