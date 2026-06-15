package registry_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/app/registry"
	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
)

// --- fakes ---

type fakeStore struct {
	machines map[string]machine.Machine
}

func newFakeStore() *fakeStore {
	return &fakeStore{machines: make(map[string]machine.Machine)}
}

func (s *fakeStore) Upsert(_ context.Context, m machine.Machine) error {
	existing, ok := s.machines[m.ID()]
	if ok {
		// preserve enrolled_at: rehydrate with original enrolled_at
		updated := machine.Rehydrate(m.ID(), m.InstanceID(), m.Name(), m.OS(), m.Arch(), m.AgentVersion(),
			existing.EnrolledAt(), m.LastSeenAt())
		s.machines[m.ID()] = updated
		return nil
	}
	s.machines[m.ID()] = m
	return nil
}

func (s *fakeStore) UpdateLastSeen(_ context.Context, id string, ts int64) error {
	m, ok := s.machines[id]
	if !ok {
		return machine.ErrNotFound
	}
	updated := machine.Rehydrate(m.ID(), m.InstanceID(), m.Name(), m.OS(), m.Arch(), m.AgentVersion(),
		m.EnrolledAt(), ts)
	s.machines[id] = updated
	return nil
}

func (s *fakeStore) List(_ context.Context) ([]machine.Machine, error) {
	out := make([]machine.Machine, 0, len(s.machines))
	for _, m := range s.machines {
		out = append(out, m)
	}
	return out, nil
}

func (s *fakeStore) ByID(_ context.Context, id string) (machine.Machine, error) {
	m, ok := s.machines[id]
	if !ok {
		return machine.Machine{}, machine.ErrNotFound
	}
	return m, nil
}

type fakeLive struct {
	online  map[string]bool
	metrics map[string]registry.Metrics
}

func (f *fakeLive) IsOnline(id string) bool { return f.online[id] }
func (f *fakeLive) OnlineIDs() []string {
	ids := make([]string, 0, len(f.online))
	for id, on := range f.online {
		if on {
			ids = append(ids, id)
		}
	}
	return ids
}
func (f *fakeLive) Metrics(id string) (registry.Metrics, bool) {
	m, ok := f.metrics[id]
	return m, ok
}

type fakeClock struct{ ts int64 }

func (c *fakeClock) Now() int64 { return c.ts }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))
}

// --- tests ---

