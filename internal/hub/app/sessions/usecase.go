package sessions

import (
	"context"
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
	pid, err := u.gateway.OpenSession(ctx, in.MachineID, id, in.Cwd, in.Shell, cols, rows)
	if err != nil {
		return session.Session{}, err
	}

	now := u.clock.Now()
	s := session.New(id, in.MachineID, in.ProjectID, in.Title, in.Shell, now)
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

// MarkExited records that a session has exited. Satisfies wsagent.SessionEvents.
func (u *UseCase) MarkExited(ctx context.Context, id string, exitCode int) error {
	return u.store.SetExited(ctx, id, exitCode, u.clock.Now())
}

// MarkMachineSessionsLost bulk-marks all running sessions for a machine as lost.
// Called when a process restart is detected (new instanceID on Hello).
func (u *UseCase) MarkMachineSessionsLost(ctx context.Context, machineID string) error {
	return u.store.MarkRunningLost(ctx, machineID, u.clock.Now())
}

// Rename updates the title of a session. Returns session.ErrNotFound if no
// session with the given id exists.
func (u *UseCase) Rename(ctx context.Context, id, title string) error {
	return u.store.SetTitle(ctx, id, title)
}
