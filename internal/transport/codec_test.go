package transport

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"
)

func TestEncodeDecodeHello(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	dec := NewDecoder(&buf)

	want := NewHello("machine-1", "", "devbox", "linux", "amd64", "0.1.0", ProtocolVersion)
	if err := enc.Encode(want); err != nil {
		t.Fatalf("Encode Hello: %v", err)
	}

	f, err := dec.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if f.Type != TypeHello {
		t.Fatalf("frame type: got %q, want %q", f.Type, TypeHello)
	}

	got, err := Unmarshal[Hello](f)
	if err != nil {
		t.Fatalf("Unmarshal Hello: %v", err)
	}
	if got.MachineID != want.MachineID {
		t.Errorf("MachineID: got %q, want %q", got.MachineID, want.MachineID)
	}
	if got.Name != want.Name {
		t.Errorf("Name: got %q, want %q", got.Name, want.Name)
	}
	if got.OS != want.OS {
		t.Errorf("OS: got %q, want %q", got.OS, want.OS)
	}
	if got.Arch != want.Arch {
		t.Errorf("Arch: got %q, want %q", got.Arch, want.Arch)
	}
	if got.AgentVersion != want.AgentVersion {
		t.Errorf("AgentVersion: got %q, want %q", got.AgentVersion, want.AgentVersion)
	}
	if got.ProtocolVersion != want.ProtocolVersion {
		t.Errorf("ProtocolVersion: got %d, want %d", got.ProtocolVersion, want.ProtocolVersion)
	}
}

func TestEncodeDecodeHeartbeat(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	dec := NewDecoder(&buf)

	now := time.Now().Unix()
	sessions := []SessionStat{
		{ID: "sess-1", Status: "running", BytesOut: 1024},
	}
	want := NewHeartbeat(now, sessions, nil)
	if err := enc.Encode(want); err != nil {
		t.Fatalf("Encode Heartbeat: %v", err)
	}

	f, err := dec.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if f.Type != TypeHeartbeat {
		t.Fatalf("frame type: got %q, want %q", f.Type, TypeHeartbeat)
	}

	got, err := Unmarshal[Heartbeat](f)
	if err != nil {
		t.Fatalf("Unmarshal Heartbeat: %v", err)
	}
	if got.TS != want.TS {
		t.Errorf("TS: got %d, want %d", got.TS, want.TS)
	}
	if len(got.Sessions) != 1 {
		t.Fatalf("Sessions len: got %d, want 1", len(got.Sessions))
	}
	if got.Sessions[0].ID != sessions[0].ID {
		t.Errorf("Session ID: got %q, want %q", got.Sessions[0].ID, sessions[0].ID)
	}
	if got.Sessions[0].BytesOut != sessions[0].BytesOut {
		t.Errorf("BytesOut: got %d, want %d", got.Sessions[0].BytesOut, sessions[0].BytesOut)
	}
}

func TestMultipleFrames(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	dec := NewDecoder(&buf)

	hello := NewHello("m1", "", "box", "linux", "arm64", "0.1.0", 1)
	hb := NewHeartbeat(12345, nil, nil)

	if err := enc.Encode(hello); err != nil {
		t.Fatalf("Encode Hello: %v", err)
	}
	if err := enc.Encode(hb); err != nil {
		t.Fatalf("Encode Heartbeat: %v", err)
	}

	f1, err := dec.Next()
	if err != nil {
		t.Fatalf("Next frame 1: %v", err)
	}
	if f1.Type != TypeHello {
		t.Errorf("frame 1 type: got %q, want %q", f1.Type, TypeHello)
	}

	f2, err := dec.Next()
	if err != nil {
		t.Fatalf("Next frame 2: %v", err)
	}
	if f2.Type != TypeHeartbeat {
		t.Errorf("frame 2 type: got %q, want %q", f2.Type, TypeHeartbeat)
	}

	_, err = dec.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF after last frame, got %v", err)
	}
}

func TestHeartbeatWithMetrics(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	dec := NewDecoder(&buf)

	m := &Metrics{CPUPercent: 42.5, MemUsedMB: 1024, MemTotalMB: 8192}
	want := NewHeartbeat(9999, nil, m)
	if err := enc.Encode(want); err != nil {
		t.Fatalf("Encode Heartbeat with Metrics: %v", err)
	}

	f, err := dec.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	got, err := Unmarshal[Heartbeat](f)
	if err != nil {
		t.Fatalf("Unmarshal Heartbeat: %v", err)
	}
	if got.Metrics == nil {
		t.Fatal("Metrics: got nil, want non-nil")
	}
	if got.Metrics.CPUPercent != m.CPUPercent {
		t.Errorf("CPUPercent: got %v, want %v", got.Metrics.CPUPercent, m.CPUPercent)
	}
	if got.Metrics.MemUsedMB != m.MemUsedMB {
		t.Errorf("MemUsedMB: got %d, want %d", got.Metrics.MemUsedMB, m.MemUsedMB)
	}
	if got.Metrics.MemTotalMB != m.MemTotalMB {
		t.Errorf("MemTotalMB: got %d, want %d", got.Metrics.MemTotalMB, m.MemTotalMB)
	}
}

func TestHeartbeatWithoutMetrics(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	dec := NewDecoder(&buf)

	want := NewHeartbeat(1234, nil, nil)
	if err := enc.Encode(want); err != nil {
		t.Fatalf("Encode Heartbeat without Metrics: %v", err)
	}

	f, err := dec.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	got, err := Unmarshal[Heartbeat](f)
	if err != nil {
		t.Fatalf("Unmarshal Heartbeat: %v", err)
	}
	if got.Metrics != nil {
		t.Errorf("Metrics: got %+v, want nil", got.Metrics)
	}
}

func TestProtocolSupported5(t *testing.T) {
	if !ProtocolSupported(5) {
		t.Error("ProtocolSupported(5): got false, want true")
	}
	if !ProtocolSupported(4) {
		t.Error("ProtocolSupported(4): got false, want true")
	}
	if !ProtocolSupported(1) {
		t.Error("ProtocolSupported(1): got false, want true (min boundary)")
	}
	if ProtocolSupported(0) {
		t.Error("ProtocolSupported(0): got true, want false (below min)")
	}
	if ProtocolSupported(6) {
		t.Error("ProtocolSupported(6): got true, want false (above max)")
	}
}
