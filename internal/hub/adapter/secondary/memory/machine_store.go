package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
)

// MachineStore is a thread-safe in-memory implementation of registry.MachineStore.
// Used in tests and the in-process E2E harness.
type MachineStore struct {
	mu       sync.RWMutex
	machines map[string]machine.Machine
}

// NewMachineStore returns an empty MachineStore.
func NewMachineStore() *MachineStore {
	return &MachineStore{machines: make(map[string]machine.Machine)}
}

// Upsert inserts a machine or updates its mutable fields. enrolled_at and
// revoked_at are preserved on an existing record.
func (s *MachineStore) Upsert(_ context.Context, m machine.Machine) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.machines[m.ID()]
	if ok {
		updated := machine.RehydrateFull(
			m.ID(), m.InstanceID(), m.Name(), m.OS(), m.Arch(), m.AgentVersion(),
			existing.EnrolledAt(), m.LastSeenAt(), existing.RevokedAt(),
		)
		s.machines[m.ID()] = updated
		return nil
	}
	s.machines[m.ID()] = m
	return nil
}

// UpdateLastSeen bumps the last_seen_at for the given id.
// Returns machine.ErrNotFound (wrapped) if the id is unknown.
func (s *MachineStore) UpdateLastSeen(_ context.Context, id string, ts int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, ok := s.machines[id]
	if !ok {
		return fmt.Errorf("memory: update last_seen_at %q: %w", id, machine.ErrNotFound)
	}
	updated := machine.RehydrateFull(m.ID(), m.InstanceID(), m.Name(), m.OS(), m.Arch(), m.AgentVersion(),
		m.EnrolledAt(), ts, m.RevokedAt())
	s.machines[id] = updated
	return nil
}

// MarkRevoked sets revoked_at for the given id.
// Returns machine.ErrNotFound (wrapped) if not present.
func (s *MachineStore) MarkRevoked(_ context.Context, id string, ts int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, ok := s.machines[id]
	if !ok {
		return fmt.Errorf("memory: mark revoked %q: %w", id, machine.ErrNotFound)
	}
	s.machines[id] = m.MarkRevoked(ts)
	return nil
}

// List returns a snapshot of all stored machines.
func (s *MachineStore) List(_ context.Context) ([]machine.Machine, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]machine.Machine, 0, len(s.machines))
	for _, m := range s.machines {
		out = append(out, m)
	}
	return out, nil
}


// ByID returns the machine with the given id.
// Returns machine.ErrNotFound (wrapped) if not present.
func (s *MachineStore) ByID(_ context.Context, id string) (machine.Machine, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	m, ok := s.machines[id]
	if !ok {
		return machine.Machine{}, fmt.Errorf("memory: by id %q: %w", id, machine.ErrNotFound)
	}
	return m, nil
}
