package sessions

import (
	"context"

	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

// SessionStore is the persistence port for session records.
type SessionStore interface {
	Create(ctx context.Context, s session.Session) error
	ByID(ctx context.Context, id string) (session.Session, error)
	List(ctx context.Context) ([]session.Session, error)
	ListByMachine(ctx context.Context, machineID string) ([]session.Session, error)
	AutoRelaunchSessions(ctx context.Context, machineID string) ([]session.Session, error)
	SetExited(ctx context.Context, id string, exitCode int, ts int64) error
	MarkRunningLost(ctx context.Context, machineID string, ts int64) error
	// MarkRunningDisconnected bulk-marks a machine's running sessions as
	// disconnected (a dropped connection; PTYs presumed alive).
	MarkRunningDisconnected(ctx context.Context, machineID string, ts int64) error
	// MarkDisconnectedRunning restores a machine's disconnected sessions to
	// running after a same-instance reconnect (the blip is over).
	MarkDisconnectedRunning(ctx context.Context, machineID string) error
	SetRunning(ctx context.Context, id string) error
	SetLost(ctx context.Context, id string, ts int64) error
	SetTitle(ctx context.Context, id, title string) error
	SetAutoRelaunch(ctx context.Context, id string, v bool) error
	// SetStat updates a session's activity and/or live working directory (pwd).
	// An empty activity or pwd leaves that column untouched (preserve-on-empty).
	SetStat(ctx context.Context, id, activity, pwd string, lastActiveAt int64) error
	Delete(ctx context.Context, id string) error
}

// AgentGateway is the outbound port for controlling agent PTY sessions.
type AgentGateway interface {
	OpenSession(ctx context.Context, machineID, sessionID, cwd, shell string, cols, rows int, createDir, revive bool) (pid int, err error)
	CloseSession(ctx context.Context, machineID, sessionID string) error
}

// Clock returns the current unix-second timestamp.
type Clock interface {
	Now() int64
}

// AuditSink is the consumer-side port for recording security-relevant actions.
type AuditSink interface {
	Record(ctx context.Context, action audit.Action, machineID, sessionID, detail string) error
}
