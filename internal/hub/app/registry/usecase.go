package registry

import (
	"context"
	"errors"
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
	InstanceID   string
	Name         string
	OS           string
	Arch         string
	AgentVersion string
}

// MachineView pairs a Machine with its current online status and host metrics.
type MachineView struct {
	Machine machine.Machine
	Online  bool
	Metrics *Metrics
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

// Register upserts the machine record and detects process restarts via instanceID.
// restarted is true when a prior record exists with a non-empty instanceID that
// differs from the incoming one (same instanceID means WS reconnect, not restart).
// On a re-register the store preserves enrolled_at.
func (u *UseCase) Register(ctx context.Context, in RegisterInput) (m machine.Machine, restarted bool, err error) {
	prior, priorErr := u.store.ByID(ctx, in.MachineID)
	if priorErr != nil && !errors.Is(priorErr, machine.ErrNotFound) {
		return machine.Machine{}, false, priorErr
	}
	if priorErr == nil && in.InstanceID != "" && prior.InstanceID() != "" && prior.InstanceID() != in.InstanceID {
		restarted = true
	}

	now := u.clock.Now()
	m = machine.New(in.MachineID, in.InstanceID, in.Name, in.OS, in.Arch, in.AgentVersion, now)
	if err := u.store.Upsert(ctx, m); err != nil {
		return machine.Machine{}, false, err
	}
	u.log.Info("machine registered", "machineID", in.MachineID, "name", in.Name, "restarted", restarted)
	return m, restarted, nil
}

// Heartbeat records that the machine was seen at the current time.
func (u *UseCase) Heartbeat(ctx context.Context, id string) error {
	return u.store.UpdateLastSeen(ctx, id, u.clock.Now())
}

// List returns all machines with their online status and host metrics overlaid.
func (u *UseCase) List(ctx context.Context) ([]MachineView, error) {
	machines, err := u.store.List(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]MachineView, len(machines))
	for i, m := range machines {
		online := u.live.IsOnline(m.ID())
		v := MachineView{
			Machine: m,
			Online:  online,
		}
		if online {
			if met, ok := u.live.Metrics(m.ID()); ok {
				v.Metrics = &met
			}
		}
		views[i] = v
	}
	return views, nil
}
