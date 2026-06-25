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
	s := session.New("s1", "m1", "", "my title", "/bin/bash", "", 1000)
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

	if err := ss.Create(ctx, session.New("s1", "m1", "", "", "", "", 1000)); err != nil {
		t.Fatalf("Create s1: %v", err)
	}
	if err := ss.Create(ctx, session.New("s2", "m1", "", "", "", "", 1001)); err != nil {
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

	if err := ss.Create(ctx, session.New("s1", "m1", "", "", "", "", 1000)); err != nil {
		t.Fatalf("Create s1: %v", err)
	}
	if err := ss.Create(ctx, session.New("s2", "m2", "", "", "", "", 1001)); err != nil {
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

	s := session.New("s1", "m1", "", "", "", "", 1000)
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
	s := session.New("s1", "m1", "", "", "", "", 1000)
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

	s := session.New("s1", "m1", "", "old title", "/bin/bash", "", 1000)
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
	sNull := session.New("s-null", "m1", "", "", "", "", 1000)
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
	sProj := session.New("s-proj", "m1", "p1", "", "", "", 1000)
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

func TestSessionStore_SetActivity_NoLastActiveAt(t *testing.T) {
	ms, ss := openTestSessionDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")

	s := session.New("s1", "m1", "", "", "", "", 1000)
	if err := ss.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	origLAT := s.LastActiveAt()

	// SetActivity with lastActiveAt=0 must NOT update last_active_at.
	if err := ss.SetActivity(ctx, "s1", session.ActivityAwaitingInput, 0); err != nil {
		t.Fatalf("SetActivity: %v", err)
	}

	got, err := ss.ByID(ctx, "s1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Activity() != session.ActivityAwaitingInput {
		t.Errorf("Activity: got %q, want %q", got.Activity(), session.ActivityAwaitingInput)
	}
	if got.LastActiveAt() != origLAT {
		t.Errorf("LastActiveAt must not change when lastActiveAt=0: got %d, want %d", got.LastActiveAt(), origLAT)
	}
}

func TestSessionStore_SetActivity_WithLastActiveAt(t *testing.T) {
	ms, ss := openTestSessionDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")

	s := session.New("s1", "m1", "", "", "", "", 1000)
	if err := ss.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	now := int64(9999)
	if err := ss.SetActivity(ctx, "s1", session.ActivityActive, now); err != nil {
		t.Fatalf("SetActivity: %v", err)
	}

	got, err := ss.ByID(ctx, "s1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Activity() != session.ActivityActive {
		t.Errorf("Activity: got %q, want %q", got.Activity(), session.ActivityActive)
	}
	if got.LastActiveAt() != now {
		t.Errorf("LastActiveAt: got %d, want %d", got.LastActiveAt(), now)
	}
}

func TestSessionStore_SetActivity_NotFound(t *testing.T) {
	_, ss := openTestSessionDB(t)
	err := ss.SetActivity(context.Background(), "no-such-id", session.ActivityIdle, 0)
	if !errors.Is(err, session.ErrNotFound) {
		t.Errorf("SetActivity missing: got %v, want session.ErrNotFound", err)
	}
}

func TestSessionStore_MarkRunningLost(t *testing.T) {
	ms, ss := openTestSessionDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")

	// Seed: one running session and one already-exited session.
	running := session.New("s-run", "m1", "", "", "", "", 1000)
	if err := ss.Create(ctx, running); err != nil {
		t.Fatalf("Create running: %v", err)
	}
	exited := session.New("s-exit", "m1", "", "", "", "", 1000)
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

func TestSessionStore_SetAutoRelaunch(t *testing.T) {
	ms, ss := openTestSessionDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")

	s := session.New("s1", "m1", "", "", "", "/work", 1000)
	if err := ss.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Default should be false.
	got, err := ss.ByID(ctx, "s1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.AutoRelaunch() {
		t.Errorf("AutoRelaunch default: want false, got true")
	}

	// Enable auto_relaunch.
	if err := ss.SetAutoRelaunch(ctx, "s1", true); err != nil {
		t.Fatalf("SetAutoRelaunch true: %v", err)
	}
	got, err = ss.ByID(ctx, "s1")
	if err != nil {
		t.Fatalf("ByID after enable: %v", err)
	}
	if !got.AutoRelaunch() {
		t.Errorf("AutoRelaunch after enable: want true, got false")
	}
	if got.Cwd() != "/work" {
		t.Errorf("Cwd: got %q, want /work", got.Cwd())
	}

	// Disable again.
	if err := ss.SetAutoRelaunch(ctx, "s1", false); err != nil {
		t.Fatalf("SetAutoRelaunch false: %v", err)
	}
	got, err = ss.ByID(ctx, "s1")
	if err != nil {
		t.Fatalf("ByID after disable: %v", err)
	}
	if got.AutoRelaunch() {
		t.Errorf("AutoRelaunch after disable: want false, got true")
	}
}

func TestSessionStore_AutoRelaunchSessions(t *testing.T) {
	ms, ss := openTestSessionDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")

	s1 := session.New("s1", "m1", "", "", "/bin/bash", "/home/user", 1000)
	s2 := session.New("s2", "m1", "", "", "/bin/sh", "/tmp", 1000)
	s3 := session.New("s3", "m1", "", "", "", "", 1000)

	for _, s := range []session.Session{s1, s2, s3} {
		if err := ss.Create(ctx, s); err != nil {
			t.Fatalf("Create %s: %v", s.ID(), err)
		}
	}

	// Enable auto_relaunch on s1 and s2 only.
	if err := ss.SetAutoRelaunch(ctx, "s1", true); err != nil {
		t.Fatalf("SetAutoRelaunch s1: %v", err)
	}
	if err := ss.SetAutoRelaunch(ctx, "s2", true); err != nil {
		t.Fatalf("SetAutoRelaunch s2: %v", err)
	}

	// Mark s3 as exited (it's not running, so shouldn't appear even if auto_relaunch were set).
	if err := ss.SetExited(ctx, "s3", 0, 1500); err != nil {
		t.Fatalf("SetExited s3: %v", err)
	}

	revivals, err := ss.AutoRelaunchSessions(ctx, "m1")
	if err != nil {
		t.Fatalf("AutoRelaunchSessions: %v", err)
	}
	if len(revivals) != 2 {
		t.Fatalf("AutoRelaunchSessions: got %d sessions, want 2", len(revivals))
	}
	// Both should have auto_relaunch=true and status=running.
	for _, r := range revivals {
		if !r.AutoRelaunch() {
			t.Errorf("session %q: AutoRelaunch want true, got false", r.ID())
		}
		if r.Status() != session.StatusRunning {
			t.Errorf("session %q: Status want running, got %q", r.ID(), r.Status())
		}
	}
}

func TestSessionStore_SetRunning(t *testing.T) {
	ms, ss := openTestSessionDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")

	s := session.New("s1", "m1", "", "", "", "", 1000)
	if err := ss.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := ss.SetExited(ctx, "s1", 0, 1500); err != nil {
		t.Fatalf("SetExited: %v", err)
	}

	if err := ss.SetRunning(ctx, "s1"); err != nil {
		t.Fatalf("SetRunning: %v", err)
	}

	got, err := ss.ByID(ctx, "s1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Status() != session.StatusRunning {
		t.Errorf("Status after SetRunning: got %q, want running", got.Status())
	}
}

func TestSessionStore_SetLost(t *testing.T) {
	ms, ss := openTestSessionDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")

	s := session.New("s1", "m1", "", "", "", "", 1000)
	if err := ss.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ss.SetLost(ctx, "s1", 2000); err != nil {
		t.Fatalf("SetLost: %v", err)
	}

	got, err := ss.ByID(ctx, "s1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.Status() != session.StatusLost {
		t.Errorf("Status after SetLost: got %q, want lost", got.Status())
	}
	if got.LastActiveAt() != 2000 {
		t.Errorf("LastActiveAt after SetLost: got %d, want 2000", got.LastActiveAt())
	}
}

func TestSessionStore_MarkRunningLost_SkipsAutoRelaunch(t *testing.T) {
	ms, ss := openTestSessionDB(t)
	ctx := context.Background()

	insertMachine(t, ms, "m1")

	// auto_relaunch=false: should be marked lost.
	s1 := session.New("s1", "m1", "", "", "", "", 1000)
	if err := ss.Create(ctx, s1); err != nil {
		t.Fatalf("Create s1: %v", err)
	}

	// auto_relaunch=true: must be preserved (not marked lost).
	s2 := session.New("s2", "m1", "", "", "", "/home/user", 1000)
	if err := ss.Create(ctx, s2); err != nil {
		t.Fatalf("Create s2: %v", err)
	}
	if err := ss.SetAutoRelaunch(ctx, "s2", true); err != nil {
		t.Fatalf("SetAutoRelaunch s2: %v", err)
	}

	if err := ss.MarkRunningLost(ctx, "m1", 2000); err != nil {
		t.Fatalf("MarkRunningLost: %v", err)
	}

	got1, err := ss.ByID(ctx, "s1")
	if err != nil {
		t.Fatalf("ByID s1: %v", err)
	}
	if got1.Status() != session.StatusLost {
		t.Errorf("s1 (auto_relaunch=false): got status %q, want lost", got1.Status())
	}

	got2, err := ss.ByID(ctx, "s2")
	if err != nil {
		t.Fatalf("ByID s2: %v", err)
	}
	if got2.Status() != session.StatusRunning {
		t.Errorf("s2 (auto_relaunch=true): got status %q, want running (must not be marked lost)", got2.Status())
	}
}
