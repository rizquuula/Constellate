package attach

import (
	"context"
	"io"

	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

// SessionStore is the read port for session records.
type SessionStore interface {
	ByID(ctx context.Context, id string) (session.Session, error)
}

// AgentGateway is the outbound port for data stream operations.
type AgentGateway interface {
	OpenDataStream(ctx context.Context, machineID, sessionID string) (io.ReadWriteCloser, error)
	Resize(ctx context.Context, machineID, sessionID string, cols, rows int) error
}
