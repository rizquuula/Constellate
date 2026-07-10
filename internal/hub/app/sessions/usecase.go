package sessions

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

// SystemClock implements Clock using the real wall clock.
type SystemClock struct{}

func (SystemClock) Now() int64 { return time.Now().Unix() }

// OpenInput carries the data needed to open a new session.
type OpenInput struct {
	MachineID string
	ProjectID string
	Title     string
	Cwd       string
	Shell     string
	Cols      int
	Rows      int
	// CreateDir asks the agent to create Cwd (recursively) if missing instead of
	// rejecting the open with cwd_not_found.
	CreateDir bool
}

// UseCase orchestrates session lifecycle.
type UseCase struct {
	store   SessionStore
	gateway AgentGateway
	audit   AuditSink
	clock   Clock
	newID   func() string
	log     *slog.Logger
}

// New constructs a UseCase with the provided adapters.
func New(store SessionStore, gateway AgentGateway, clock Clock, newID func() string, log *slog.Logger, auditSink AuditSink) *UseCase {
	return &UseCase{
		store:   store,
		gateway: gateway,
		audit:   auditSink,
		clock:   clock,
		newID:   newID,
		log:     log,
	}
}

// Open opens a new PTY session on the target machine, then persists its metadata.
// If the agent rejects the open, no record is persisted.
func (u *UseCase) Open(ctx context.Context, in OpenInput) (session.Session, error) {
	cols, rows := in.Cols, in.Rows
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}

	id := u.newID()
	pid, err := u.gateway.OpenSession(ctx, in.MachineID, id, in.Cwd, in.Shell, cols, rows, in.CreateDir, false)
	if err != nil {
		return session.Session{}, err
	}

	title := in.Title
	if title == "" {
		title = generateSessionName()
	}

	now := u.clock.Now()
	s := session.New(id, in.MachineID, in.ProjectID, title, in.Shell, in.Cwd, now)
	if err := u.store.Create(ctx, s); err != nil {
		_ = u.gateway.CloseSession(ctx, in.MachineID, id)
		return session.Session{}, err
	}

	_ = u.audit.Record(ctx, audit.ActionOpen, in.MachineID, id, "")
	u.log.Info("session opened", "sessionID", id, "machineID", in.MachineID, "pid", pid)
	return s, nil
}

// List returns all session records.
func (u *UseCase) List(ctx context.Context) ([]session.Session, error) {
	return u.store.List(ctx)
}

// ListByMachine returns all sessions for the given machine.
func (u *UseCase) ListByMachine(ctx context.Context, machineID string) ([]session.Session, error) {
	return u.store.ListByMachine(ctx, machineID)
}

// ByID returns a single session by its ID.
func (u *UseCase) ByID(ctx context.Context, id string) (session.Session, error) {
	return u.store.ByID(ctx, id)
}

// Close instructs the agent to close the session. The agent confirms exit asynchronously
// via SessionExited → MarkExited.
func (u *UseCase) Close(ctx context.Context, id string) error {
	s, err := u.store.ByID(ctx, id)
	if err != nil {
		return err
	}
	if err := u.gateway.CloseSession(ctx, s.MachineID(), id); err != nil {
		return err
	}
	_ = u.audit.Record(ctx, audit.ActionClose, s.MachineID(), id, "")
	return nil
}

// Delete permanently removes a session record. Only non-running sessions
// (exited/lost) may be deleted: a running session is refused with
// ErrSessionRunning — close it first. Returns session.ErrNotFound if no
// session with the given id exists.
func (u *UseCase) Delete(ctx context.Context, id string) error {
	s, err := u.store.ByID(ctx, id)
	if err != nil {
		return err
	}
	if s.Status() == session.StatusRunning {
		return ErrSessionRunning
	}
	if err := u.store.Delete(ctx, id); err != nil {
		return err
	}
	_ = u.audit.Record(ctx, audit.ActionDelete, s.MachineID(), id, "")
	return nil
}

// ForceDelete signals the agent to stop the PTY (best-effort — an offline or
// erroring agent is ignored) and unconditionally removes the record, including
// running sessions. Returns session.ErrNotFound if no record exists.
func (u *UseCase) ForceDelete(ctx context.Context, id string) error {
	s, err := u.store.ByID(ctx, id)
	if err != nil {
		return err
	}
	if s.Status() == session.StatusRunning {
		if cerr := u.gateway.CloseSession(ctx, s.MachineID(), id); cerr != nil {
			u.log.Debug("sessions: ForceDelete: close signal failed (ignored)", "sessionID", id, "err", cerr)
		}
	}
	if err := u.store.Delete(ctx, id); err != nil {
		return err
	}
	_ = u.audit.Record(ctx, audit.ActionDelete, s.MachineID(), id, "force")
	return nil
}

