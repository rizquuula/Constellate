package registry

import (
	"context"
	"log/slog"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
)

// SystemClock implements Clock using the real wall clock.
type SystemClock struct{}

func (SystemClock) Now() int64 { return time.Now().Unix() }

// RegisterInput carries the data needed to register or re-register a machine.
type RegisterInput struct {
	MachineID    string
	Name         string
	OS           string
	Arch         string
	AgentVersion string
}

// MachineView pairs a Machine with its current online status.
type MachineView struct {
	Machine machine.Machine
	Online  bool
}

// UseCase orchestrates machine registration, heartbeats, and listing.
type UseCase struct {
	store MachineStore
	live  LiveAgents
	clock Clock
	log   *slog.Logger
}

// New constructs a UseCase with the provided adapters.
func New(store MachineStore, live LiveAgents, clock Clock, log *slog.Logger) *UseCase {
	return &UseCase{
		store: store,
		live:  live,
		clock: clock,
		log:   log,
	}
}

// Register upserts the machine record. On a re-register the store must preserve enrolled_at.
func (u *UseCase) Register(ctx context.Context, in RegisterInput) (machine.Machine, error) {
	now := u.clock.Now()
	m := machine.New(in.MachineID, in.Name, in.OS, in.Arch, in.AgentVersion, now)
	if err := u.store.Upsert(ctx, m); err != nil {
		return machine.Machine{}, err
	}
	u.log.Info("machine registered", "machineID", in.MachineID, "name", in.Name)
	return m, nil
}

// Heartbeat records that the machine was seen at the current time.
func (u *UseCase) Heartbeat(ctx context.Context, id string) error {
	return u.store.UpdateLastSeen(ctx, id, u.clock.Now())
}

// List returns all machines with their online status overlaid.
func (u *UseCase) List(ctx context.Context) ([]MachineView, error) {
	machines, err := u.store.List(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]MachineView, len(machines))
	for i, m := range machines {
		views[i] = MachineView{
			Machine: m,
			Online:  u.live.IsOnline(m.ID()),
		}
	}
	return views, nil
}
