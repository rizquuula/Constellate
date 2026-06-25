package scrollbackfile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, 0)

	want := []byte("hello scrollback")
	if err := a.Save("sess-01", want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := a.Load("sess-01")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("Load: got %q, want %q", got, want)
	}
}

func TestLoadMissingReturnsNilNil(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, 0)

	data, err := a.Load("no-such-session")
	if err != nil {
		t.Fatalf("Load missing: unexpected error: %v", err)
	}
	if data != nil {
		t.Errorf("Load missing: expected nil, got %q", data)
	}
}

func TestDeleteRemovesFile(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, 0)

	if err := a.Save("sess-del", []byte("data")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := a.Delete("sess-del"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	data, err := a.Load("sess-del")
	if err != nil {
		t.Fatalf("Load after Delete: %v", err)
	}
	if data != nil {
		t.Errorf("Load after Delete: expected nil, got %q", data)
	}
}

func TestDeleteMissingIsNoOp(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, 0)

	if err := a.Delete("nonexistent"); err != nil {
		t.Errorf("Delete nonexistent: unexpected error: %v", err)
	}
}

func TestSaveIsAtomic(t *testing.T) {
	// A crashed write to a temp file must not leave a corrupt target.
	// We verify atomicity by checking that after a successful Save the file
	// is fully readable and correct (no partial state exposed).
	dir := t.TempDir()
	a := New(dir, 0)

	payload := bytes.Repeat([]byte("X"), 4096)
	if err := a.Save("atomic-sess", payload); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := a.Load("atomic-sess")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("atomic write produced wrong result: got %d bytes, want %d", len(got), len(payload))
	}
	// No temp files should remain.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "atomic-sess.scrollback" {
			t.Errorf("unexpected file left after Save: %s", e.Name())
		}
	}
}

func TestLRUEviction(t *testing.T) {
	dir := t.TempDir()
	cap := int64(50) // tiny cap: 50 bytes
	a := New(dir, cap)

	// Write three sessions with 20 bytes each. After the third save total is 60 > 50.
	// Oldest two should be evicted until we are under cap (only the newest survives).
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("sess-%02d", i)
		data := bytes.Repeat([]byte{byte('A' + i)}, 20)
		if err := a.Save(id, data); err != nil {
			t.Fatalf("Save %s: %v", id, err)
		}
		// Sleep briefly so mtime differs on filesystems with 1-second resolution.
		time.Sleep(10 * time.Millisecond)
	}

	// sess-02 (most recently written) must still exist.
	got, err := a.Load("sess-02")
	if err != nil {
		t.Fatalf("Load sess-02: %v", err)
	}
	if got == nil {
		t.Error("sess-02 was evicted but should be protected (just written)")
	}

	// Total size must now be ≤ cap.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, _ := e.Info()
		if info != nil {
			total += info.Size()
		}
	}
	if total > cap {
		t.Errorf("total size after eviction: %d > cap %d", total, cap)
	}
}

func TestInvalidIDRejected(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, 0)

	cases := []string{"../../etc/passwd", "/abs/path", "a\\b", ""}
	for _, id := range cases {
		if err := a.Save(id, []byte("x")); err == nil {
			t.Errorf("Save(%q): expected error, got nil", id)
		}
		if _, err := a.Load(id); err == nil {
			t.Errorf("Load(%q): expected error, got nil", id)
		}
		if err := a.Delete(id); err == nil && id != "" {
			t.Errorf("Delete(%q): expected error, got nil", id)
		}
	}
}

func TestSaveCreatesDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "deep", "nested", "scrollback")
	a := New(dir, 0)

	if err := a.Save("sess-mkdir", []byte("hi")); err != nil {
		t.Fatalf("Save (create dirs): %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("dir was not created: %v", err)
	}
}
