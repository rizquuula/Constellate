package sessions_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/app/sessions"
	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

// --- fakes ---

type fakeSessionStore struct {
	data    map[string]session.Session
	created []session.Session
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{data: make(map[string]session.Session)}
}

func (s *fakeSessionStore) Create(_ context.Context, ss session.Session) error {
	s.data[ss.ID()] = ss
	s.created = append(s.created, ss)
	return nil
}

func (s *fakeSessionStore) ByID(_ context.Context, id string) (session.Session, error) {
	ss, ok := s.data[id]
	if !ok {
		return session.Session{}, session.ErrNotFound
	}
	return ss, nil
}

func (s *fakeSessionStore) List(_ context.Context) ([]session.Session, error) {
	out := make([]session.Session, 0, len(s.data))
	for _, ss := range s.data {
		out = append(out, ss)
	}
	return out, nil
}

func (s *fakeSessionStore) ListByMachine(_ context.Context, machineID string) ([]session.Session, error) {
	var out []session.Session
	for _, ss := range s.data {
		if ss.MachineID() == machineID {
			out = append(out, ss)
		}
	}
	return out, nil
}

func (s *fakeSessionStore) SetExited(_ context.Context, id string, exitCode int, ts int64) error {
	ss, ok := s.data[id]
	if !ok {
		return session.ErrNotFound
	}
	ss.SetExited(exitCode, ts)
	s.data[id] = ss
	return nil
}

func (s *fakeSessionStore) MarkRunningLost(_ context.Context, machineID string, ts int64) error {
	for id, ss := range s.data {
		if ss.MachineID() == machineID && ss.Status() == session.StatusRunning {
			ss.SetStatus(session.StatusLost)
			ss.Touch(ts)
			s.data[id] = ss
		}
	}
	return nil
}

func (s *fakeSessionStore) SetTitle(_ context.Context, id, title string) error {
	ss, ok := s.data[id]
	if !ok {
		return session.ErrNotFound
	}
	ss.SetTitle(title)
	s.data[id] = ss
	return nil
}

type fakeGateway struct {
	openErr    error
	closeCalls []string
	openCalls  int
	pidReturn  int
}

func (g *fakeGateway) OpenSession(_ context.Context, _, sessionID, _, _ string, _, _ int) (int, error) {
	g.openCalls++
	return g.pidReturn, g.openErr
}

func (g *fakeGateway) CloseSession(_ context.Context, _, sessionID string) error {
	g.closeCalls = append(g.closeCalls, sessionID)
	return nil
}

type fixedClock struct{ ts int64 }

func (c *fixedClock) Now() int64 { return c.ts }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))
}

var idCounter int

func nextID() func() string {
	idCounter = 0
	return func() string {
		idCounter++
		return "generated-id"
	}
}

// --- tests ---

func TestOpen_SpawnsThenPersists(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 42}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger())

	s, err := uc.Open(context.Background(), sessions.OpenInput{
		MachineID: "m1",
		ProjectID: "p1",
		Title:     "test",
		Shell:     "/bin/bash",
		Cols:      80,
		Rows:      24,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if s.MachineID() != "m1" {
		t.Errorf("MachineID: got %q, want m1", s.MachineID())
	}
	if s.Status() != session.StatusRunning {
		t.Errorf("Status: got %q, want running", s.Status())
	}
	if gw.openCalls != 1 {
		t.Errorf("gateway.OpenSession called %d times, want 1", gw.openCalls)
	}
	if len(store.created) != 1 {
		t.Errorf("store.Create called %d times, want 1", len(store.created))
	}
}

func TestOpen_DefaultsDimensions(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger())

	_, err := uc.Open(context.Background(), sessions.OpenInput{
		MachineID: "m1",
		Cols:      0,
		Rows:      -1,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
}

func TestOpen_GatewayError_DoesNotPersist(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{openErr: errors.New("agent refused")}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger())

	_, err := uc.Open(context.Background(), sessions.OpenInput{MachineID: "m1"})
	if err == nil {
		t.Fatal("Open should return error when gateway fails")
	}
	if len(store.created) != 0 {
		t.Errorf("store.Create should not be called on gateway error; called %d times", len(store.created))
	}
}

func TestMarkExited_SetsExited(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger())

	s, err := uc.Open(context.Background(), sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	clk.ts = 2000
	if err := uc.MarkExited(context.Background(), s.ID(), 0); err != nil {
		t.Fatalf("MarkExited: %v", err)
	}

	got, err := store.ByID(context.Background(), s.ID())
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Status() != session.StatusExited {
		t.Errorf("Status: got %q, want exited", got.Status())
	}
	if got.ExitCode() != 0 {
		t.Errorf("ExitCode: got %d, want 0", got.ExitCode())
	}
}

func TestClose_CallsGateway(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger())

	s, err := uc.Open(context.Background(), sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := uc.Close(context.Background(), s.ID()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(gw.closeCalls) != 1 {
		t.Errorf("gateway.CloseSession called %d times, want 1", len(gw.closeCalls))
	}
	if gw.closeCalls[0] != s.ID() {
		t.Errorf("CloseSession sessionID: got %q, want %q", gw.closeCalls[0], s.ID())
	}
}

func TestClose_NotFound(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger())

	err := uc.Close(context.Background(), "no-such-id")
	if !errors.Is(err, session.ErrNotFound) {
		t.Errorf("Close missing: got %v, want session.ErrNotFound", err)
	}
}

func TestRename_Found(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger())

	s, err := uc.Open(context.Background(), sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := uc.Rename(context.Background(), s.ID(), "new-title"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	got, err := store.ByID(context.Background(), s.ID())
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Title() != "new-title" {
		t.Errorf("Title: got %q, want new-title", got.Title())
	}
}

func TestRename_NotFound(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger())

	err := uc.Rename(context.Background(), "no-such-id", "title")
	if !errors.Is(err, session.ErrNotFound) {
		t.Errorf("Rename missing: got %v, want session.ErrNotFound", err)
	}
}

func TestMarkMachineSessionsLost(t *testing.T) {
	store := newFakeSessionStore()
	gw := &fakeGateway{pidReturn: 1}
	clk := &fixedClock{ts: 1000}
	uc := sessions.New(store, gw, clk, nextID(), discardLogger())
	ctx := context.Background()

	// Open a running session.
	s, err := uc.Open(ctx, sessions.OpenInput{MachineID: "m1"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Seed a pre-exited session manually into the store.
	exitedID := "exited-session"
	es := session.New(exitedID, "m1", "", "", "", 1000)
	es.SetExited(0, 1000)
	store.data[exitedID] = es

	clk.ts = 2000
	if err := uc.MarkMachineSessionsLost(ctx, "m1"); err != nil {
		t.Fatalf("MarkMachineSessionsLost: %v", err)
	}

	gotRun, err := store.ByID(ctx, s.ID())
	if err != nil {
		t.Fatalf("ByID running: %v", err)
	}
	if gotRun.Status() != session.StatusLost {
		t.Errorf("running session: got status %q, want lost", gotRun.Status())
	}

	gotExit, err := store.ByID(ctx, exitedID)
	if err != nil {
		t.Fatalf("ByID exited: %v", err)
	}
	if gotExit.Status() != session.StatusExited {
		t.Errorf("exited session: must remain exited, got %q", gotExit.Status())
	}
}
