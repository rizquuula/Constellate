package sessions

import (
	"context"

	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

// SessionStore is the persistence port for session records.
type SessionStore interface {
	Create(ctx context.Context, s session.Session) error
	ByID(ctx context.Context, id string) (session.Session, error)
	List(ctx context.Context) ([]session.Session, error)
	ListByMachine(ctx context.Context, machineID string) ([]session.Session, error)
	SetExited(ctx context.Context, id string, exitCode int, ts int64) error
}

// AgentGateway is the outbound port for controlling agent PTY sessions.
type AgentGateway interface {
	OpenSession(ctx context.Context, machineID, sessionID, cwd, shell string, cols, rows int) (pid int, err error)
	CloseSession(ctx context.Context, machineID, sessionID string) error
}

// Clock returns the current unix-second timestamp.
type Clock interface {
	Now() int64
}
