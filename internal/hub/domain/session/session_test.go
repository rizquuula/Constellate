package session_test

import (
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

func TestNew(t *testing.T) {
	s := session.New("sid1", "mid1", "pid1", "my title", "/bin/bash", 1000)

	if s.ID() != "sid1" {
		t.Errorf("ID: got %q, want %q", s.ID(), "sid1")
	}
	if s.MachineID() != "mid1" {
		t.Errorf("MachineID: got %q, want %q", s.MachineID(), "mid1")
	}
	if s.ProjectID() != "pid1" {
		t.Errorf("ProjectID: got %q, want %q", s.ProjectID(), "pid1")
	}
	if s.Title() != "my title" {
		t.Errorf("Title: got %q, want %q", s.Title(), "my title")
	}
	if s.Shell() != "/bin/bash" {
		t.Errorf("Shell: got %q, want %q", s.Shell(), "/bin/bash")
	}
	if s.Status() != session.StatusRunning {
		t.Errorf("Status: got %q, want running", s.Status())
	}
	if s.ExitCode() != 0 {
		t.Errorf("ExitCode: got %d, want 0", s.ExitCode())
	}
	if s.CreatedAt() != 1000 {
		t.Errorf("CreatedAt: got %d, want 1000", s.CreatedAt())
	}
	if s.LastActiveAt() != 1000 {
		t.Errorf("New: LastActiveAt should equal CreatedAt, got %d", s.LastActiveAt())
	}
}

func TestRehydrate(t *testing.T) {
	s := session.Rehydrate("sid2", "pid2", "mid2", "title2", "/bin/zsh", session.StatusExited, 1, 500, 999)

	if s.ID() != "sid2" {
		t.Errorf("ID: got %q, want sid2", s.ID())
	}
	if s.Status() != session.StatusExited {
		t.Errorf("Status: got %q, want exited", s.Status())
	}
	if s.ExitCode() != 1 {
		t.Errorf("ExitCode: got %d, want 1", s.ExitCode())
	}
	if s.CreatedAt() != 500 {
		t.Errorf("CreatedAt: got %d, want 500", s.CreatedAt())
	}
	if s.LastActiveAt() != 999 {
		t.Errorf("LastActiveAt: got %d, want 999", s.LastActiveAt())
	}
}

func TestSetExited(t *testing.T) {
	s := session.New("sid3", "mid3", "pid3", "t", "/bin/sh", 100)
	s.SetExited(2, 200)

	if s.Status() != session.StatusExited {
		t.Errorf("Status: got %q, want exited", s.Status())
	}
	if s.ExitCode() != 2 {
		t.Errorf("ExitCode: got %d, want 2", s.ExitCode())
	}
	if s.LastActiveAt() != 200 {
		t.Errorf("LastActiveAt: got %d, want 200", s.LastActiveAt())
	}
	if s.CreatedAt() != 100 {
		t.Errorf("SetExited must not change CreatedAt: got %d", s.CreatedAt())
	}
}

func TestSetStatus(t *testing.T) {
	s := session.New("sid4", "mid4", "", "", "", 100)
	s.SetStatus(session.StatusLost)
	if s.Status() != session.StatusLost {
		t.Errorf("Status: got %q, want lost", s.Status())
	}
}

func TestTouch(t *testing.T) {
	s := session.New("sid5", "mid5", "", "", "", 100)
	s.Touch(300)
	if s.LastActiveAt() != 300 {
		t.Errorf("Touch: got %d, want 300", s.LastActiveAt())
	}
	if s.CreatedAt() != 100 {
		t.Errorf("Touch must not change CreatedAt: got %d", s.CreatedAt())
	}
}

func TestSetTitle(t *testing.T) {
	s := session.New("sid6", "mid6", "", "original", "", 100)
	s.SetTitle("renamed")
	if s.Title() != "renamed" {
		t.Errorf("SetTitle: got %q, want renamed", s.Title())
	}
}
