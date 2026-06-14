package hubclient

import (
	"testing"

	"github.com/rizquuula/Constellate/internal/agent/domain/terminal"
	"github.com/rizquuula/Constellate/internal/transport"
)

// makeCell is a convenience constructor for terminal.Cell.
func makeCell(r rune, fg, bg int, attrs uint16) terminal.Cell {
	return terminal.Cell{Rune: r, FG: fg, BG: bg, Attrs: attrs}
}

// TestEncodeSnapshotMetadata verifies that top-level fields are populated correctly.
func TestEncodeSnapshotMetadata(t *testing.T) {
	scr := terminal.Screen{
		Cols: 10,
		Rows: 2,
		Cursor: terminal.Cursor{X: 3, Y: 1, Visible: true},
		Cells: [][]terminal.Cell{
			{makeCell('A', 0, 0, 0), makeCell(' ', 0, 0, 0), makeCell(' ', 0, 0, 0),
				makeCell(' ', 0, 0, 0), makeCell(' ', 0, 0, 0), makeCell(' ', 0, 0, 0),
				makeCell(' ', 0, 0, 0), makeCell(' ', 0, 0, 0), makeCell(' ', 0, 0, 0),
				makeCell(' ', 0, 0, 0)},
			blankRow(10),
		},
	}
	s := terminal.SessionScreen{ID: "sess-1", Rev: 42, Screen: scr}
	snap := encodeSnapshot(s, "machine-X")

	if snap.Type != transport.TypeSnapshot {
		t.Errorf("Type: got %q, want %q", snap.Type, transport.TypeSnapshot)
	}
	if snap.SessionID != "sess-1" {
		t.Errorf("SessionID: got %q", snap.SessionID)
	}
	if snap.MachineID != "machine-X" {
		t.Errorf("MachineID: got %q", snap.MachineID)
	}
	if snap.Cols != 10 || snap.Rows != 2 {
		t.Errorf("Cols/Rows: got %d×%d, want 10×2", snap.Cols, snap.Rows)
	}
	if snap.Cursor.X != 3 || snap.Cursor.Y != 1 || !snap.Cursor.Visible {
		t.Errorf("Cursor: got %+v", snap.Cursor)
	}
	if snap.Rev != 42 {
		t.Errorf("Rev: got %d, want 42", snap.Rev)
	}
	if len(snap.Lines) != 2 {
		t.Errorf("Lines count: got %d, want 2", len(snap.Lines))
	}
}

// blankRow returns a row of cols blank cells.
func blankRow(cols int) []terminal.Cell {
	row := make([]terminal.Cell, cols)
	for i := range row {
		row[i] = terminal.BlankCell
	}
	return row
}

// TestEncodeSnapshotRunGrouping verifies RLE groups adjacent same-style cells.
func TestEncodeSnapshotRunGrouping(t *testing.T) {
	// Row: [bold 'A', bold 'B', default ' ', default ' ']
	bold := terminal.AttrBold
	row := []terminal.Cell{
		makeCell('A', 1, 0, bold),
		makeCell('B', 1, 0, bold),
		makeCell(' ', 0, 0, 0),
		makeCell(' ', 0, 0, 0),
	}
	scr := terminal.Screen{Cols: 4, Rows: 1, Cells: [][]terminal.Cell{row}}
	snap := encodeSnapshot(terminal.SessionScreen{ID: "x", Screen: scr}, "m")

	if len(snap.Lines) != 1 {
		t.Fatalf("Lines: got %d, want 1", len(snap.Lines))
	}
	runs := snap.Lines[0].Runs

	// Trailing blank run should be dropped; only the bold run should remain.
	if len(runs) != 1 {
		t.Fatalf("Runs: got %d, want 1 (trailing blank trimmed)", len(runs))
	}
	r := runs[0]
	if r.Text != "AB" {
		t.Errorf("run Text: got %q, want %q", r.Text, "AB")
	}
	if r.FG != 1 {
		t.Errorf("run FG: got %d, want 1", r.FG)
	}
	if r.Attrs != bold {
		t.Errorf("run Attrs: got %d, want %d", r.Attrs, bold)
	}
}

