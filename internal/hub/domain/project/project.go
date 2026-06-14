package project

import (
	"fmt"
	"strings"
)

// Project represents a logical grouping of sessions bound to a machine and working dir.
// Fields are unexported; use constructors and accessors.
type Project struct {
	id        string
	machineID string
	name      string
	path      string
	color     string
	createdAt int64
}

// New creates and validates a Project. Inputs are trimmed; id, machineID, name,
// and path must all be non-empty after trimming.
func New(id, machineID, name, path, color string, createdAt int64) (Project, error) {
	id = strings.TrimSpace(id)
	machineID = strings.TrimSpace(machineID)
	name = strings.TrimSpace(name)
	path = strings.TrimSpace(path)
	color = strings.TrimSpace(color)

	if id == "" {
		return Project{}, fmt.Errorf("project: id is empty: %w", ErrInvalid)
	}
	if machineID == "" {
		return Project{}, fmt.Errorf("project: machineID is empty: %w", ErrInvalid)
	}
	if name == "" {
		return Project{}, fmt.Errorf("project: name is empty: %w", ErrInvalid)
	}
	if path == "" {
		return Project{}, fmt.Errorf("project: path is empty: %w", ErrInvalid)
	}

	return Project{
		id:        id,
		machineID: machineID,
		name:      name,
		path:      path,
		color:     color,
		createdAt: createdAt,
	}, nil
}

// Rehydrate reconstructs a Project from a persisted row without re-validating.
func Rehydrate(id, machineID, name, path, color string, createdAt int64) Project {
	return Project{
		id:        id,
		machineID: machineID,
		name:      name,
		path:      path,
		color:     color,
		createdAt: createdAt,
	}
}

func (p Project) ID() string        { return p.id }
func (p Project) MachineID() string { return p.machineID }
func (p Project) Name() string      { return p.name }
func (p Project) Path() string      { return p.path }
func (p Project) Color() string     { return p.color }
func (p Project) CreatedAt() int64  { return p.createdAt }
