package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

// SessionStore implements sessions.SessionStore and attach.SessionStore against
// the SQLite sessions table.
type SessionStore struct {
	db *sql.DB
}

// NewSessionStore returns a SessionStore backed by db.
func NewSessionStore(db *sql.DB) *SessionStore {
	return &SessionStore{db: db}
}

// Create inserts a new session record.
func (s *SessionStore) Create(ctx context.Context, ss session.Session) error {
	var projectID sql.NullString
	if ss.ProjectID() != "" {
		projectID = sql.NullString{String: ss.ProjectID(), Valid: true}
	}
	var title sql.NullString
	if ss.Title() != "" {
		title = sql.NullString{String: ss.Title(), Valid: true}
	}
	var shell sql.NullString
	if ss.Shell() != "" {
		shell = sql.NullString{String: ss.Shell(), Valid: true}
	}
	var cwd sql.NullString
	if ss.Cwd() != "" {
		cwd = sql.NullString{String: ss.Cwd(), Valid: true}
	}
	autoRelaunch := 0
	if ss.AutoRelaunch() {
		autoRelaunch = 1
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, project_id, machine_id, title, shell, cwd, auto_relaunch, status, exit_code, created_at, last_active_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)
	`,
		ss.ID(), projectID, ss.MachineID(), title, shell, cwd, autoRelaunch, string(ss.Status()),
		ss.CreatedAt(), ss.LastActiveAt(),
	)
	if err != nil {
		return fmt.Errorf("sqlite: create session %q: %w", ss.ID(), err)
	}
	return nil
}

// ByID returns a single session by its id.
// Returns session.ErrNotFound (wrapped) if no row matches.
func (s *SessionStore) ByID(ctx context.Context, id string) (session.Session, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, project_id, machine_id, title, shell, cwd, auto_relaunch, status, exit_code, created_at, last_active_at, activity, pwd
		FROM sessions WHERE id = ?
	`, id)
	ss, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return session.Session{}, fmt.Errorf("sqlite: by id %q: %w", id, session.ErrNotFound)
	}
	if err != nil {
		return session.Session{}, fmt.Errorf("sqlite: by id %q: %w", id, err)
	}
	return ss, nil
}

// List returns all session records.
func (s *SessionStore) List(ctx context.Context) ([]session.Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, machine_id, title, shell, cwd, auto_relaunch, status, exit_code, created_at, last_active_at, activity, pwd
		FROM sessions
	`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return collectSessions(rows)
}

// ListByMachine returns all sessions for the given machine.
func (s *SessionStore) ListByMachine(ctx context.Context, machineID string) ([]session.Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, machine_id, title, shell, cwd, auto_relaunch, status, exit_code, created_at, last_active_at, activity, pwd
		FROM sessions WHERE machine_id = ?
	`, machineID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list sessions by machine %q: %w", machineID, err)
	}
	defer func() { _ = rows.Close() }()

	return collectSessions(rows)
}

// AutoRelaunchSessions returns running or disconnected sessions for the given
// machine that have auto_relaunch=1. Used during restart reconciliation; the
// sessions are already disconnected by the time a restart is detected on
// reconnect, so both statuses must match.
func (s *SessionStore) AutoRelaunchSessions(ctx context.Context, machineID string) ([]session.Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, machine_id, title, shell, cwd, auto_relaunch, status, exit_code, created_at, last_active_at, activity, pwd
		FROM sessions WHERE machine_id = ? AND status IN ('running', 'disconnected') AND auto_relaunch = 1
	`, machineID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: auto-relaunch sessions for machine %q: %w", machineID, err)
	}
	defer func() { _ = rows.Close() }()

	return collectSessions(rows)
}

// MarkRunningLost bulk-marks all running or disconnected sessions (with
// auto_relaunch=0) for a machine as lost. Sessions with auto_relaunch=1 are
// handled separately by the relaunch path. Disconnected sessions match too:
// by the time a restart is detected they were already marked disconnected on
// the connection drop.
func (s *SessionStore) MarkRunningLost(ctx context.Context, machineID string, ts int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET status = ?, last_active_at = ?
		WHERE machine_id = ? AND status IN ('running', 'disconnected') AND auto_relaunch = 0
	`, string(session.StatusLost), ts, machineID)
	if err != nil {
		return fmt.Errorf("sqlite: mark running lost for machine %q: %w", machineID, err)
	}
	return nil
}

// MarkRunningDisconnected bulk-marks all running sessions for a machine as
// disconnected (all auto_relaunch values). Called on the connection-teardown
// path: the machine is unreachable but the PTYs are presumed alive.
func (s *SessionStore) MarkRunningDisconnected(ctx context.Context, machineID string, ts int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET status = ?, last_active_at = ?
		WHERE machine_id = ? AND status = ?
	`, string(session.StatusDisconnected), ts, machineID, string(session.StatusRunning))
	if err != nil {
		return fmt.Errorf("sqlite: mark running disconnected for machine %q: %w", machineID, err)
	}
	return nil
}

// MarkDisconnectedRunning bulk-restores all disconnected sessions for a machine
// to running. Called on a same-instance reconnect: the blip is over and the
// PTYs survived it.
func (s *SessionStore) MarkDisconnectedRunning(ctx context.Context, machineID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET status = ?
		WHERE machine_id = ? AND status = ?
	`, string(session.StatusRunning), machineID, string(session.StatusDisconnected))
	if err != nil {
		return fmt.Errorf("sqlite: mark disconnected running for machine %q: %w", machineID, err)
	}
	return nil
}

