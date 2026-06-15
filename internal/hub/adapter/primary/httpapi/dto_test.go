package httpapi

import (
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/app/registry"
	"github.com/rizquuula/Constellate/internal/hub/domain/machine"
)

func makeMachine() machine.Machine {
	return machine.New("m1", "", "box", "linux", "amd64", "0.1", 1000)
}

func TestMachineToDTO_NoMetrics(t *testing.T) {
	v := registry.MachineView{Machine: makeMachine(), Online: true, Metrics: nil}
	dto := machineToDTO(v)
	if dto.CPUPercent != nil {
		t.Errorf("CPUPercent: got %v, want nil", dto.CPUPercent)
	}
	if dto.MemUsedMB != nil {
		t.Errorf("MemUsedMB: got %v, want nil", dto.MemUsedMB)
	}
	if dto.MemTotalMB != nil {
		t.Errorf("MemTotalMB: got %v, want nil", dto.MemTotalMB)
	}
}

func TestMachineToDTO_MetricsPresent(t *testing.T) {
	met := registry.Metrics{CPUPercent: 25.0, MemUsedMB: 512, MemTotalMB: 8192}
	v := registry.MachineView{Machine: makeMachine(), Online: true, Metrics: &met}
	dto := machineToDTO(v)
	if dto.CPUPercent == nil || *dto.CPUPercent != 25.0 {
		t.Errorf("CPUPercent: got %v, want 25.0", dto.CPUPercent)
	}
	if dto.MemUsedMB == nil || *dto.MemUsedMB != 512 {
		t.Errorf("MemUsedMB: got %v, want 512", dto.MemUsedMB)
	}
	if dto.MemTotalMB == nil || *dto.MemTotalMB != 8192 {
		t.Errorf("MemTotalMB: got %v, want 8192", dto.MemTotalMB)
	}
}

func TestMachineToDTO_CPUMinusSentinel(t *testing.T) {
	// CPUPercent == -1 means unavailable; it must be omitted from the DTO.
	met := registry.Metrics{CPUPercent: -1, MemUsedMB: 256, MemTotalMB: 4096}
	v := registry.MachineView{Machine: makeMachine(), Online: true, Metrics: &met}
	dto := machineToDTO(v)
	if dto.CPUPercent != nil {
		t.Errorf("CPUPercent with sentinel -1: got %v, want nil (omitted)", dto.CPUPercent)
	}
	if dto.MemUsedMB == nil || *dto.MemUsedMB != 256 {
		t.Errorf("MemUsedMB: got %v, want 256", dto.MemUsedMB)
	}
}
