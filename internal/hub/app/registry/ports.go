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

// Metrics holds the latest host resource sample received from a live agent.
type Metrics struct {
	CPUPercent float64
	MemUsedMB  int64
	MemTotalMB int64
}

// LiveAgents is the read port for live agent connection presence.
type LiveAgents interface {
	IsOnline(id string) bool
	OnlineIDs() []string
	// Metrics returns the latest host metrics for machineID and whether any
	// sample has been received. Only populated while the agent is online.
	Metrics(id string) (Metrics, bool)
}

// Clock returns the current unix-second timestamp.
type Clock interface {
	Now() int64
}
