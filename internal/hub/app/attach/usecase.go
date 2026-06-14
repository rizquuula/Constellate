package attach

import (
	"context"
	"io"
	"log/slog"
)

// UseCase orchestrates browser attachment to an existing PTY session.
type UseCase struct {
	store   SessionStore
	gateway AgentGateway
	log     *slog.Logger
}

// New constructs a UseCase with the provided adapters.
func New(store SessionStore, gateway AgentGateway, log *slog.Logger) *UseCase {
	return &UseCase{
		store:   store,
		gateway: gateway,
		log:     log,
	}
}

// OpenStream resolves the session's machine and opens a data stream to its PTY.
func (u *UseCase) OpenStream(ctx context.Context, sessionID string) (machineID string, stream io.ReadWriteCloser, err error) {
	s, err := u.store.ByID(ctx, sessionID)
	if err != nil {
		return "", nil, err
	}
	stream, err = u.gateway.OpenDataStream(ctx, s.MachineID(), sessionID)
	if err != nil {
		return "", nil, err
	}
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
