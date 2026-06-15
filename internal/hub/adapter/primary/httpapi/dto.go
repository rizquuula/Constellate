package httpapi

import (
	"github.com/rizquuula/Constellate/internal/hub/app/registry"
	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
	"github.com/rizquuula/Constellate/internal/hub/domain/project"
	"github.com/rizquuula/Constellate/internal/hub/domain/session"
)

// MachineDTO is the HTTP representation of a machine's view.
type MachineDTO struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	OS           string   `json:"os"`
	Arch         string   `json:"arch"`
	AgentVersion string   `json:"agentVersion"`
	EnrolledAt   int64    `json:"enrolledAt"`
	LastSeenAt   int64    `json:"lastSeenAt"`
	Online       bool     `json:"online"`
	Status       string   `json:"status"`
	CPUPercent   *float64 `json:"cpuPercent,omitempty"`
	MemUsedMB    *int64   `json:"memUsedMB,omitempty"`
	MemTotalMB   *int64   `json:"memTotalMB,omitempty"`
}

// SessionDTO is the HTTP representation of a session.
type SessionDTO struct {
	ID           string `json:"id"`
	MachineID    string `json:"machineID"`
	ProjectID    string `json:"projectID"`
	Title        string `json:"title"`
	Shell        string `json:"shell"`
	Status       string `json:"status"`
	ExitCode     int    `json:"exitCode"`
	CreatedAt    int64  `json:"createdAt"`
	LastActiveAt int64  `json:"lastActiveAt"`
	Activity     string `json:"activity"`
}

func sessionToDTO(s session.Session) SessionDTO {
	return SessionDTO{
		ID:           s.ID(),
		MachineID:    s.MachineID(),
		ProjectID:    s.ProjectID(),
		Title:        s.Title(),
		Shell:        s.Shell(),
		Status:       string(s.Status()),
		ExitCode:     s.ExitCode(),
		CreatedAt:    s.CreatedAt(),
		LastActiveAt: s.LastActiveAt(),
		Activity:     s.Activity(),
	}
}

// ProjectDTO is the HTTP representation of a project.
type ProjectDTO struct {
	ID        string `json:"id"`
	MachineID string `json:"machineID"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Color     string `json:"color"`
	CreatedAt int64  `json:"createdAt"`
}

func projectToDTO(p project.Project) ProjectDTO {
	return ProjectDTO{
		ID:        p.ID(),
		MachineID: p.MachineID(),
		Name:      p.Name(),
		Path:      p.Path(),
		Color:     p.Color(),
		CreatedAt: p.CreatedAt(),
	}
}

func machineToDTO(v registry.MachineView) MachineDTO {
	status := string(machine.StatusOffline)
	if v.Online {
		status = string(machine.StatusOnline)
	}
	dto := MachineDTO{
		ID:           v.Machine.ID(),
		Name:         v.Machine.Name(),
		OS:           v.Machine.OS(),
		Arch:         v.Machine.Arch(),
		AgentVersion: v.Machine.AgentVersion(),
		EnrolledAt:   v.Machine.EnrolledAt(),
		LastSeenAt:   v.Machine.LastSeenAt(),
		Online:       v.Online,
		Status:       status,
	}
	if v.Metrics != nil {
		if v.Metrics.CPUPercent >= 0 {
			cpu := v.Metrics.CPUPercent
			dto.CPUPercent = &cpu
		}
		used := v.Metrics.MemUsedMB
		total := v.Metrics.MemTotalMB
		dto.MemUsedMB = &used
		dto.MemTotalMB = &total
	}
	return dto
}
