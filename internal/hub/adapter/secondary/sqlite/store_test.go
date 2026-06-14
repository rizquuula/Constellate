package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/sqlite"
	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
)

func openTestDB(t *testing.T) *sqlite.MachineStore {
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
	return sqlite.NewMachineStore(db)
}

func TestMachineStore_UpsertAndByID(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	m := machine.New("id1", "inst1", "box1", "linux", "amd64", "0.1.0", 1000)
	if err := store.Upsert(ctx, m); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.ByID(ctx, "id1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.ID() != "id1" {
		t.Errorf("ID: got %q", got.ID())
	}
	if got.InstanceID() != "inst1" {
		t.Errorf("InstanceID: got %q, want inst1", got.InstanceID())
	}
	if got.Name() != "box1" {
		t.Errorf("Name: got %q", got.Name())
	}
	if got.OS() != "linux" {
		t.Errorf("OS: got %q", got.OS())
	}
	if got.Arch() != "amd64" {
		t.Errorf("Arch: got %q", got.Arch())
	}
	if got.AgentVersion() != "0.1.0" {
		t.Errorf("AgentVersion: got %q", got.AgentVersion())
	}
	if got.EnrolledAt() != 1000 {
		t.Errorf("EnrolledAt: got %d", got.EnrolledAt())
	}
}

func TestMachineStore_List(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	if err := store.Upsert(ctx, machine.New("id1", "", "box1", "linux", "amd64", "0.1", 1000)); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List: got %d, want 1", len(list))
	}
}

func TestMachineStore_UpdateLastSeen(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	if err := store.Upsert(ctx, machine.New("id1", "", "box1", "linux", "amd64", "0.1", 1000)); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := store.UpdateLastSeen(ctx, "id1", 9999); err != nil {
		t.Fatalf("UpdateLastSeen: %v", err)
	}

	got, err := store.ByID(ctx, "id1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.LastSeenAt() != 9999 {
		t.Errorf("LastSeenAt: got %d, want 9999", got.LastSeenAt())
	}
}

func TestMachineStore_ReUpsert_PreservesEnrolledAt(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	if err := store.Upsert(ctx, machine.New("id1", "inst-A", "box1", "linux", "amd64", "0.1", 1000)); err != nil {
		t.Fatalf("first Upsert: %v", err)
	}

	// Re-upsert with a different enrolled_at value; the store must ignore it.
	m2 := machine.New("id1", "inst-B", "box1-new", "linux", "arm64", "0.2", 9999)
	if err := store.Upsert(ctx, m2); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}

	got, err := store.ByID(ctx, "id1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.EnrolledAt() != 1000 {
		t.Errorf("re-upsert must preserve enrolled_at: got %d, want 1000", got.EnrolledAt())
	}
	if got.Name() != "box1-new" {
		t.Errorf("re-upsert should update name: got %q", got.Name())
	}
}

func TestMachineStore_ByID_NotFound(t *testing.T) {
	store := openTestDB(t)
	_, err := store.ByID(context.Background(), "no-such-id")
	if !errors.Is(err, machine.ErrNotFound) {
		t.Errorf("ByID missing: got %v, want machine.ErrNotFound", err)
	}
}

func TestMachineStore_InstanceID_RoundTrip(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	if err := store.Upsert(ctx, machine.New("id1", "inst-X", "box1", "linux", "amd64", "0.1", 1000)); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.ByID(ctx, "id1")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if got.InstanceID() != "inst-X" {
		t.Errorf("InstanceID: got %q, want inst-X", got.InstanceID())
	}

	// Re-upsert with a new instance_id; it must be updated.
	if err := store.Upsert(ctx, machine.New("id1", "inst-Y", "box1", "linux", "amd64", "0.1", 1000)); err != nil {
		t.Fatalf("re-Upsert: %v", err)
	}
	got2, err := store.ByID(ctx, "id1")
	if err != nil {
		t.Fatalf("ByID after re-Upsert: %v", err)
	}
	if got2.InstanceID() != "inst-Y" {
		t.Errorf("InstanceID after re-Upsert: got %q, want inst-Y", got2.InstanceID())
	}
}

func TestMachineStore_MigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}