func TestRegisterAndList_OnlineStatus(t *testing.T) {
	store := newFakeStore()
	live := &fakeLive{online: map[string]bool{"m1": true}}
	clk := &fakeClock{ts: 1000}
	uc := registry.New(store, live, clk, discardLogger())
	ctx := context.Background()

	_, _, err := uc.Register(ctx, registry.RegisterInput{
		MachineID: "m1", Name: "box1", OS: "linux", Arch: "amd64", AgentVersion: "0.1",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	_, _, err = uc.Register(ctx, registry.RegisterInput{
		MachineID: "m2", Name: "box2", OS: "linux", Arch: "arm64", AgentVersion: "0.1",
	})
	if err != nil {
		t.Fatalf("Register m2: %v", err)
	}

	views, err := uc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("List: got %d views, want 2", len(views))
	}

	byID := make(map[string]registry.MachineView)
	for _, v := range views {
		byID[v.Machine.ID()] = v
	}

	if !byID["m1"].Online {
		t.Error("m1 should be online")
	}
	if byID["m2"].Online {
		t.Error("m2 should be offline")
	}
}

func TestHeartbeat_UpdatesLastSeen(t *testing.T) {
	store := newFakeStore()
	live := &fakeLive{online: map[string]bool{}}
	clk := &fakeClock{ts: 1000}
	uc := registry.New(store, live, clk, discardLogger())
	ctx := context.Background()

	_, _, err := uc.Register(ctx, registry.RegisterInput{MachineID: "m1", Name: "box1"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	clk.ts = 2000
	if err := uc.Heartbeat(ctx, "m1"); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	m, err := store.ByID(ctx, "m1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if m.LastSeenAt() != 2000 {
		t.Errorf("LastSeenAt: got %d, want 2000", m.LastSeenAt())
	}
}

func TestReRegister_PreservesEnrolledAt(t *testing.T) {
	store := newFakeStore()
	live := &fakeLive{online: map[string]bool{}}
	clk := &fakeClock{ts: 1000}
	uc := registry.New(store, live, clk, discardLogger())
	ctx := context.Background()

	_, _, err := uc.Register(ctx, registry.RegisterInput{MachineID: "m1", Name: "box1"})
	if err != nil {
		t.Fatalf("first Register: %v", err)
	}

	clk.ts = 9999
	_, _, err = uc.Register(ctx, registry.RegisterInput{MachineID: "m1", Name: "box1-renamed"})
	if err != nil {
		t.Fatalf("second Register: %v", err)
	}

	m, err := store.ByID(ctx, "m1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if m.EnrolledAt() != 1000 {
		t.Errorf("re-register must preserve enrolled_at: got %d, want 1000", m.EnrolledAt())
	}
	if m.Name() != "box1-renamed" {
		t.Errorf("re-register should update name: got %q", m.Name())
	}
}

func TestList_MetricsPopulatedForOnlineMachine(t *testing.T) {
	store := newFakeStore()
	met := registry.Metrics{CPUPercent: 30.0, MemUsedMB: 1024, MemTotalMB: 16384}
	live := &fakeLive{
		online:  map[string]bool{"m1": true},
		metrics: map[string]registry.Metrics{"m1": met},
	}
	clk := &fakeClock{ts: 1000}
	uc := registry.New(store, live, clk, discardLogger())
	ctx := context.Background()

	_, _, err := uc.Register(ctx, registry.RegisterInput{MachineID: "m1", Name: "box1"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	views, err := uc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("List: got %d views, want 1", len(views))
	}
	v := views[0]
	if v.Metrics == nil {
		t.Fatal("MachineView.Metrics: got nil, want non-nil for online machine")
	}
	if v.Metrics.CPUPercent != met.CPUPercent {
		t.Errorf("CPUPercent: got %v, want %v", v.Metrics.CPUPercent, met.CPUPercent)
	}
	if v.Metrics.MemTotalMB != met.MemTotalMB {
		t.Errorf("MemTotalMB: got %d, want %d", v.Metrics.MemTotalMB, met.MemTotalMB)
	}
}

func TestList_MetricsNilForOfflineMachine(t *testing.T) {
	store := newFakeStore()
	live := &fakeLive{
		online:  map[string]bool{},
		metrics: map[string]registry.Metrics{},
	}
	clk := &fakeClock{ts: 1000}
	uc := registry.New(store, live, clk, discardLogger())
	ctx := context.Background()

	_, _, err := uc.Register(ctx, registry.RegisterInput{MachineID: "m1", Name: "box1"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	views, err := uc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("List: got %d views, want 1", len(views))
	}
	if views[0].Metrics != nil {
		t.Errorf("MachineView.Metrics: got %+v, want nil for offline machine", views[0].Metrics)
	}
}

func TestHeartbeat_MissingMachine(t *testing.T) {
	store := newFakeStore()
	live := &fakeLive{online: map[string]bool{}}
	clk := &fakeClock{ts: 1000}
	uc := registry.New(store, live, clk, discardLogger())

	err := uc.Heartbeat(context.Background(), "no-such-id")
	if !errors.Is(err, machine.ErrNotFound) {
		t.Errorf("Heartbeat missing machine: got %v, want machine.ErrNotFound", err)
	}
}

func TestRegister_RestartDetection(t *testing.T) {
	store := newFakeStore()
	live := &fakeLive{online: map[string]bool{}}
	clk := &fakeClock{ts: 1000}
	uc := registry.New(store, live, clk, discardLogger())
	ctx := context.Background()

	// First register (no prior row) → restarted=false.
	_, restarted, err := uc.Register(ctx, registry.RegisterInput{
		MachineID: "m1", InstanceID: "inst-A", Name: "box1",
	})
	if err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if restarted {
		t.Error("first register: restarted should be false (no prior row)")
	}

	// Same instanceID again → restarted=false (WS reconnect).
	_, restarted, err = uc.Register(ctx, registry.RegisterInput{
		MachineID: "m1", InstanceID: "inst-A", Name: "box1",
	})
	if err != nil {
		t.Fatalf("second Register (same instanceID): %v", err)
	}
	if restarted {
		t.Error("same instanceID: restarted should be false (reconnect, not restart)")
	}

	// Different instanceID → restarted=true (process restart).
	_, restarted, err = uc.Register(ctx, registry.RegisterInput{
		MachineID: "m1", InstanceID: "inst-B", Name: "box1",
	})
	if err != nil {
		t.Fatalf("third Register (different instanceID): %v", err)
	}
	if !restarted {
		t.Error("different instanceID: restarted should be true")
	}

	// Empty incoming instanceID → restarted=false (legacy caller).
	_, restarted, err = uc.Register(ctx, registry.RegisterInput{
		MachineID: "m1", InstanceID: "", Name: "box1",
	})
	if err != nil {
		t.Fatalf("fourth Register (empty instanceID): %v", err)
	}
	if restarted {
		t.Error("empty incoming instanceID: restarted should be false")
	}
}
