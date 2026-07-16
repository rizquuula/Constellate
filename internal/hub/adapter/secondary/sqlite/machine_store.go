package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
)

// MachineStore implements registry.MachineStore against the SQLite machines table.
type MachineStore struct {
	db *sql.DB
}

// NewMachineStore returns a MachineStore backed by db.
func NewMachineStore(db *sql.DB) *MachineStore {
	return &MachineStore{db: db}
}

// Upsert inserts the machine or updates non-identity fields on conflict.
// enrolled_at is intentionally excluded from the UPDATE so it is never clobbered.
func (s *MachineStore) Upsert(ctx context.Context, m machine.Machine) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO machines (id, instance_id, name, os, arch, agent_version, enrolled_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			instance_id   = excluded.instance_id,
			name          = excluded.name,
			os            = excluded.os,
			arch          = excluded.arch,
			agent_version = excluded.agent_version,
			last_seen_at  = excluded.last_seen_at
	`,
		m.ID(), m.InstanceID(), m.Name(), m.OS(), m.Arch(), m.AgentVersion(), m.EnrolledAt(), m.LastSeenAt(),
	)
	if err != nil {
		return fmt.Errorf("sqlite: upsert machine %q: %w", m.ID(), err)
	}
	return nil
}

// UpdateLastSeen bumps the last_seen_at timestamp for the given machine id.
func (s *MachineStore) UpdateLastSeen(ctx context.Context, id string, ts int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE machines SET last_seen_at = ? WHERE id = ?`, ts, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite: update last_seen_at %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: rows affected %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("sqlite: update last_seen_at %q: %w", id, machine.ErrNotFound)
	}
	return nil
}

// List returns all machine records.
func (s *MachineStore) List(ctx context.Context) ([]machine.Machine, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, instance_id, name, os, arch, agent_version, enrolled_at, last_seen_at, revoked_at
		FROM machines
	`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list machines: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []machine.Machine
	for rows.Next() {
		m, err := scanMachine(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan machine row: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list machines rows: %w", err)
	}
	return out, nil
}

// ByID returns a single machine by its id.
// Returns machine.ErrNotFound (wrapped) if no row matches.
func (s *MachineStore) ByID(ctx context.Context, id string) (machine.Machine, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, name, os, arch, agent_version, enrolled_at, last_seen_at, revoked_at
		FROM machines WHERE id = ?
	`, id)
	m, err := scanMachine(row)
	if errors.Is(err, sql.ErrNoRows) {
		return machine.Machine{}, fmt.Errorf("sqlite: by id %q: %w", id, machine.ErrNotFound)
	}
	if err != nil {
		return machine.Machine{}, fmt.Errorf("sqlite: by id %q: %w", id, err)
	}
	return m, nil
}

// MarkRevoked sets revoked_at for the given machine id.
// Returns machine.ErrNotFound (wrapped) if no row matches.
func (s *MachineStore) MarkRevoked(ctx context.Context, id string, ts int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE machines SET revoked_at = ? WHERE id = ?`, ts, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite: mark revoked %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: rows affected %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("sqlite: mark revoked %q: %w", id, machine.ErrNotFound)
	}
	return nil
}

// ClearRevoked resets revoked_at to NULL for the given machine id.
// Returns machine.ErrNotFound (wrapped) if no row matches.
func (s *MachineStore) ClearRevoked(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE machines SET revoked_at = NULL WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite: clear revoked %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: rows affected %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("sqlite: clear revoked %q: %w", id, machine.ErrNotFound)
	}
	return nil
}

// Delete removes the machine and every row that references it. Because FK
// enforcement is ON (see db.go), children must go before their parent, so this
// method intentionally spans sibling tables (sessions, projects,
// machine_credentials) in a single transaction to guarantee an atomic cascade —
// either the machine and all of its dependents disappear together or nothing
// does. Returns machine.ErrNotFound (wrapped) if no machine row matches.
func (s *MachineStore) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: delete machine %q: begin tx: %w", id, err)
	}
	// No-op once the tx is committed; rolls back on any early return otherwise.
	defer func() { _ = tx.Rollback() }()

	children := []string{
		`DELETE FROM sessions            WHERE machine_id = ?`,
		`DELETE FROM projects            WHERE machine_id = ?`,
		`DELETE FROM machine_credentials WHERE machine_id = ?`,
	}
	for _, q := range children {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return fmt.Errorf("sqlite: delete machine %q children: %w", id, err)
		}
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM machines WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete machine %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: rows affected %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("sqlite: delete machine %q: %w", id, machine.ErrNotFound)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: delete machine %q: commit: %w", id, err)
	}
	return nil
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanMachine(s scanner) (machine.Machine, error) {
	var (
		id, name, osName   string
		instanceID         sql.NullString
		arch, agentVersion sql.NullString
		enrolledAt         int64
		lastSeenAt         sql.NullInt64
		revokedAt          sql.NullInt64
	)
	if err := s.Scan(&id, &instanceID, &name, &osName, &arch, &agentVersion, &enrolledAt, &lastSeenAt, &revokedAt); err != nil {
		return machine.Machine{}, err
	}
	ls := enrolledAt
	if lastSeenAt.Valid {
		ls = lastSeenAt.Int64
	}
	m := machine.Rehydrate(
		id,
		instanceID.String,
		name,
		osName,
		arch.String,
		agentVersion.String,
		enrolledAt, ls,
	)
	if revokedAt.Valid {
		m = m.MarkRevoked(revokedAt.Int64)
	}
	return m, nil
}