// MarkExited records that a session has exited. Satisfies wsagent.SessionEvents.
// A not-found session is silently ignored: it may have been force-deleted before
// the agent's asynchronous exit report arrived (a benign race).
func (u *UseCase) MarkExited(ctx context.Context, id string, exitCode int) error {
	if err := u.store.SetExited(ctx, id, exitCode, u.clock.Now()); err != nil {
		if errors.Is(err, session.ErrNotFound) {
			u.log.Debug("sessions: MarkExited: session not found (may have been force-deleted)", "sessionID", id)
			return nil
		}
		return err
	}
	return nil
}

// ReconcileMachineRestart handles a detected agent process restart (new instanceID on Hello).
// Sessions with auto_relaunch=true are re-opened on the agent (same session ID, preserving scrollback).
// Sessions with auto_relaunch=false are marked lost, as before.
// Idempotency: the registry only returns restarted=true once per instanceID change, so this
// method is called at most once per actual restart event.
// Satisfies wsagent.SessionEvents.
func (u *UseCase) ReconcileMachineRestart(ctx context.Context, machineID string) error {
	ts := u.clock.Now()

	revivals, err := u.store.AutoRelaunchSessions(ctx, machineID)
	if err != nil {
		u.log.Error("sessions: ReconcileMachineRestart: fetch auto-relaunch sessions failed",
			"machineID", machineID, "err", err)
		// Fall back: mark all running sessions lost so nothing is silently orphaned.
		return u.store.MarkRunningLost(ctx, machineID, ts)
	}

	for _, s := range revivals {
		pid, rerr := u.gateway.OpenSession(ctx, machineID, s.ID(), s.Cwd(), s.Shell(), 80, 24, false, true)
		if rerr != nil {
			u.log.Warn("sessions: relaunch failed, marking session lost",
				"sessionID", s.ID(), "machineID", machineID, "err", rerr)
			if lerr := u.store.SetLost(ctx, s.ID(), ts); lerr != nil {
				u.log.Error("sessions: SetLost after failed relaunch failed",
					"sessionID", s.ID(), "err", lerr)
			}
			continue
		}
		if serr := u.store.SetRunning(ctx, s.ID()); serr != nil {
			u.log.Warn("sessions: SetRunning after relaunch failed", "sessionID", s.ID(), "err", serr)
		}
		_ = u.audit.Record(ctx, audit.ActionRelaunch, machineID, s.ID(), "")
		u.log.Info("sessions: session relaunched after restart",
			"sessionID", s.ID(), "machineID", machineID, "pid", pid)
	}

	// Mark remaining running sessions (auto_relaunch=false) as lost.
	return u.store.MarkRunningLost(ctx, machineID, ts)
}

// SetAutoRelaunch enables or disables auto-relaunch for a session.
// Returns session.ErrNotFound if no session with the given id exists.
func (u *UseCase) SetAutoRelaunch(ctx context.Context, id string, v bool) error {
	return u.store.SetAutoRelaunch(ctx, id, v)
}

// Rename updates the title of a session. Returns session.ErrNotFound if no
// session with the given id exists.
func (u *UseCase) Rename(ctx context.Context, id, title string) error {
	return u.store.SetTitle(ctx, id, title)
}

// RecordStat persists the per-session activity and/or live working directory
// (pwd) from a heartbeat. An unrecognised activity is dropped (not persisted)
// but a valid pwd on the same stat is still persisted. When both are empty the
// call is a no-op. A not-found session (agent reported before the hub has
// created it) is silently ignored — this must not break the heartbeat path.
func (u *UseCase) RecordStat(ctx context.Context, sessionID, activity, pwd string) error {
	switch activity {
	case session.ActivityActive, session.ActivityIdle,
		session.ActivityAwaitingInput, session.ActivityUnknown:
	default:
		activity = "" // don't persist a bogus activity, but still persist pwd
	}
	if activity == "" && pwd == "" {
		return nil
	}

	var lastActiveAt int64
	if activity == session.ActivityActive {
		lastActiveAt = u.clock.Now()
	}

	if err := u.store.SetStat(ctx, sessionID, activity, pwd, lastActiveAt); err != nil {
		if errors.Is(err, session.ErrNotFound) {
			u.log.Debug("sessions: RecordStat: session not found (agent may precede hub record)", "sessionID", sessionID)
			return nil
		}
		return err
	}
	return nil
}
