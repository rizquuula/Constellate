package terminal

import "testing"

func TestNewSession(t *testing.T) {
	s := New("id1", "/bin/bash", "/home/user", 80, 24)

	if s.ID() != "id1" {
		t.Errorf("ID: got %q, want %q", s.ID(), "id1")
	}
	if s.Shell() != "/bin/bash" {
		t.Errorf("Shell: got %q, want %q", s.Shell(), "/bin/bash")
	}
	if s.Cwd() != "/home/user" {
		t.Errorf("Cwd: got %q, want %q", s.Cwd(), "/home/user")
	}
	if s.Cols() != 80 {
		t.Errorf("Cols: got %d, want 80", s.Cols())
	}
	if s.Rows() != 24 {
		t.Errorf("Rows: got %d, want 24", s.Rows())
	}
	if s.Pid() != 0 {
		t.Errorf("Pid: got %d, want 0", s.Pid())
	}
	if s.Status() != StatusRunning {
		t.Errorf("Status: got %q, want %q", s.Status(), StatusRunning)
	}
}

func TestSessionMutators(t *testing.T) {
	s := New("id2", "/bin/sh", "/tmp", 120, 40)

	s.SetPid(1234)
	if s.Pid() != 1234 {
		t.Errorf("SetPid: got %d, want 1234", s.Pid())
	}

	s.SetStatus(StatusExited)
	if s.Status() != StatusExited {
		t.Errorf("SetStatus: got %q, want %q", s.Status(), StatusExited)
	}

	s.SetSize(100, 30)
	if s.Cols() != 100 {
		t.Errorf("SetSize cols: got %d, want 100", s.Cols())
	}
	if s.Rows() != 30 {
		t.Errorf("SetSize rows: got %d, want 30", s.Rows())
	}
}
