package sysmetrics_test

import (
	"runtime"
	"testing"

	"github.com/rizquuula/Constellate/internal/agent/adapter/secondary/sysmetrics"
)

func TestCollector_SampleLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only test")
	}
	c := sysmetrics.Collector{}
	m, ok := c.Sample()
	if !ok {
		t.Fatal("Sample: got ok=false, want true on Linux")
	}
	if m.MemTotalMB <= 0 {
		t.Errorf("MemTotalMB: got %d, want >0", m.MemTotalMB)
	}
}

func TestCollector_SampleReturnsBool(t *testing.T) {
	c := sysmetrics.Collector{}
	_, ok := c.Sample()
	// On any supported platform (linux/mac/windows), ok should be true.
	if !ok {
		t.Error("Sample: got ok=false; expected true on a supported platform")
	}
}