// TestEncodeSnapshotColorPassthrough verifies FG/BG values are copied faithfully.
func TestEncodeSnapshotColorPassthrough(t *testing.T) {
	// One cell with truecolor FG, palette BG.
	fg := terminal.ColorTruecolorFlag | (255 << 16) | (128 << 8) // #FF8000
	bg := 16 + 1                                                       // palette index 17
	row := []terminal.Cell{makeCell('Z', fg, bg, 0)}
	scr := terminal.Screen{Cols: 1, Rows: 1, Cells: [][]terminal.Cell{row}}
	snap := encodeSnapshot(terminal.SessionScreen{ID: "y", Screen: scr}, "m")

	runs := snap.Lines[0].Runs
	if len(runs) != 1 {
		t.Fatalf("Runs: got %d, want 1", len(runs))
	}
	if runs[0].FG != fg {
		t.Errorf("FG: got %d, want %d", runs[0].FG, fg)
	}
	if runs[0].BG != bg {
		t.Errorf("BG: got %d, want %d", runs[0].BG, bg)
	}
}

// TestEncodeSnapshotTrailingBlankTrimming verifies that trailing all-default
// blank runs are omitted and non-blank tailing runs are kept.
func TestEncodeSnapshotTrailingBlankTrimming(t *testing.T) {
	// Row: ['X' default] [' ' default x3] — trailing blanks trimmed.
	row := []terminal.Cell{
		makeCell('X', 0, 0, 0),
		makeCell(' ', 0, 0, 0),
		makeCell(' ', 0, 0, 0),
		makeCell(' ', 0, 0, 0),
	}
	scr := terminal.Screen{Cols: 4, Rows: 1, Cells: [][]terminal.Cell{row}}
	snap := encodeSnapshot(terminal.SessionScreen{ID: "z", Screen: scr}, "m")

	runs := snap.Lines[0].Runs
	// 'X' and the blanks share the same style (all default), so they merge into
	// one run. That single run contains 'X' which is not a space, so it is NOT
	// trimmed.
	if len(runs) != 1 {
		t.Fatalf("Runs: got %d, want 1", len(runs))
	}
	if runs[0].Text != "X   " {
		t.Errorf("run Text: got %q, want %q", runs[0].Text, "X   ")
	}
}

// TestEncodeSnapshotAllBlankRow verifies a fully-blank default row produces
// zero runs (all trimmed).
func TestEncodeSnapshotAllBlankRow(t *testing.T) {
	row := blankRow(5)
	scr := terminal.Screen{Cols: 5, Rows: 1, Cells: [][]terminal.Cell{row}}
	snap := encodeSnapshot(terminal.SessionScreen{ID: "blank", Screen: scr}, "m")

	runs := snap.Lines[0].Runs
	if len(runs) != 0 {
		t.Errorf("all-blank row: got %d runs, want 0", len(runs))
	}
}

// TestEncodeSnapshotMultipleRuns verifies multiple style groups in a single row.
func TestEncodeSnapshotMultipleRuns(t *testing.T) {
	// Row: [fg=1 'A']['B'] [fg=2 'C']['D']
	row := []terminal.Cell{
		makeCell('A', 1, 0, 0),
		makeCell('B', 1, 0, 0),
		makeCell('C', 2, 0, 0),
		makeCell('D', 2, 0, 0),
	}
	scr := terminal.Screen{Cols: 4, Rows: 1, Cells: [][]terminal.Cell{row}}
	snap := encodeSnapshot(terminal.SessionScreen{ID: "multi", Screen: scr}, "m")

	runs := snap.Lines[0].Runs
	if len(runs) != 2 {
		t.Fatalf("Runs: got %d, want 2", len(runs))
	}
	if runs[0].Text != "AB" || runs[0].FG != 1 {
		t.Errorf("run[0]: got %+v", runs[0])
	}
	if runs[1].Text != "CD" || runs[1].FG != 2 {
		t.Errorf("run[1]: got %+v", runs[1])
	}
}
