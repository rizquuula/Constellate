package machine_test

import (
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
)

func TestNew(t *testing.T) {
	m := machine.New("id1", "inst1", "mybox", "linux", "amd64", "0.1.0", 1000)

	if m.ID() != "id1" {
		t.Errorf("ID: got %q, want %q", m.ID(), "id1")
	}
	if m.InstanceID() != "inst1" {
		t.Errorf("InstanceID: got %q, want %q", m.InstanceID(), "inst1")
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
	m := machine.Rehydrate("id2", "inst2", "box2", "darwin", "arm64", "0.2.0", 500, 999)

	if m.InstanceID() != "inst2" {
		t.Errorf("InstanceID: got %q, want %q", m.InstanceID(), "inst2")
	}
	if m.EnrolledAt() != 500 {
		t.Errorf("EnrolledAt: got %d, want 500", m.EnrolledAt())
	}
	if m.LastSeenAt() != 999 {
		t.Errorf("LastSeenAt: got %d, want 999", m.LastSeenAt())
	}
}

func TestTouch(t *testing.T) {
	m := machine.New("id3", "", "box3", "linux", "amd64", "0.1.0", 100)
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

func TestRevocation(t *testing.T) {
	m := machine.New("id4", "", "box4", "linux", "amd64", "0.1.0", 100)

	if m.Revoked() {
		t.Error("new machine must not be revoked")
	}
	if m.RevokedAt() != 0 {
		t.Errorf("RevokedAt: got %d, want 0", m.RevokedAt())
	}

	revoked := m.MarkRevoked(9999)

	if !revoked.Revoked() {
		t.Error("MarkRevoked: should be revoked")
	}
	if revoked.RevokedAt() != 9999 {
		t.Errorf("RevokedAt: got %d, want 9999", revoked.RevokedAt())
	}

	// Original must be unchanged (value receiver).
	if m.Revoked() {
		t.Error("MarkRevoked must return a copy; original should remain unrevoked")
	}
}

func TestRehydrateFull(t *testing.T) {
	m := machine.RehydrateFull("id5", "inst5", "box5", "linux", "amd64", "0.2.0", 500, 600, 700)

	if m.EnrolledAt() != 500 {
		t.Errorf("EnrolledAt: got %d, want 500", m.EnrolledAt())
	}
	if m.LastSeenAt() != 600 {
		t.Errorf("LastSeenAt: got %d, want 600", m.LastSeenAt())
	}
	if m.RevokedAt() != 700 {
		t.Errorf("RevokedAt: got %d, want 700", m.RevokedAt())
	}
	if !m.Revoked() {
		t.Error("RehydrateFull with revokedAt=700 should be revoked")
	}
}
