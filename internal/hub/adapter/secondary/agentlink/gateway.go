package agentlink

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/rizquuula/Constellate/internal/transport"
)

// ErrAgentOffline is returned when the target machine is not connected.
var ErrAgentOffline = errors.New("agentlink: agent offline")

// Gateway sends control commands to agents via the agentlink Registry.
type Gateway struct {
	reg *Registry
	log *slog.Logger
}

// NewGateway returns a Gateway backed by reg.
func NewGateway(reg *Registry) *Gateway {
	return &Gateway{reg: reg, log: slog.Default()}
}

// OpenSession instructs the agent identified by machineID to start a new PTY session.
// It blocks until the agent replies with SessionOpened or an error, the context is
// cancelled, or a 10-second timeout expires.
func (g *Gateway) OpenSession(ctx context.Context, machineID, sessionID, cwd, shell string, cols, rows int) (pid int, err error) {
	conn, ok := g.reg.Get(machineID)
	if !ok {
		return 0, ErrAgentOffline
	}

	ch := conn.awaitOpen(sessionID)
	if err := conn.sendControl(transport.NewOpenSession(sessionID, cwd, shell, cols, rows)); err != nil {
		conn.cancelOpen(sessionID)
		return 0, err
	}

	select {
	case res := <-ch:
		return res.pid, res.err
	case <-ctx.Done():
		conn.cancelOpen(sessionID)
		return 0, ctx.Err()
	case <-time.After(10 * time.Second):
		conn.cancelOpen(sessionID)
		return 0, errors.New("agentlink: OpenSession timed out")
	}
}

// Resize instructs the agent to resize an existing session's PTY.
func (g *Gateway) Resize(ctx context.Context, machineID, sessionID string, cols, rows int) error {
	conn, ok := g.reg.Get(machineID)
	if !ok {
		return ErrAgentOffline
	}
	_ = ctx
	return conn.sendControl(transport.NewResize(sessionID, cols, rows))
}

// CloseSession instructs the agent to terminate a session.
func (g *Gateway) CloseSession(ctx context.Context, machineID, sessionID string) error {
	conn, ok := g.reg.Get(machineID)
	if !ok {
		return ErrAgentOffline
	}
	_ = ctx
	return conn.sendControl(transport.NewCloseSession(sessionID))
}

// SetSnapshotsEnabled broadcasts an EnableSnaps message to all currently online agents.
// Per-conn errors are logged and swallowed so one broken connection doesn't block others.
// Implements overview.SnapshotControl.
func (g *Gateway) SetSnapshotsEnabled(enabled bool) {
	ids := g.reg.OnlineIDs()
	for _, id := range ids {
		conn, ok := g.reg.Get(id)
		if !ok {
			continue
		}
		if err := conn.EnableSnaps(enabled); err != nil {
			g.log.Warn("agentlink: EnableSnaps send failed", "machineID", id, "enabled", enabled, "err", err)
		}
	}
}

// OpenDataStream opens a hub-initiated yamux stream to the agent for the given session.
// Returns an io.ReadWriteCloser so callers outside this package are not coupled to net.Conn.
func (g *Gateway) OpenDataStream(ctx context.Context, machineID, sessionID string) (io.ReadWriteCloser, error) {
	conn, ok := g.reg.Get(machineID)
	if !ok {
		return nil, ErrAgentOffline
	}
	_ = ctx
	return conn.openDataStream(sessionID)
}
