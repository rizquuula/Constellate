package machine_test

import (
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
)

func TestNew(t *testing.T) {
	m := machine.New("id1", "mybox", "linux", "amd64", "0.1.0", 1000)

	if m.ID() != "id1" {
		t.Errorf("ID: got %q, want %q", m.ID(), "id1")
	}
	if m.Name() != "mybox" {
		t.Errorf("Name: got %q, want %q", m.Name(), "mybox")
	}
	if m.OS() != "linux" {
		t.Errorf("OS: got %q, want %q", m.OS(), "linux")
	}
	if m.Arch() != "amd64" {
		t.Errorf("Arch: got %q, want %q", m.Arch(), "amd64")
	}
	if m.AgentVersion() != "0.1.0" {
		t.Errorf("AgentVersion: got %q, want %q", m.AgentVersion(), "0.1.0")
	}
	if m.EnrolledAt() != 1000 {
		t.Errorf("EnrolledAt: got %d, want 1000", m.EnrolledAt())
	}
	if m.LastSeenAt() != 1000 {
		t.Errorf("New: LastSeenAt should equal EnrolledAt, got %d", m.LastSeenAt())
	}
}

func TestRehydrate(t *testing.T) {
	m := machine.Rehydrate("id2", "box2", "darwin", "arm64", "0.2.0", 500, 999)

	if m.EnrolledAt() != 500 {
		t.Errorf("EnrolledAt: got %d, want 500", m.EnrolledAt())
	}
	if m.LastSeenAt() != 999 {
		t.Errorf("LastSeenAt: got %d, want 999", m.LastSeenAt())
	}
}

func TestTouch(t *testing.T) {
	m := machine.New("id3", "box3", "linux", "amd64", "0.1.0", 100)
	if m.LastSeenAt() != 100 {
		t.Fatalf("pre-touch LastSeenAt: got %d, want 100", m.LastSeenAt())
	}
	m.Touch(200)
	if m.LastSeenAt() != 200 {
		t.Errorf("post-touch LastSeenAt: got %d, want 200", m.LastSeenAt())
	}
	if m.EnrolledAt() != 100 {
		t.Errorf("Touch must not change EnrolledAt: got %d", m.EnrolledAt())
	}
}