// SetRunning sets a single session's status back to running (used after a successful relaunch).
func (s *SessionStore) SetRunning(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET status = ? WHERE id = ?
	`, string(session.StatusRunning), id)
	if err != nil {
		return fmt.Errorf("sqlite: set running %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: rows affected %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("sqlite: set running %q: %w", id, session.ErrNotFound)
	}
	return nil
}

// SetLost marks a single session as lost (used for failed relaunch attempts).
func (s *SessionStore) SetLost(ctx context.Context, id string, ts int64) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET status = ?, last_active_at = ? WHERE id = ?
	`, string(session.StatusLost), ts, id)
	if err != nil {
		return fmt.Errorf("sqlite: set lost %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: rows affected %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("sqlite: set lost %q: %w", id, session.ErrNotFound)
	}
	return nil
}

// SetExited updates the session's status, exit_code, and last_active_at.
func (s *SessionStore) SetExited(ctx context.Context, id string, exitCode int, ts int64) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET status = ?, exit_code = ?, last_active_at = ? WHERE id = ?
	`, string(session.StatusExited), exitCode, ts, id)
	if err != nil {
		return fmt.Errorf("sqlite: set exited %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: rows affected %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("sqlite: set exited %q: %w", id, session.ErrNotFound)
	}
	return nil
}

// SetAutoRelaunch updates the auto_relaunch flag for a session.
// Returns session.ErrNotFound (wrapped) if no row matches.
func (s *SessionStore) SetAutoRelaunch(ctx context.Context, id string, v bool) error {
	val := 0
	if v {
		val = 1
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET auto_relaunch = ? WHERE id = ?
	`, val, id)
	if err != nil {
		return fmt.Errorf("sqlite: set auto_relaunch %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: rows affected %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("sqlite: set auto_relaunch %q: %w", id, session.ErrNotFound)
	}
	return nil
}

// SetTitle updates the session's title (metadata only; last_active_at is not touched).
// An empty title is stored as NULL. Returns session.ErrNotFound on 0 rows affected.
func (s *SessionStore) SetTitle(ctx context.Context, id, title string) error {
	var t sql.NullString
	if title != "" {
		t = sql.NullString{String: title, Valid: true}
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET title = ? WHERE id = ?
	`, t, id)
	if err != nil {
		return fmt.Errorf("sqlite: set title %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: rows affected %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("sqlite: set title %q: %w", id, session.ErrNotFound)
	}
	return nil
}

// SetStat updates the session's activity and/or live working directory (pwd)
// columns. When lastActiveAt > 0, last_active_at is also updated (only when the
// session is "active"). An empty activity or pwd leaves that column untouched
// (preserve-on-empty via COALESCE(NULLIF(?, ''), col)).
// Returns session.ErrNotFound (wrapped) if 0 rows are affected.
func (s *SessionStore) SetStat(ctx context.Context, id, activity, pwd string, lastActiveAt int64) error {
	var (
		res sql.Result
		err error
	)
	if lastActiveAt > 0 {
		res, err = s.db.ExecContext(ctx, `
			UPDATE sessions
			   SET activity = COALESCE(NULLIF(?, ''), activity),
			       pwd      = COALESCE(NULLIF(?, ''), pwd),
			       last_active_at = ?
			 WHERE id = ?
		`, activity, pwd, lastActiveAt, id)
	} else {
		res, err = s.db.ExecContext(ctx, `
			UPDATE sessions
			   SET activity = COALESCE(NULLIF(?, ''), activity),
			       pwd      = COALESCE(NULLIF(?, ''), pwd)
			 WHERE id = ?
		`, activity, pwd, id)
	}
	if err != nil {
		return fmt.Errorf("sqlite: set stat %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: rows affected %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("sqlite: set stat %q: %w", id, session.ErrNotFound)
	}
	return nil
}

// Delete permanently removes a session record. Returns session.ErrNotFound
// (wrapped) if no row matches.
func (s *SessionStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: delete session %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: rows affected %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("sqlite: delete session %q: %w", id, session.ErrNotFound)
	}
	return nil
}

// CountByProject returns the number of sessions that reference the given
// project ID (regardless of status).
func (s *SessionStore) CountByProject(ctx context.Context, projectID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sessions WHERE project_id = ?
	`, projectID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("sqlite: count sessions by project %q: %w", projectID, err)
	}
	return n, nil
}

func collectSessions(rows *sql.Rows) ([]session.Session, error) {
	var out []session.Session
	for rows.Next() {
		ss, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scan session row: %w", err)
		}
		out = append(out, ss)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: sessions rows: %w", err)
	}
	return out, nil
}

func scanSession(s scanner) (session.Session, error) {
	var (
		id, machineID string
		statusStr     string
		projectID     sql.NullString
		title         sql.NullString
		shell         sql.NullString
		cwd           sql.NullString
		autoRelaunch  int
		exitCode      sql.NullInt64
		lastActiveAt  sql.NullInt64
		createdAt     int64
		activity      sql.NullString
		pwd           sql.NullString
	)
	if err := s.Scan(&id, &projectID, &machineID, &title, &shell, &cwd, &autoRelaunch, &statusStr, &exitCode, &createdAt, &lastActiveAt, &activity, &pwd); err != nil {
		return session.Session{}, err
	}
	var code int
	if exitCode.Valid {
		code = int(exitCode.Int64)
	}
	lat := createdAt
	if lastActiveAt.Valid {
		lat = lastActiveAt.Int64
	}
	ss := session.Rehydrate(
		id,
		projectID.String,
		machineID,
		title.String,
		shell.String,
		cwd.String,
		session.Status(statusStr),
		code,
		autoRelaunch != 0,
		createdAt,
		lat,
	)
	if activity.Valid && activity.String != "" {
		ss.SetActivity(activity.String)
	}
	if pwd.Valid && pwd.String != "" {
		ss.SetPwd(pwd.String)
	}
	return ss, nil
}
