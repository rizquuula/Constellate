package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/sqlite"
	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

func openTestSessionDB(t *testing.T) (*sqlite.MachineStore, *sqlite.SessionStore) {
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
	return sqlite.NewMachineStore(db), sqlite.NewSessionStore(db)
}

func insertMachine(t *testing.T, ms *sqlite.MachineStore, id string) {
	t.Helper()
	m := machine.New(id, "", "box", "linux", "amd64", "0.1", 1000)
	if err := ms.Upsert(context.Background(), m); err != nil {
		t.Fatalf("insert machine: %v", err)
	}
}

func TestSessionStore_CreateAndByID(t *testing.T) {
	ms, ss := openTestSessionDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")

	// project_id is nullable and REFERENCES projects(id); pass empty to store as NULL.
	s := session.New("s1", "m1", "", "my title", "/bin/bash", 1000)
	if err := ss.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := ss.ByID(ctx, "s1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.ID() != "s1" {
		t.Errorf("ID: got %q, want s1", got.ID())
	}
	if got.MachineID() != "m1" {
		t.Errorf("MachineID: got %q, want m1", got.MachineID())
	}
	if got.Title() != "my title" {
		t.Errorf("Title: got %q, want 'my title'", got.Title())
	}
	if got.Shell() != "/bin/bash" {
		t.Errorf("Shell: got %q, want /bin/bash", got.Shell())
	}
	if got.Status() != session.StatusRunning {
		t.Errorf("Status: got %q, want running", got.Status())
	}
	if got.CreatedAt() != 1000 {
		t.Errorf("CreatedAt: got %d, want 1000", got.CreatedAt())
	}
}

func TestSessionStore_List(t *testing.T) {
	ms, ss := openTestSessionDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")

	if err := ss.Create(ctx, session.New("s1", "m1", "", "", "", 1000)); err != nil {
		t.Fatalf("Create s1: %v", err)
	}
	if err := ss.Create(ctx, session.New("s2", "m1", "", "", "", 1001)); err != nil {
		t.Fatalf("Create s2: %v", err)
	}

	list, err := ss.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("List: got %d, want 2", len(list))
	}
}

func TestSessionStore_ListByMachine(t *testing.T) {
	ms, ss := openTestSessionDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")
	insertMachine(t, ms, "m2")

	if err := ss.Create(ctx, session.New("s1", "m1", "", "", "", 1000)); err != nil {
		t.Fatalf("Create s1: %v", err)
	}
	if err := ss.Create(ctx, session.New("s2", "m2", "", "", "", 1001)); err != nil {
		t.Fatalf("Create s2: %v", err)
	}

	list, err := ss.ListByMachine(ctx, "m1")
	if err != nil {
		t.Fatalf("ListByMachine: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListByMachine: got %d, want 1", len(list))
	}
	if list[0].ID() != "s1" {
		t.Errorf("ListByMachine: got session %q, want s1", list[0].ID())
	}
}

func TestSessionStore_SetExited(t *testing.T) {
	ms, ss := openTestSessionDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")

	s := session.New("s1", "m1", "", "", "", 1000)
	if err := ss.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ss.SetExited(ctx, "s1", 42, 2000); err != nil {
		t.Fatalf("SetExited: %v", err)
	}

	got, err := ss.ByID(ctx, "s1")
	if err != nil {
		t.Fatalf("ByID after SetExited: %v", err)
	}
	if got.Status() != session.StatusExited {
		t.Errorf("Status: got %q, want exited", got.Status())
	}
	if got.ExitCode() != 42 {
		t.Errorf("ExitCode: got %d, want 42", got.ExitCode())
	}
	if got.LastActiveAt() != 2000 {
		t.Errorf("LastActiveAt: got %d, want 2000", got.LastActiveAt())
	}
}

func TestSessionStore_ByID_NotFound(t *testing.T) {
	_, ss := openTestSessionDB(t)
	_, err := ss.ByID(context.Background(), "no-such-id")
	if !errors.Is(err, session.ErrNotFound) {
		t.Errorf("ByID missing: got %v, want session.ErrNotFound", err)
	}
}

