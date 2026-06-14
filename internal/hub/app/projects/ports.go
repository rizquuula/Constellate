package projects

import (
	"context"

	"github.com/rizquuula/Constellate/internal/hub/domain/project"
)

// ProjectStore is the persistence port for project records.
type ProjectStore interface {
	Create(ctx context.Context, p project.Project) error
	ByID(ctx context.Context, id string) (project.Project, error)
	List(ctx context.Context) ([]project.Project, error)
	ListByMachine(ctx context.Context, machineID string) ([]project.Project, error)
}

// Clock returns the current unix-second timestamp.
type Clock interface {
	Now() int64
}
