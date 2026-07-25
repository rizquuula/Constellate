package attach

import (
	"context"
	"io"
	"log/slog"

	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

// UseCase orchestrates browser attachment to an existing PTY session.
type UseCase struct {
	store   SessionStore
	gateway AgentGateway
	audit   AuditSink
	log     *slog.Logger
}

// New constructs a UseCase with the provided adapters.
func New(store SessionStore, gateway AgentGateway, log *slog.Logger, auditSink AuditSink) *UseCase {
	return &UseCase{
		store:   store,
		gateway: gateway,
		audit:   auditSink,
		log:     log,
	}
}

// OpenStream resolves the session's machine and opens a data stream to its PTY.
func (u *UseCase) OpenStream(ctx context.Context, sessionID string) (machineID string, stream io.ReadWriteCloser, err error) {
	s, err := u.store.ByID(ctx, sessionID)
	if err != nil {
		return "", nil, err
	}
	// An ended session has no PTY to attach to. Refusing here — before the
	// gateway call and before the audit record — keeps the browser from
	// retry-looping on a permanent failure and writing a spurious attach event
	// per attempt. StatusDisconnected still proceeds: the machine may be
	// mid-blip, and that path yields the retryable agent-offline error.
	if st := s.Status(); st == session.StatusExited || st == session.StatusLost {
		return "", nil, session.ErrEnded
	}
	stream, err = u.gateway.OpenDataStream(ctx, s.MachineID(), sessionID)
	if err != nil {
		return "", nil, err
	}
	_ = u.audit.Record(ctx, audit.ActionAttach, s.MachineID(), sessionID, "")
	return s.MachineID(), stream, nil
}

// Resize forwards a PTY resize request to the correct agent.
func (u *UseCase) Resize(ctx context.Context, sessionID string, cols, rows int) error {
	s, err := u.store.ByID(ctx, sessionID)
	if err != nil {
		return err
	}
	return u.gateway.Resize(ctx, s.MachineID(), sessionID, cols, rows)
}