func TestSessionStore_NullableFields(t *testing.T) {
	ms, ss := openTestSessionDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")

	// Create with empty optional fields
	s := session.New("s1", "m1", "", "", "", 1000)
	if err := ss.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := ss.ByID(ctx, "s1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.ProjectID() != "" {
		t.Errorf("ProjectID should be empty, got %q", got.ProjectID())
	}
	if got.Title() != "" {
		t.Errorf("Title should be empty, got %q", got.Title())
	}
	if got.Shell() != "" {
		t.Errorf("Shell should be empty, got %q", got.Shell())
	}
}

func TestSessionStore_SetTitle(t *testing.T) {
	ms, ss := openTestSessionDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")

	s := session.New("s1", "m1", "", "old title", "/bin/bash", 1000)
	if err := ss.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ss.SetTitle(ctx, "s1", "new title"); err != nil {
		t.Fatalf("SetTitle: %v", err)
	}

	got, err := ss.ByID(ctx, "s1")
	if err != nil {
		t.Fatalf("ByID after SetTitle: %v", err)
	}
	if got.Title() != "new title" {
		t.Errorf("Title: got %q, want 'new title'", got.Title())
	}
	// last_active_at must NOT be updated by a rename (metadata only).
	if got.LastActiveAt() != s.LastActiveAt() {
		t.Errorf("LastActiveAt must be unchanged after rename: got %d, want %d", got.LastActiveAt(), s.LastActiveAt())
	}
}

func TestSessionStore_SetTitle_NotFound(t *testing.T) {
	_, ss := openTestSessionDB(t)
	err := ss.SetTitle(context.Background(), "no-such-id", "title")
	if !errors.Is(err, session.ErrNotFound) {
		t.Errorf("SetTitle missing: got %v, want session.ErrNotFound", err)
	}
}

func TestSessionStore_ProjectID_NullAndSet(t *testing.T) {
	ms, ps, ss := openTestFullDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")
	insertTestProject(t, ps, "p1", "m1", "/work")

	// Session with NULL project_id.
	sNull := session.New("s-null", "m1", "", "", "", 1000)
	if err := ss.Create(ctx, sNull); err != nil {
		t.Fatalf("Create s-null: %v", err)
	}
	gotNull, err := ss.ByID(ctx, "s-null")
	if err != nil {
		t.Fatalf("ByID s-null: %v", err)
	}
	if gotNull.ProjectID() != "" {
		t.Errorf("s-null ProjectID: got %q, want empty", gotNull.ProjectID())
	}

	// Session with a project_id.
	sProj := session.New("s-proj", "m1", "p1", "", "", 1000)
	if err := ss.Create(ctx, sProj); err != nil {
		t.Fatalf("Create s-proj: %v", err)
	}
	gotProj, err := ss.ByID(ctx, "s-proj")
	if err != nil {
		t.Fatalf("ByID s-proj: %v", err)
	}
	if gotProj.ProjectID() != "p1" {
		t.Errorf("s-proj ProjectID: got %q, want p1", gotProj.ProjectID())
	}
}

func TestSessionStore_MarkRunningLost(t *testing.T) {
	ms, ss := openTestSessionDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")

	// Seed: one running session and one already-exited session.
	running := session.New("s-run", "m1", "", "", "", 1000)
	if err := ss.Create(ctx, running); err != nil {
		t.Fatalf("Create running: %v", err)
	}
	exited := session.New("s-exit", "m1", "", "", "", 1000)
	if err := ss.Create(ctx, exited); err != nil {
		t.Fatalf("Create exited: %v", err)
	}
	if err := ss.SetExited(ctx, "s-exit", 0, 1500); err != nil {
		t.Fatalf("SetExited: %v", err)
	}

	// Mark running sessions lost.
	if err := ss.MarkRunningLost(ctx, "m1", 2000); err != nil {
		t.Fatalf("MarkRunningLost: %v", err)
	}

	gotRun, err := ss.ByID(ctx, "s-run")
	if err != nil {
		t.Fatalf("ByID s-run: %v", err)
	}
	if gotRun.Status() != session.StatusLost {
		t.Errorf("running session: got status %q, want lost", gotRun.Status())
	}
	if gotRun.LastActiveAt() != 2000 {
		t.Errorf("running session: LastActiveAt got %d, want 2000", gotRun.LastActiveAt())
	}

	gotExit, err := ss.ByID(ctx, "s-exit")
	if err != nil {
		t.Fatalf("ByID s-exit: %v", err)
	}
	if gotExit.Status() != session.StatusExited {
		t.Errorf("exited session must remain exited, got %q", gotExit.Status())
	}
}
