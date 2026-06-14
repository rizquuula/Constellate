package projects_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/memory"
	"github.com/rizquuula/Constellate/internal/hub/app/projects"
	"github.com/rizquuula/Constellate/internal/hub/domain/project"
)

type fixedClock struct{ ts int64 }

func (c *fixedClock) Now() int64 { return c.ts }

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))
}

var idCounter int

func nextID() func() string {
	idCounter = 0
	return func() string {
		idCounter++
		return "generated-id"
	}
}

func TestCreate_Succeeds(t *testing.T) {
	store := memory.NewProjectStore()
	clk := &fixedClock{ts: 1000}
	uc := projects.New(store, clk, nextID(), discardLogger())

	p, err := uc.Create(context.Background(), projects.CreateInput{
		MachineID: "m1",
		Name:      "myproject",
		Path:      "/home/user/work",
		Color:     "#ff0000",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.MachineID() != "m1" {
		t.Errorf("MachineID: got %q, want m1", p.MachineID())
	}
	if p.Name() != "myproject" {
		t.Errorf("Name: got %q, want myproject", p.Name())
	}
	if p.Path() != "/home/user/work" {
		t.Errorf("Path: got %q, want /home/user/work", p.Path())
	}
	if p.CreatedAt() != 1000 {
		t.Errorf("CreatedAt: got %d, want 1000", p.CreatedAt())
	}
}

func TestCreate_DuplicatePath(t *testing.T) {
	store := memory.NewProjectStore()
	clk := &fixedClock{ts: 1000}

	var callCount int
	newID := func() string {
		callCount++
		return "id-" + string(rune('0'+callCount))
	}

	uc := projects.New(store, clk, newID, discardLogger())

	_, err := uc.Create(context.Background(), projects.CreateInput{
		MachineID: "m1",
		Name:      "proj1",
		Path:      "/work",
	})
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err = uc.Create(context.Background(), projects.CreateInput{
		MachineID: "m1",
		Name:      "proj2",
		Path:      "/work",
	})
	if !errors.Is(err, project.ErrDuplicatePath) {
		t.Errorf("duplicate path: got %v, want project.ErrDuplicatePath", err)
	}
}

func TestList(t *testing.T) {
	store := memory.NewProjectStore()
	clk := &fixedClock{ts: 1000}

	var callCount int
	newID := func() string {
		callCount++
		return "id-" + string(rune('0'+callCount))
	}
	uc := projects.New(store, clk, newID, discardLogger())

	_, err := uc.Create(context.Background(), projects.CreateInput{MachineID: "m1", Name: "p1", Path: "/a"})
	if err != nil {
		t.Fatalf("Create p1: %v", err)
	}
	_, err = uc.Create(context.Background(), projects.CreateInput{MachineID: "m2", Name: "p2", Path: "/b"})
	if err != nil {
		t.Fatalf("Create p2: %v", err)
	}

	list, err := uc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("List: got %d, want 2", len(list))
	}
}

func TestListByMachine(t *testing.T) {
	store := memory.NewProjectStore()
	clk := &fixedClock{ts: 1000}

	var callCount int
	newID := func() string {
		callCount++
		return "id-" + string(rune('0'+callCount))
	}
	uc := projects.New(store, clk, newID, discardLogger())

	_, err := uc.Create(context.Background(), projects.CreateInput{MachineID: "m1", Name: "p1", Path: "/a"})
	if err != nil {
		t.Fatalf("Create m1/p1: %v", err)
	}
	_, err = uc.Create(context.Background(), projects.CreateInput{MachineID: "m2", Name: "p2", Path: "/b"})
	if err != nil {
		t.Fatalf("Create m2/p2: %v", err)
	}

	list, err := uc.ListByMachine(context.Background(), "m1")
	if err != nil {
		t.Fatalf("ListByMachine: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListByMachine m1: got %d, want 1", len(list))
	}
	if list[0].MachineID() != "m1" {
		t.Errorf("ListByMachine: got machineID %q, want m1", list[0].MachineID())
	}
}
