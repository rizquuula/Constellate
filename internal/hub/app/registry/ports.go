package registry

import (
	"context"

	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
)

// MachineStore is the persistence port for machine records.
type MachineStore interface {
	Upsert(ctx context.Context, m machine.Machine) error
	UpdateLastSeen(ctx context.Context, id string, ts int64) error
	List(ctx context.Context) ([]machine.Machine, error)
	ByID(ctx context.Context, id string) (machine.Machine, error)
}

// LiveAgents is the read port for live agent connection presence.
type LiveAgents interface {
	IsOnline(id string) bool
	OnlineIDs() []string
}

// Clock returns the current unix-second timestamp.
type Clock interface {
	Now() int64
}
