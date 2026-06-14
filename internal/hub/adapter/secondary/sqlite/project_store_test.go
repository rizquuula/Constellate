package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/sqlite"
	"github.com/rizquuula/Constellate/internal/hub/domain/project"
)

// openTestFullDB opens a fresh DB, migrates it, and returns all three stores
// backed by the same *sql.DB. Use this when a test needs more than one store.
func openTestFullDB(t *testing.T) (*sqlite.MachineStore, *sqlite.ProjectStore, *sqlite.SessionStore) {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "full_test.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlite.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return sqlite.NewMachineStore(db), sqlite.NewProjectStore(db), sqlite.NewSessionStore(db)
}

func insertTestProject(t *testing.T, ps *sqlite.ProjectStore, id, machineID, path string) {
	t.Helper()
	p := project.Rehydrate(id, machineID, "test-project-"+id, path, "", 1000)
	if err := ps.Create(context.Background(), p); err != nil {
		t.Fatalf("insertTestProject %q: %v", id, err)
	}
}

// --- Project store tests ---

func TestProjectStore_CreateAndByID(t *testing.T) {
	ms, ps, _ := openTestFullDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")

	p := project.Rehydrate("p1", "m1", "myproject", "/work", "#ff0000", 1000)
	if err := ps.Create(ctx, p); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := ps.ByID(ctx, "p1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.ID() != "p1" {
		t.Errorf("ID: got %q, want p1", got.ID())
	}
	if got.MachineID() != "m1" {
		t.Errorf("MachineID: got %q, want m1", got.MachineID())
	}
	if got.Name() != "myproject" {
		t.Errorf("Name: got %q, want myproject", got.Name())
	}
	if got.Path() != "/work" {
		t.Errorf("Path: got %q, want /work", got.Path())
	}
	if got.Color() != "#ff0000" {
		t.Errorf("Color: got %q, want #ff0000", got.Color())
	}
	if got.CreatedAt() != 1000 {
		t.Errorf("CreatedAt: got %d, want 1000", got.CreatedAt())
	}
}

func TestProjectStore_DuplicatePath(t *testing.T) {
	ms, ps, _ := openTestFullDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")

	p1 := project.Rehydrate("p1", "m1", "proj1", "/work", "", 1000)
	if err := ps.Create(ctx, p1); err != nil {
		t.Fatalf("Create p1: %v", err)
	}

	p2 := project.Rehydrate("p2", "m1", "proj2", "/work", "", 1001)
	err := ps.Create(ctx, p2)
	if !errors.Is(err, project.ErrDuplicatePath) {
		t.Errorf("duplicate path: got %v, want project.ErrDuplicatePath", err)
	}
}

func TestProjectStore_ListByMachine(t *testing.T) {
	ms, ps, _ := openTestFullDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")
	insertMachine(t, ms, "m2")

	if err := ps.Create(ctx, project.Rehydrate("p1", "m1", "a", "/a", "", 1000)); err != nil {
		t.Fatalf("Create p1: %v", err)
	}
	if err := ps.Create(ctx, project.Rehydrate("p2", "m2", "b", "/b", "", 1001)); err != nil {
		t.Fatalf("Create p2: %v", err)
	}

	list, err := ps.ListByMachine(ctx, "m1")
	if err != nil {
		t.Fatalf("ListByMachine: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListByMachine m1: got %d, want 1", len(list))
	}
	if list[0].ID() != "p1" {
		t.Errorf("ListByMachine: got %q, want p1", list[0].ID())
	}
}

func TestProjectStore_ByID_NotFound(t *testing.T) {
	_, ps, _ := openTestFullDB(t)
	_, err := ps.ByID(context.Background(), "no-such-id")
	if !errors.Is(err, project.ErrNotFound) {
		t.Errorf("ByID missing: got %v, want project.ErrNotFound", err)
	}
}

func TestProjectStore_InvalidMachineID(t *testing.T) {
	_, ps, _ := openTestFullDB(t)
	ctx := context.Background()

	// No machine inserted — FK to machines(id) must fire → ErrInvalid (not ErrDuplicatePath).
	p := project.Rehydrate("p-bad", "nonexistent-machine", "proj", "/work", "", 1000)
	err := ps.Create(ctx, p)
	if !errors.Is(err, project.ErrInvalid) {
		t.Errorf("bad machineID: got %v, want project.ErrInvalid", err)
	}
}
