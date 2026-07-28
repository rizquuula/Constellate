package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/sqlite"
)

func openTestOperatorSessionDB(t *testing.T) *sqlite.OperatorSessionStore {
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
	return sqlite.NewOperatorSessionStore(db)
}

func TestOperatorSessionStore_CreateAndValidate(t *testing.T) {
	store := openTestOperatorSessionDB(t)
	ctx := context.Background()

	if err := store.Create(ctx, "sess1", 1000, 9000); err != nil {
		t.Fatalf("Create: %v", err)
	}

	ok, expiresAt, err := store.Validate(ctx, "sess1", 1001)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !ok {
		t.Error("expected valid session")
	}
	if expiresAt != 9000 {
		t.Errorf("expiresAt: got %d, want 9000", expiresAt)
	}
}

func TestOperatorSessionStore_Validate_Expired(t *testing.T) {
	store := openTestOperatorSessionDB(t)
	ctx := context.Background()

	if err := store.Create(ctx, "sess2", 1000, 2000); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// now >= expires_at → invalid
	ok, _, err := store.Validate(ctx, "sess2", 2000)
	if err != nil {
		t.Fatalf("Validate expired: %v", err)
	}
	if ok {
		t.Error("expected expired session to be invalid")
	}
}

func TestOperatorSessionStore_Validate_Missing(t *testing.T) {
	store := openTestOperatorSessionDB(t)
	ctx := context.Background()

	ok, _, err := store.Validate(ctx, "no-such-session", 1000)
	if err != nil {
		t.Fatalf("Validate missing: %v", err)
	}
	if ok {
		t.Error("expected missing session to be invalid")
	}
}

func TestOperatorSessionStore_Delete(t *testing.T) {
	store := openTestOperatorSessionDB(t)
	ctx := context.Background()

	if err := store.Create(ctx, "sess3", 1000, 9000); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Delete(ctx, "sess3"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	ok, _, err := store.Validate(ctx, "sess3", 1001)
	if err != nil {
		t.Fatalf("Validate after delete: %v", err)
	}
	if ok {
		t.Error("expected session to be gone after delete")
	}
}

func TestOperatorSessionStore_Refresh_MovesExpiry(t *testing.T) {
	store := openTestOperatorSessionDB(t)
	ctx := context.Background()

	if err := store.Create(ctx, "sess4", 1000, 2000); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Refresh(ctx, "sess4", 9000); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// The session was about to expire at 2000; it must now survive past it.
	ok, expiresAt, err := store.Validate(ctx, "sess4", 2500)
	if err != nil {
		t.Fatalf("Validate after refresh: %v", err)
	}
	if !ok {
		t.Error("expected refreshed session to still be valid")
	}
	if expiresAt != 9000 {
		t.Errorf("expiresAt: got %d, want 9000", expiresAt)
	}
}

func TestOperatorSessionStore_Refresh_UnknownID_NotAnError(t *testing.T) {
	store := openTestOperatorSessionDB(t)

	if err := store.Refresh(context.Background(), "no-such-session", 9000); err != nil {
		t.Errorf("Refresh on unknown id: got %v, want nil", err)
	}
}
