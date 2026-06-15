package agentlink_test

import (
	"sync"
	"testing"

	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/agentlink"
	"github.com/rizquuula/Constellate/internal/transport"
)

func TestRegistry_AddGetRemove(t *testing.T) {
	r := agentlink.NewRegistry()

	c := agentlink.NewConn("m1", nil, nil, 100)
	r.Add("m1", c)

	got, ok := r.Get("m1")
	if !ok {
		t.Fatal("Get: expected to find m1")
	}
	if got.MachineID != "m1" {
		t.Errorf("Get MachineID: got %q", got.MachineID)
	}

	r.Remove("m1")
	_, ok = r.Get("m1")
	if ok {
		t.Error("Get after Remove: expected not found")
	}
}

func TestRegistry_IsOnline(t *testing.T) {
	r := agentlink.NewRegistry()

	if r.IsOnline("m1") {
		t.Error("IsOnline: should be false before Add")
	}

	r.Add("m1", agentlink.NewConn("m1", nil, nil, 0))
	if !r.IsOnline("m1") {
		t.Error("IsOnline: should be true after Add")
	}

	r.Remove("m1")
	if r.IsOnline("m1") {
		t.Error("IsOnline: should be false after Remove")
	}
}

func TestRegistry_OnlineIDs(t *testing.T) {
	r := agentlink.NewRegistry()

	r.Add("m1", agentlink.NewConn("m1", nil, nil, 0))
	r.Add("m2", agentlink.NewConn("m2", nil, nil, 0))

	ids := r.OnlineIDs()
	if len(ids) != 2 {
		t.Fatalf("OnlineIDs: got %d, want 2", len(ids))
	}

	r.Remove("m1")
	ids = r.OnlineIDs()
	if len(ids) != 1 {
		t.Fatalf("OnlineIDs after Remove: got %d, want 1", len(ids))
	}
	if ids[0] != "m2" {
		t.Errorf("OnlineIDs: got %q, want m2", ids[0])
	}
}

func TestRegistry_UpdateMetrics(t *testing.T) {
	r := agentlink.NewRegistry()

	// Before any connection: Metrics returns not-found.
	_, ok := r.Metrics("m1")
	if ok {
		t.Error("Metrics before Add: expected ok=false")
	}

	c := agentlink.NewConn("m1", nil, nil, 0)
	r.Add("m1", c)

	// After Add but before any UpdateMetrics: still not found.
	_, ok = r.Metrics("m1")
	if ok {
		t.Error("Metrics after Add (no update yet): expected ok=false")
	}

	// After UpdateMetrics: values round-trip.
	r.UpdateMetrics("m1", transport.Metrics{CPUPercent: 12.5, MemUsedMB: 512, MemTotalMB: 4096})
	got, ok := r.Metrics("m1")
	if !ok {
		t.Fatal("Metrics after UpdateMetrics: expected ok=true")
	}
	if got.CPUPercent != 12.5 {
		t.Errorf("CPUPercent: got %v, want 12.5", got.CPUPercent)
	}
	if got.MemUsedMB != 512 {
		t.Errorf("MemUsedMB: got %d, want 512", got.MemUsedMB)
	}
	if got.MemTotalMB != 4096 {
		t.Errorf("MemTotalMB: got %d, want 4096", got.MemTotalMB)
	}

	// After Remove: not found again.
	r.Remove("m1")
	_, ok = r.Metrics("m1")
	if ok {
		t.Error("Metrics after Remove: expected ok=false")
	}
}

func TestRegistry_Concurrent(t *testing.T) {
	r := agentlink.NewRegistry()
	const n = 100

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "m" + string(rune('0'+i%10))
			r.Add(id, agentlink.NewConn(id, nil, nil, 0))
			r.IsOnline(id)
			r.OnlineIDs()
			r.Remove(id)
		}(i)
	}
	wg.Wait()
}
