package sysmetrics

import (
	"github.com/rizquuula/Constellate/internal/transport"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

// Collector samples host CPU and RAM metrics using gopsutil.
// It is safe for concurrent use; gopsutil calls are goroutine-safe.
type Collector struct{}

// Sample returns the current host metrics and whether the sample is usable.
// CPUPercent is the host CPU utilisation since the previous call (non-blocking,
// returns ~0 on the first call). It is set to -1 when unavailable.
// ok is false only when both CPU and RAM are unavailable.
func (Collector) Sample() (transport.Metrics, bool) {
	var m transport.Metrics
	cpuOK, ramOK := true, true

	percents, err := cpu.Percent(0, false)
	if err != nil || len(percents) == 0 {
		m.CPUPercent = -1
		cpuOK = false
	} else {
		m.CPUPercent = percents[0]
	}

	vm, err := mem.VirtualMemory()
	if err != nil {
		ramOK = false
	} else {
		m.MemUsedMB = int64(vm.Used / (1024 * 1024))
		m.MemTotalMB = int64(vm.Total / (1024 * 1024))
	}

	return m, cpuOK || ramOK
}
