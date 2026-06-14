package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
)

// AuditStore implements app/audit.AuditStore against the SQLite audit_log table.
type AuditStore struct {
	db *sql.DB
}

// NewAuditStore returns an AuditStore backed by db.
func NewAuditStore(db *sql.DB) *AuditStore {
	return &AuditStore{db: db}
}

// Append inserts a new audit event. Empty machineID/sessionID/detail are stored as NULL.
func (s *AuditStore) Append(ctx context.Context, e audit.Event) error {
	machineID := nullString(e.MachineID())
	sessionID := nullString(e.SessionID())
	detail := nullString(e.Detail())

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (ts, actor, action, machine_id, session_id, detail)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		e.TS(), e.Actor(), string(e.Action()), machineID, sessionID, detail,
	)
	if err != nil {
		return fmt.Errorf("sqlite: append audit: %w", err)
	}
	return nil
}

// List returns the most recent limit events, ordered newest-first.
func (s *AuditStore) List(ctx context.Context, limit int) ([]audit.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT ts, actor, action, machine_id, session_id, detail
		 FROM audit_log
		 ORDER BY ts DESC, id DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list audit: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []audit.Event
	for rows.Next() {
		e, err := scanAuditEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan audit row: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list audit rows: %w", err)
	}
	return out, nil
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func scanAuditEvent(s scanner) (audit.Event, error) {
	var (
		ts              int64
		actor, action   string
		machineID       sql.NullString
		sessionID       sql.NullString
		detail          sql.NullString
	)
	if err := s.Scan(&ts, &actor, &action, &machineID, &sessionID, &detail); err != nil {
		return audit.Event{}, err
	}
	return audit.NewEvent(ts, actor, audit.Action(action), machineID.String, sessionID.String, detail.String), nil
}
