package transport

import (
	"bytes"
	"testing"
)

func TestColorEncoding(t *testing.T) {
	if got := PaletteColor(0); got != 1 {
		t.Errorf("PaletteColor(0): got %d, want 1", got)
	}
	if got := PaletteColor(255); got != 256 {
		t.Errorf("PaletteColor(255): got %d, want 256", got)
	}
	// Truecolor values must be distinguishable from the palette range.
	tc := TruecolorColor(0x12, 0x34, 0x56)
	if tc < ColorTruecolorFlag {
		t.Fatalf("truecolor value %d below flag %d", tc, ColorTruecolorFlag)
	}
	if rgb := tc & 0xFFFFFF; rgb != 0x123456 {
		t.Errorf("truecolor rgb: got %06x, want 123456", rgb)
	}
}

func TestEncodeDecodeSnapshot(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	dec := NewDecoder(&buf)

	want := Snapshot{
		Type:      TypeSnapshot,
		SessionID: "sess-1",
		MachineID: "m1",
		Cols:      80,
		Rows:      2,
		Cursor:    Cursor{X: 5, Y: 1, Visible: true},
		Rev:       42,
		Lines: []SnapLine{
			{Runs: []SnapRun{
				{Text: "ok ", FG: PaletteColor(2), Attrs: AttrBold},
				{Text: "err", FG: TruecolorColor(255, 0, 0), BG: PaletteColor(0), Attrs: AttrInverse},
			}},
			{Runs: []SnapRun{{Text: "$"}}}, // default style, ColorDefault omitted
		},
	}

	if err := enc.Encode(want); err != nil {
		t.Fatalf("Encode Snapshot: %v", err)
	}

	f, err := dec.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if f.Type != TypeSnapshot {
		t.Fatalf("frame type: got %q, want %q", f.Type, TypeSnapshot)
	}

	got, err := Unmarshal[Snapshot](f)
	if err != nil {
		t.Fatalf("Unmarshal Snapshot: %v", err)
	}
	if got.SessionID != want.SessionID || got.MachineID != want.MachineID {
		t.Errorf("ids: got %q/%q", got.SessionID, got.MachineID)
	}
	if got.Cols != 80 || got.Rows != 2 || got.Rev != 42 {
		t.Errorf("dims/rev: got cols=%d rows=%d rev=%d", got.Cols, got.Rows, got.Rev)
	}
	if got.Cursor != want.Cursor {
		t.Errorf("cursor: got %+v, want %+v", got.Cursor, want.Cursor)
	}
	if len(got.Lines) != 2 || len(got.Lines[0].Runs) != 2 {
		t.Fatalf("lines shape: %+v", got.Lines)
	}
	r0 := got.Lines[0].Runs[0]
	if r0.Text != "ok " || r0.FG != PaletteColor(2) || r0.Attrs != AttrBold {
		t.Errorf("run0: got %+v", r0)
	}
	r1 := got.Lines[0].Runs[1]
	if r1.FG&0xFFFFFF != 0xFF0000 || r1.FG < ColorTruecolorFlag {
		t.Errorf("run1 fg truecolor: got %d", r1.FG)
	}
	if got.Lines[1].Runs[0].FG != ColorDefault {
		t.Errorf("default fg should decode to 0, got %d", got.Lines[1].Runs[0].FG)
	}
}

func TestEncodeDecodeEnableSnaps(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	dec := NewDecoder(&buf)

	if err := enc.Encode(NewEnableSnaps(true)); err != nil {
		t.Fatalf("Encode EnableSnaps: %v", err)
	}
	f, err := dec.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if f.Type != TypeEnableSnaps {
		t.Fatalf("frame type: got %q, want %q", f.Type, TypeEnableSnaps)
	}
	got, err := Unmarshal[EnableSnaps](f)
	if err != nil {
		t.Fatalf("Unmarshal EnableSnaps: %v", err)
	}
	if !got.Enabled {
		t.Errorf("Enabled: got false, want true")
	}
}

func TestProtocolWindow(t *testing.T) {
	if !ProtocolSupported(1) || !ProtocolSupported(2) || !ProtocolSupported(3) || !ProtocolSupported(4) {
		t.Errorf("protocol window should accept 1, 2, 3, and 4")
	}
	if ProtocolSupported(0) || ProtocolSupported(5) {
		t.Errorf("protocol window should reject 0 and 5")
	}
}
