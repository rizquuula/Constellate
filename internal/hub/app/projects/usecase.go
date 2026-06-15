package projects

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/domain/project"
)

// ErrHasSessions is returned when a project delete is refused because the
// project still owns one or more sessions. Callers must reassign or remove
// those sessions first. Maps to HTTP 409 Conflict.
var ErrHasSessions = errors.New("projects: project has sessions")

// SystemClock implements Clock using the real wall clock.
type SystemClock struct{}

func (SystemClock) Now() int64 { return time.Now().Unix() }

// CreateInput carries the data needed to create a new project.
type CreateInput struct {
	MachineID string
	Name      string
	Path      string
	Color     string
}

// UseCase orchestrates project management.
type UseCase struct {
	store    ProjectStore
	sessions SessionCounter
	clock    Clock
	newID    func() string
	log      *slog.Logger
}

// New constructs a UseCase with the provided adapters.
func New(store ProjectStore, sessions SessionCounter, clock Clock, newID func() string, log *slog.Logger) *UseCase {
	return &UseCase{
		store:    store,
		sessions: sessions,
		clock:    clock,
		newID:    newID,
		log:      log,
	}
}

// Create generates a new project ID, builds the domain object, and persists it.
// Returns project.ErrDuplicatePath if a project with the same (machineID, path) exists.
func (u *UseCase) Create(ctx context.Context, in CreateInput) (project.Project, error) {
	id := u.newID()
	now := u.clock.Now()
	p, err := project.New(id, in.MachineID, in.Name, in.Path, in.Color, now)
	if err != nil {
		return project.Project{}, err
	}
	if err := u.store.Create(ctx, p); err != nil {
		return project.Project{}, err
	}
	u.log.Info("project created", "projectID", id, "machineID", in.MachineID, "name", in.Name)
	return p, nil
}

// List returns all project records.
func (u *UseCase) List(ctx context.Context) ([]project.Project, error) {
	return u.store.List(ctx)
}

// ListByMachine returns all projects for the given machine.
func (u *UseCase) ListByMachine(ctx context.Context, machineID string) ([]project.Project, error) {
	return u.store.ListByMachine(ctx, machineID)
}

// ByID returns a single project by its ID.
func (u *UseCase) ByID(ctx context.Context, id string) (project.Project, error) {
	return u.store.ByID(ctx, id)
}

// Delete removes a project. It refuses (ErrHasSessions) if the project still
// owns any sessions — they must be reassigned or removed first. Returns
// project.ErrNotFound if no project with the given ID exists.
func (u *UseCase) Delete(ctx context.Context, id string) error {
	p, err := u.store.ByID(ctx, id)
	if err != nil {
		return err
	}
	n, err := u.sessions.CountByProject(ctx, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrHasSessions
	}
	if err := u.store.Delete(ctx, id); err != nil {
		return err
	}
	u.log.Info("project deleted", "projectID", id, "machineID", p.MachineID())
	return nil
}
