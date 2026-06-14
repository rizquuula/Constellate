package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/sqlite"
	"github.com/rizquuula/Constellate/internal/hub/app/enroll"
)

func openTestEnrollTokenDB(t *testing.T) *sqlite.EnrollTokenStore {
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
	return sqlite.NewEnrollTokenStore(db)
}

func TestEnrollTokenStore_CreateAndConsume(t *testing.T) {
	store := openTestEnrollTokenDB(t)
	ctx := context.Background()

	const hash = "abc123hash"
	now := int64(1700000000)
	expiresAt := now + 900

	if err := store.Create(ctx, hash, expiresAt); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Consume(ctx, hash, now); err != nil {
		t.Fatalf("Consume (first): %v", err)
	}
}

func TestEnrollTokenStore_DoubleConsume(t *testing.T) {
	store := openTestEnrollTokenDB(t)
	ctx := context.Background()

	const hash = "double-spend-hash"
	now := int64(1700000000)
	expiresAt := now + 900

	if err := store.Create(ctx, hash, expiresAt); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Consume(ctx, hash, now); err != nil {
		t.Fatalf("First Consume: %v", err)
	}

	err := store.Consume(ctx, hash, now)
	if !errors.Is(err, enroll.ErrInvalidToken) {
		t.Errorf("Double-consume: expected ErrInvalidToken, got %v", err)
	}
}

func TestEnrollTokenStore_Expired(t *testing.T) {
	store := openTestEnrollTokenDB(t)
	ctx := context.Background()

	const hash = "expired-hash"
	now := int64(1700000000)
	expiresAt := now + 900

	if err := store.Create(ctx, hash, expiresAt); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Consume at time after expiry.
	err := store.Consume(ctx, hash, now+901)
	if !errors.Is(err, enroll.ErrInvalidToken) {
		t.Errorf("Expired: expected ErrInvalidToken, got %v", err)
	}
}

func TestEnrollTokenStore_Missing(t *testing.T) {
	store := openTestEnrollTokenDB(t)
	ctx := context.Background()

	err := store.Consume(ctx, "nonexistent-hash", 1700000000)
	if !errors.Is(err, enroll.ErrInvalidToken) {
		t.Errorf("Missing: expected ErrInvalidToken, got %v", err)
	}
}
