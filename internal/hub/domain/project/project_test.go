package project_test

import (
	"errors"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/domain/project"
)

func TestNew_Valid(t *testing.T) {
	p, err := project.New("pid1", "mid1", "myproject", "/home/user/work", "#ff0000", 1000)
	if err != nil {
		t.Fatalf("New: unexpected error: %v", err)
	}
	if p.ID() != "pid1" {
		t.Errorf("ID: got %q, want pid1", p.ID())
	}
	if p.MachineID() != "mid1" {
		t.Errorf("MachineID: got %q, want mid1", p.MachineID())
	}
	if p.Name() != "myproject" {
		t.Errorf("Name: got %q, want myproject", p.Name())
	}
	if p.Path() != "/home/user/work" {
		t.Errorf("Path: got %q, want /home/user/work", p.Path())
	}
	if p.Color() != "#ff0000" {
		t.Errorf("Color: got %q, want #ff0000", p.Color())
	}
	if p.CreatedAt() != 1000 {
		t.Errorf("CreatedAt: got %d, want 1000", p.CreatedAt())
	}
}

func TestNew_Trimming(t *testing.T) {
	p, err := project.New("  pid1  ", "  mid1  ", "  myproject  ", "  /work  ", "  blue  ", 1000)
	if err != nil {
		t.Fatalf("New with surrounding spaces: unexpected error: %v", err)
	}
	if p.ID() != "pid1" {
		t.Errorf("ID not trimmed: got %q", p.ID())
	}
	if p.Name() != "myproject" {
		t.Errorf("Name not trimmed: got %q", p.Name())
	}
	if p.Path() != "/work" {
		t.Errorf("Path not trimmed: got %q", p.Path())
	}
}

func TestNew_EmptyIDRejected(t *testing.T) {
	_, err := project.New("", "mid1", "myproject", "/work", "", 1000)
	if !errors.Is(err, project.ErrInvalid) {
		t.Errorf("empty id: got %v, want ErrInvalid", err)
	}
}

func TestNew_EmptyMachineIDRejected(t *testing.T) {
	_, err := project.New("pid1", "", "myproject", "/work", "", 1000)
	if !errors.Is(err, project.ErrInvalid) {
		t.Errorf("empty machineID: got %v, want ErrInvalid", err)
	}
}

func TestNew_EmptyNameRejected(t *testing.T) {
	_, err := project.New("pid1", "mid1", "", "/work", "", 1000)
	if !errors.Is(err, project.ErrInvalid) {
		t.Errorf("empty name: got %v, want ErrInvalid", err)
	}
}

func TestNew_EmptyPathRejected(t *testing.T) {
	_, err := project.New("pid1", "mid1", "myproject", "", "", 1000)
	if !errors.Is(err, project.ErrInvalid) {
		t.Errorf("empty path: got %v, want ErrInvalid", err)
	}
}

func TestNew_WhitespaceOnlyNameRejected(t *testing.T) {
	_, err := project.New("pid1", "mid1", "   ", "/work", "", 1000)
	if !errors.Is(err, project.ErrInvalid) {
		t.Errorf("whitespace-only name: got %v, want ErrInvalid", err)
	}
}

func TestNew_ColorOptional(t *testing.T) {
	p, err := project.New("pid1", "mid1", "myproject", "/work", "", 1000)
	if err != nil {
		t.Fatalf("New without color: unexpected error: %v", err)
	}
	if p.Color() != "" {
		t.Errorf("Color should be empty, got %q", p.Color())
	}
}

func TestRehydrate(t *testing.T) {
	p := project.Rehydrate("pid2", "mid2", "proj2", "/path2", "green", 2000)
	if p.ID() != "pid2" {
		t.Errorf("ID: got %q, want pid2", p.ID())
	}
	if p.CreatedAt() != 2000 {
		t.Errorf("CreatedAt: got %d, want 2000", p.CreatedAt())
	}
}
