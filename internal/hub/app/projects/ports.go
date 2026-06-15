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
	Delete(ctx context.Context, id string) error
}

// SessionCounter reports how many sessions reference a project. The delete use
// case uses it to refuse removing a project that still owns sessions.
type SessionCounter interface {
	CountByProject(ctx context.Context, projectID string) (int, error)
}

// Clock returns the current unix-second timestamp.
type Clock interface {
	Now() int64
}
