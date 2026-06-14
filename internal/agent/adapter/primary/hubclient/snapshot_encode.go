package hubclient

import (
	"github.com/rizquuula/Constellate/internal/agent/domain/terminal"
	"github.com/rizquuula/Constellate/internal/transport"
)

// encodeSnapshot translates a terminal.Screen (from the vt emulator) into a
// transport.Snapshot suitable for the snapshot stream. The encoding is a
// near-direct field copy: terminal.Cell FG/BG/Attrs use the same numeric
// encoding as transport.SnapRun FG/BG/Attrs (intentional design identity).
//
// Each row is run-length encoded: contiguous cells sharing (FG, BG, Attrs)
// collapse into a single SnapRun. A trailing run that is entirely default-blank
// (space rune, FG/BG/Attrs all zero) is dropped to save bytes; viewers pad the
// row remainder with default-styled spaces.
func encodeSnapshot(s terminal.SessionScreen, machineID string) transport.Snapshot {
	scr := s.Screen
	lines := make([]transport.SnapLine, scr.Rows)

	for y := 0; y < scr.Rows; y++ {
		row := scr.Cells[y]
		var runs []transport.SnapRun

		if len(row) == 0 {
			lines[y] = transport.SnapLine{}
			continue
		}

		// Start first run.
		runFG := row[0].FG
		runBG := row[0].BG
		runAttrs := row[0].Attrs
		runText := []rune{row[0].Rune}

		flush := func() {
			if len(runText) == 0 {
				return
			}
			runs = append(runs, transport.SnapRun{
				Text:  string(runText),
				FG:    runFG,
				BG:    runBG,
				Attrs: runAttrs,
			})
		}

		for x := 1; x < len(row); x++ {
			c := row[x]
			if c.FG == runFG && c.BG == runBG && c.Attrs == runAttrs {
				runText = append(runText, c.Rune)
			} else {
				flush()
				runFG = c.FG
				runBG = c.BG
				runAttrs = c.Attrs
				runText = []rune{c.Rune}
			}
		}
		flush()

		// Drop the trailing run if it is entirely default-blank (space, all-zero style).
		for len(runs) > 0 {
			last := runs[len(runs)-1]
			if last.FG == 0 && last.BG == 0 && last.Attrs == 0 && isAllSpaces(last.Text) {
				runs = runs[:len(runs)-1]
			} else {
				break
			}
		}

		lines[y] = transport.SnapLine{Runs: runs}
	}

	return transport.Snapshot{
		Type:      transport.TypeSnapshot,
		SessionID: s.ID,
		MachineID: machineID,
		Cols:      scr.Cols,
		Rows:      scr.Rows,
		Cursor: transport.Cursor{
			X:       scr.Cursor.X,
			Y:       scr.Cursor.Y,
			Visible: scr.Cursor.Visible,
		},
		Lines: lines,
		Rev:   s.Rev,
	}
}

// isAllSpaces reports whether every rune in s is a space character.
func isAllSpaces(s string) bool {
	for _, r := range s {
		if r != ' ' {
			return false
		}
	}
	return true
}
