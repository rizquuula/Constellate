package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/rizquuula/Constellate/internal/hub/domain/project"
)

// ProjectStore is a thread-safe in-memory implementation of projects.ProjectStore.
// Used in tests and the in-process E2E harness.
type ProjectStore struct {
	mu       sync.RWMutex
	projects map[string]project.Project
}

// NewProjectStore returns an empty ProjectStore.
func NewProjectStore() *ProjectStore {
	return &ProjectStore{projects: make(map[string]project.Project)}
}

// Create inserts a new project. Returns project.ErrDuplicatePath if a project
// with the same (machineID, path) already exists.
func (s *ProjectStore) Create(_ context.Context, p project.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.projects {
		if existing.MachineID() == p.MachineID() && existing.Path() == p.Path() {
			return fmt.Errorf("memory: create project %q: %w", p.ID(), project.ErrDuplicatePath)
		}
	}
	s.projects[p.ID()] = p
	return nil
}

// ByID returns the project with the given id.
// Returns project.ErrNotFound (wrapped) if not present.
func (s *ProjectStore) ByID(_ context.Context, id string) (project.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.projects[id]
	if !ok {
		return project.Project{}, fmt.Errorf("memory: by id %q: %w", id, project.ErrNotFound)
	}
	return p, nil
}

// List returns a snapshot of all stored projects.
func (s *ProjectStore) List(_ context.Context) ([]project.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]project.Project, 0, len(s.projects))
	for _, p := range s.projects {
		out = append(out, p)
	}
	return out, nil
}

// ListByMachine returns all projects for the given machine.
func (s *ProjectStore) ListByMachine(_ context.Context, machineID string) ([]project.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []project.Project
	for _, p := range s.projects {
		if p.MachineID() == machineID {
			out = append(out, p)
		}
	}
	return out, nil
}

// Delete permanently removes a project record.
// Returns project.ErrNotFound (wrapped) if not present.
func (s *ProjectStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.projects[id]; !ok {
		return fmt.Errorf("memory: delete %q: %w", id, project.ErrNotFound)
	}
	delete(s.projects, id)
	return nil
}
