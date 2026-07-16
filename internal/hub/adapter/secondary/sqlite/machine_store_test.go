package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/sqlite"
	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
	"github.com/rizquuula/Constellate/internal/hub/domain/project"
	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

// openTestCascadeDB opens a fresh migrated DB and returns the raw handle plus
// every store needed to seed a machine with dependents. Row-count assertions run
// against the raw *sql.DB.
func openTestCascadeDB(t *testing.T) (*sql.DB, *sqlite.MachineStore, *sqlite.CredentialStore, *sqlite.ProjectStore, *sqlite.SessionStore) {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "cascade_test.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlite.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db, sqlite.NewMachineStore(db), sqlite.NewCredentialStore(db),
		sqlite.NewProjectStore(db), sqlite.NewSessionStore(db)
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return n
}

func TestMachineStore_Delete_CascadesAllReferences(t *testing.T) {
	db, ms, cs, ps, ss := openTestCascadeDB(t)
	ctx := context.Background()

	// Seed a machine with a credential, a project, and a session referencing it.
	if err := ms.Upsert(ctx, machine.New("m1", "inst1", "box1", "linux", "amd64", "0.1", 1000)); err != nil {
		t.Fatalf("Upsert machine: %v", err)
	}
	if err := cs.Save(ctx, "m1", []byte("pubkey"), 1000); err != nil {
		t.Fatalf("Save credential: %v", err)
	}
	if err := ps.Create(ctx, project.Rehydrate("p1", "m1", "proj", "/work", "", 1000)); err != nil {
		t.Fatalf("Create project: %v", err)
	}
	if err := ss.Create(ctx, session.New("s1", "m1", "p1", "title", "/bin/bash", "", 1000)); err != nil {
		t.Fatalf("Create session: %v", err)
	}

	// A machine must be revoked before it can be deleted at the domain level, but
	// the store-level Delete is unconditional; revoke first to mirror real usage.
	if err := ms.MarkRevoked(ctx, "m1", 2000); err != nil {
		t.Fatalf("MarkRevoked: %v", err)
	}

	if err := ms.Delete(ctx, "m1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// No rows may remain for m1 in any of the four tables.
	if n := countRows(t, db, `SELECT COUNT(*) FROM machines WHERE id = ?`, "m1"); n != 0 {
		t.Errorf("machines rows: got %d, want 0", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM machine_credentials WHERE machine_id = ?`, "m1"); n != 0 {
		t.Errorf("machine_credentials rows: got %d, want 0", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM projects WHERE machine_id = ?`, "m1"); n != 0 {
		t.Errorf("projects rows: got %d, want 0", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM sessions WHERE machine_id = ?`, "m1"); n != 0 {
		t.Errorf("sessions rows: got %d, want 0", n)
	}
}

func TestMachineStore_Delete_UnknownID(t *testing.T) {
	_, ms, _, _, _ := openTestCascadeDB(t)
	if err := ms.Delete(context.Background(), "no-such-id"); !errors.Is(err, machine.ErrNotFound) {
		t.Errorf("Delete unknown: got %v, want machine.ErrNotFound", err)
	}
}

func TestMachineStore_ClearRevoked_SetThenClear(t *testing.T) {
	_, ms, _, _, _ := openTestCascadeDB(t)
	ctx := context.Background()

	if err := ms.Upsert(ctx, machine.New("m1", "", "box1", "linux", "amd64", "0.1", 1000)); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := ms.MarkRevoked(ctx, "m1", 2000); err != nil {
		t.Fatalf("MarkRevoked: %v", err)
	}
	got, err := ms.ByID(ctx, "m1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if !got.Revoked() {
		t.Fatal("expected machine to be revoked after MarkRevoked")
	}

	if err := ms.ClearRevoked(ctx, "m1"); err != nil {
		t.Fatalf("ClearRevoked: %v", err)
	}
	got, err = ms.ByID(ctx, "m1")
	if err != nil {
		t.Fatalf("ByID after clear: %v", err)
	}
	if got.Revoked() {
		t.Error("expected machine to be un-revoked after ClearRevoked")
	}
}

func TestMachineStore_ClearRevoked_UnknownID(t *testing.T) {
	_, ms, _, _, _ := openTestCascadeDB(t)
	if err := ms.ClearRevoked(context.Background(), "no-such-id"); !errors.Is(err, machine.ErrNotFound) {
		t.Errorf("ClearRevoked unknown: got %v, want machine.ErrNotFound", err)
	}
}
