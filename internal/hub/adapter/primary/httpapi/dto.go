package httpapi

import (
	"github.com/rizquuula/Constellate/internal/hub/app/registry"
	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
)

// MachineDTO is the HTTP representation of a machine's view.
type MachineDTO struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	AgentVersion string `json:"agentVersion"`
	EnrolledAt   int64  `json:"enrolledAt"`
	LastSeenAt   int64  `json:"lastSeenAt"`
	Online       bool   `json:"online"`
	Status       string `json:"status"`
}

func machineToDTO(v registry.MachineView) MachineDTO {
	status := string(machine.StatusOffline)
	if v.Online {
		status = string(machine.StatusOnline)
	}
	return MachineDTO{
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
}
