package dashboard

import (
	"context"

	"github.com/rizquuula/Constellate/internal/hub/domain/audit"
	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
	"github.com/rizquuula/Constellate/internal/hub/domain/project"
	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

// MachineStore is the read port for machine records.
// *sqlite.MachineStore and *memory.MachineStore satisfy this interface.
type MachineStore interface {
	List(ctx context.Context) ([]machine.Machine, error)
}

// LiveAgents is the read port for live agent connection presence.
// *agentlink.Registry satisfies this interface.
type LiveAgents interface {
	IsOnline(machineID string) bool
}

// SessionStore is the read port for session records.
// *sqlite.SessionStore and *memory.SessionStore satisfy this interface.
type SessionStore interface {
	List(ctx context.Context) ([]session.Session, error)
}

// ProjectStore is the read port for project records.
// *sqlite.ProjectStore and *memory.ProjectStore satisfy this interface.
type ProjectStore interface {
	List(ctx context.Context) ([]project.Project, error)
}

// AuditReader is the read port for recent audit events.
// *sqlite.AuditStore and *memory.AuditStore satisfy this interface.
type AuditReader interface {
	List(ctx context.Context, limit int) ([]audit.Event, error)
}
