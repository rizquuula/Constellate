package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/sqlite"
)

func openTestOperatorDB(t *testing.T) *sqlite.OperatorStore {
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
	n := 0
	return sqlite.NewOperatorStore(db, func() string {
		n++
		return "id-" + string(rune('A'+n))
	})
}

func TestOperatorStore_HasOperator_Empty(t *testing.T) {
	store := openTestOperatorDB(t)
	ctx := context.Background()

	has, err := store.HasOperator(ctx)
	if err != nil {
		t.Fatalf("HasOperator: %v", err)
	}
	if has {
		t.Error("expected no operator on empty db")
	}
}

func TestOperatorStore_SaveTOTP_AndTOTPSecret(t *testing.T) {
	store := openTestOperatorDB(t)
	ctx := context.Background()

	if err := store.SaveTOTP(ctx, "MYSECRET", 1000); err != nil {
		t.Fatalf("SaveTOTP: %v", err)
	}

	has, err := store.HasOperator(ctx)
	if err != nil {
		t.Fatalf("HasOperator: %v", err)
	}
	if !has {
		t.Error("expected operator after SaveTOTP")
	}

	secret, ok, err := store.TOTPSecret(ctx)
	if err != nil {
		t.Fatalf("TOTPSecret: %v", err)
	}
	if !ok {
		t.Error("expected secret to be present")
	}
	if secret != "MYSECRET" {
		t.Errorf("TOTPSecret: got %q, want MYSECRET", secret)
	}
}

func TestOperatorStore_SaveRecoveryCodes_ConsumeOnce(t *testing.T) {
	store := openTestOperatorDB(t)
	ctx := context.Background()

	hashes := []string{"hash1", "hash2", "hash3"}
	if err := store.SaveRecoveryCodes(ctx, hashes, 1000); err != nil {
		t.Fatalf("SaveRecoveryCodes: %v", err)
	}

	// First consume should succeed
	ok, err := store.ConsumeRecoveryCode(ctx, "hash1")
	if err != nil {
		t.Fatalf("ConsumeRecoveryCode first: %v", err)
	}
	if !ok {
		t.Error("expected consume to return true first time")
	}

	// Second consume of same code should fail
	ok2, err2 := store.ConsumeRecoveryCode(ctx, "hash1")
	if err2 != nil {
		t.Fatalf("ConsumeRecoveryCode second: %v", err2)
	}
	if ok2 {
		t.Error("expected consume to return false second time")
	}
}

func TestOperatorStore_ConsumeRecoveryCode_Missing(t *testing.T) {
	store := openTestOperatorDB(t)
	ctx := context.Background()

	ok, err := store.ConsumeRecoveryCode(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("ConsumeRecoveryCode missing: %v", err)
	}
	if ok {
		t.Error("expected false for missing hash")
	}
}
