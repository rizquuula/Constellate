package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/sqlite"
	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
)

func openTestAuditDB(t *testing.T) *sqlite.AuditStore {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := sqlite.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return sqlite.NewAuditStore(db)
}

func TestAuditStore_AppendAndList(t *testing.T) {
	store := openTestAuditDB(t)
	ctx := context.Background()

	e1 := audit.NewEvent(1000, "operator", audit.ActionOpen, "m1", "s1", `{"cwd":"/tmp"}`)
	e2 := audit.NewEvent(2000, "operator", audit.ActionAttach, "m1", "s1", "")

	if err := store.Append(ctx, e1); err != nil {
		t.Fatalf("Append e1: %v", err)
	}
	if err := store.Append(ctx, e2); err != nil {
		t.Fatalf("Append e2: %v", err)
	}

	// List should return newest-first.
	list, err := store.List(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List count: got %d, want 2", len(list))
	}

	// e2 has ts=2000, should be first.
	if list[0].TS() != 2000 {
		t.Errorf("list[0].TS: got %d, want 2000", list[0].TS())
	}
	if list[1].TS() != 1000 {
		t.Errorf("list[1].TS: got %d, want 1000", list[1].TS())
	}
}

func TestAuditStore_FieldFidelity(t *testing.T) {
	store := openTestAuditDB(t)
	ctx := context.Background()

	e := audit.NewEvent(5000, "alice", audit.ActionEnroll, "machX", "sessY", `{"reason":"test"}`)
	if err := store.Append(ctx, e); err != nil {
		t.Fatalf("Append: %v", err)
	}

	list, err := store.List(ctx, 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List count: got %d, want 1", len(list))
	}

	got := list[0]
	if got.TS() != 5000 {
		t.Errorf("TS: got %d, want 5000", got.TS())
	}
	if got.Actor() != "alice" {
		t.Errorf("Actor: got %q, want alice", got.Actor())
	}
	if got.Action() != audit.ActionEnroll {
		t.Errorf("Action: got %q, want enroll", got.Action())
	}
	if got.MachineID() != "machX" {
		t.Errorf("MachineID: got %q, want machX", got.MachineID())
	}
	if got.SessionID() != "sessY" {
		t.Errorf("SessionID: got %q, want sessY", got.SessionID())
	}
	if got.Detail() != `{"reason":"test"}` {
		t.Errorf("Detail: got %q, want {\"reason\":\"test\"}", got.Detail())
	}
}

func TestAuditStore_NullHandling(t *testing.T) {
	store := openTestAuditDB(t)
	ctx := context.Background()

	// Empty machineID, sessionID, and detail should round-trip as empty strings (stored as NULL).
	e := audit.NewEvent(3000, "system", audit.ActionLogin, "", "", "")
	if err := store.Append(ctx, e); err != nil {
		t.Fatalf("Append: %v", err)
	}

	list, err := store.List(ctx, 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List count: got %d, want 1", len(list))
	}

	got := list[0]
	if got.MachineID() != "" {
		t.Errorf("MachineID (NULL): got %q, want empty", got.MachineID())
	}
	if got.SessionID() != "" {
		t.Errorf("SessionID (NULL): got %q, want empty", got.SessionID())
	}
	if got.Detail() != "" {
		t.Errorf("Detail (NULL): got %q, want empty", got.Detail())
	}
}

func TestAuditStore_ListLimit(t *testing.T) {
	store := openTestAuditDB(t)
	ctx := context.Background()

	for i := int64(1); i <= 5; i++ {
		if err := store.Append(ctx, audit.NewEvent(i*1000, "operator", audit.ActionOpen, "", "", "")); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	list, err := store.List(ctx, 3)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("List count with limit 3: got %d, want 3", len(list))
	}
	// Should be the three newest: ts=5000, 4000, 3000.
	if list[0].TS() != 5000 {
		t.Errorf("list[0].TS: got %d, want 5000", list[0].TS())
	}
}
