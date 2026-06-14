package audit

import (
	"context"
	"log/slog"

	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
)

// UseCase records security-relevant actions to the audit store.
type UseCase struct {
	store AuditStore
	clock Clock
	log   *slog.Logger
}

// New constructs a UseCase with the provided adapters.
func New(store AuditStore, clock Clock, log *slog.Logger) *UseCase {
	return &UseCase{
		store: store,
		clock: clock,
		log:   log,
	}
}

// Record stamps a new audit event with the current time and the actor from ctx,
// then appends it to the store. Audit is best-effort: the error is returned so
// callers can choose to ignore it.
func (u *UseCase) Record(ctx context.Context, action audit.Action, machineID, sessionID, detail string) error {
	e := audit.NewEvent(u.clock.Now(), audit.ActorFromContext(ctx), action, machineID, sessionID, detail)
	if err := u.store.Append(ctx, e); err != nil {
		u.log.Warn("audit: append failed", "action", action, "err", err)
		return err
	}
	u.log.Debug("audit: recorded", "action", action, "actor", e.Actor(), "machineID", machineID, "sessionID", sessionID)
	return nil
}
