package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/rizquuula/Constellate/internal/hub/app/enroll"
)

// CredentialStore implements enroll.CredentialStore against the SQLite machine_credentials table.
type CredentialStore struct {
	db *sql.DB
}

// NewCredentialStore returns a CredentialStore backed by db.
func NewCredentialStore(db *sql.DB) *CredentialStore {
	return &CredentialStore{db: db}
}

// Save upserts the public key for machineID.
func (s *CredentialStore) Save(ctx context.Context, machineID string, publicKey []byte, createdAt int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO machine_credentials (machine_id, public_key, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT(machine_id) DO UPDATE SET
			public_key = excluded.public_key,
			created_at = excluded.created_at
	`, machineID, publicKey, createdAt)
	if err != nil {
		return fmt.Errorf("sqlite: save credential %q: %w", machineID, err)
	}
	return nil
}

// PublicKey returns the stored public key for machineID.
// Returns enroll.ErrUnknownMachine if no credential exists.
func (s *CredentialStore) PublicKey(ctx context.Context, machineID string) ([]byte, error) {
	var key []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT public_key FROM machine_credentials WHERE machine_id = ?`, machineID,
	).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("sqlite: public key %q: %w", machineID, enroll.ErrUnknownMachine)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite: public key %q: %w", machineID, err)
	}
	return key, nil
}
