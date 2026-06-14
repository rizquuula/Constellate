package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/sqlite"
	"github.com/rizquuula/Constellate/internal/hub/app/enroll"
	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
)

func openTestCredentialDB(t *testing.T) (*sqlite.MachineStore, *sqlite.CredentialStore) {
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
	return sqlite.NewMachineStore(db), sqlite.NewCredentialStore(db)
}

func TestCredentialStore_SaveAndPublicKey(t *testing.T) {
	ms, cs := openTestCredentialDB(t)
	ctx := context.Background()

	// machine_credentials references machines(id), so insert the machine first.
	m := machine.New("m1", "", "box1", "linux", "amd64", "0.1", 1000)
	if err := ms.Upsert(ctx, m); err != nil {
		t.Fatalf("Upsert machine: %v", err)
	}

	pubKey := make([]byte, 32)
	for i := range pubKey {
		pubKey[i] = byte(i + 1)
	}

	if err := cs.Save(ctx, "m1", pubKey, 1000); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := cs.PublicKey(ctx, "m1")
	if err != nil {
		t.Fatalf("PublicKey: %v", err)
	}
	if len(got) != len(pubKey) {
		t.Fatalf("PublicKey length: got %d, want %d", len(got), len(pubKey))
	}
	for i := range pubKey {
		if got[i] != pubKey[i] {
			t.Errorf("PublicKey[%d]: got %d, want %d", i, got[i], pubKey[i])
		}
	}
}

func TestCredentialStore_UnknownMachine(t *testing.T) {
	_, cs := openTestCredentialDB(t)
	ctx := context.Background()

	_, err := cs.PublicKey(ctx, "no-such-machine")
	if !errors.Is(err, enroll.ErrUnknownMachine) {
		t.Errorf("expected ErrUnknownMachine, got %v", err)
	}
}

func TestCredentialStore_Upsert(t *testing.T) {
	ms, cs := openTestCredentialDB(t)
	ctx := context.Background()

	m := machine.New("m2", "", "box2", "linux", "amd64", "0.1", 1000)
	if err := ms.Upsert(ctx, m); err != nil {
		t.Fatalf("Upsert machine: %v", err)
	}

	key1 := make([]byte, 32)
	key1[0] = 0xAA
	if err := cs.Save(ctx, "m2", key1, 1000); err != nil {
		t.Fatalf("Save key1: %v", err)
	}

	// Save again with a different key — should update.
	key2 := make([]byte, 32)
	key2[0] = 0xBB
	if err := cs.Save(ctx, "m2", key2, 2000); err != nil {
		t.Fatalf("Save key2: %v", err)
	}

	got, err := cs.PublicKey(ctx, "m2")
	if err != nil {
		t.Fatalf("PublicKey after upsert: %v", err)
	}
	if got[0] != 0xBB {
		t.Errorf("PublicKey[0] after upsert: got 0x%02X, want 0xBB", got[0])
	}
}
