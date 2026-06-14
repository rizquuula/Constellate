package audit_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	auditapp "github.com/rizquuula/Constellate/internal/hub/app/audit"
	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
)

// --- fakes ---

type fakeAuditStore struct {
	appended []audit.Event
	err      error
}

func (s *fakeAuditStore) Append(_ context.Context, e audit.Event) error {
	if s.err != nil {
		return s.err
	}
	s.appended = append(s.appended, e)
	return nil
}

type fixedClock struct{ ts int64 }

func (c *fixedClock) Now() int64 { return c.ts }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))
}

// --- tests ---

func TestRecord_StampsTimestampAndActor(t *testing.T) {
	store := &fakeAuditStore{}
	clk := &fixedClock{ts: 1234}
	uc := auditapp.New(store, clk, discardLogger())

	if err := uc.Record(context.Background(), audit.ActionOpen, "m1", "s1", ""); err != nil {
		t.Fatalf("Record: %v", err)
	}

	if len(store.appended) != 1 {
		t.Fatalf("appended count: got %d, want 1", len(store.appended))
	}
	e := store.appended[0]
	if e.TS() != 1234 {
		t.Errorf("TS: got %d, want 1234", e.TS())
	}
	if e.Actor() != "operator" {
		t.Errorf("Actor (default): got %q, want %q", e.Actor(), "operator")
	}
	if e.Action() != audit.ActionOpen {
		t.Errorf("Action: got %q, want %q", e.Action(), audit.ActionOpen)
	}
	if e.MachineID() != "m1" {
		t.Errorf("MachineID: got %q, want m1", e.MachineID())
	}
	if e.SessionID() != "s1" {
		t.Errorf("SessionID: got %q, want s1", e.SessionID())
	}
}

func TestRecord_InjectedActor(t *testing.T) {
	store := &fakeAuditStore{}
	clk := &fixedClock{ts: 5000}
	uc := auditapp.New(store, clk, discardLogger())

	ctx := audit.ContextWithActor(context.Background(), "alice")
	if err := uc.Record(ctx, audit.ActionLogin, "", "", ""); err != nil {
		t.Fatalf("Record: %v", err)
	}

	if len(store.appended) != 1 {
		t.Fatalf("appended count: got %d, want 1", len(store.appended))
	}
	if got := store.appended[0].Actor(); got != "alice" {
		t.Errorf("Actor: got %q, want alice", got)
	}
}

func TestRecord_StoreError_IsReturned(t *testing.T) {
	storeErr := errors.New("store unavailable")
	store := &fakeAuditStore{err: storeErr}
	clk := &fixedClock{ts: 1}
	uc := auditapp.New(store, clk, discardLogger())

	err := uc.Record(context.Background(), audit.ActionAttach, "m1", "s1", "")
	if !errors.Is(err, storeErr) {
		t.Errorf("Record error: got %v, want wrapping storeErr", err)
	}
}
