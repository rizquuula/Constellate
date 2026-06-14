package vt

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rizquuula/Constellate/internal/agent/domain/terminal"
)

// ---------- test helpers ----------

// renderRows renders the screen to a slice of plain strings, one per row,
// with trailing spaces trimmed. Useful for readable assertions.
func renderRows(s terminal.Screen) []string {
	rows := make([]string, s.Rows)
	for y, row := range s.Cells {
		var sb strings.Builder
		for _, c := range row {
			if c.Rune == 0 {
				sb.WriteByte(' ')
			} else {
				sb.WriteRune(c.Rune)
			}
		}
		rows[y] = strings.TrimRight(sb.String(), " ")
	}
	return rows
}

// cellAt returns the cell at (row, col) in the screen.
func cellAt(s terminal.Screen, row, col int) terminal.Cell {
	return s.Cells[row][col]
}

// write is a helper that calls e.Write and returns the screen.
func write(e *Emulator, s string) terminal.Screen {
	e.Write([]byte(s))
	sc, _ := e.Render()
	return sc
}

// esc builds an escape sequence string.
func esc(s string) string { return "\x1b" + s }

// csi builds a CSI sequence: ESC [ params final.
func csi(params, final string) string { return "\x1b[" + params + final }

// ---------- plain text ----------

func TestPlainText(t *testing.T) {
	e := New(10, 3)
	sc := write(e, "hello")
	rows := renderRows(sc)
	if rows[0] != "hello" {
		t.Errorf("expected %q got %q", "hello", rows[0])
	}
	if sc.Cursor.X != 5 || sc.Cursor.Y != 0 {
		t.Errorf("cursor: want (5,0) got (%d,%d)", sc.Cursor.X, sc.Cursor.Y)
	}
}

func TestLineWrap(t *testing.T) {
	e := New(5, 3)
	sc := write(e, "ABCDEFGHI") // 9 chars on 5-wide screen → wraps at col 5 and 10
	rows := renderRows(sc)
	if rows[0] != "ABCDE" {
		t.Errorf("row0: want %q got %q", "ABCDE", rows[0])
	}
	if rows[1] != "FGHI" {
		t.Errorf("row1: want %q got %q", "FGHI", rows[1])
	}
}

func TestCRLF(t *testing.T) {
	e := New(10, 4)
	sc := write(e, "one\r\ntwo\r\nthree")
	rows := renderRows(sc)
	if rows[0] != "one" {
		t.Errorf("row0: want %q got %q", "one", rows[0])
	}
	if rows[1] != "two" {
		t.Errorf("row1: want %q got %q", "two", rows[1])
	}
	if rows[2] != "three" {
		t.Errorf("row2: want %q got %q", "three", rows[2])
	}
}

func TestCR(t *testing.T) {
	e := New(10, 3)
	write(e, "hello")
	sc := write(e, "\rXX")
	rows := renderRows(sc)
	if rows[0] != "XXllo" {
		t.Errorf("CR+overwrite: want %q got %q", "XXllo", rows[0])
	}
}

func TestTab(t *testing.T) {
	e := New(24, 3)
	sc := write(e, "A\tB")
	rows := renderRows(sc)
	// 'A' at col 0, tab advances to col 8, 'B' at col 8.
	want := "A       B"
	if rows[0] != want {
		t.Errorf("tab: want %q got %q", want, rows[0])
	}
}

func TestBackspace(t *testing.T) {
	e := New(10, 3)
	write(e, "ABC")
	sc := write(e, "\bX")
	rows := renderRows(sc)
	if rows[0] != "ABX" {
		t.Errorf("backspace: want %q got %q", "ABX", rows[0])
	}
}

// ---------- UTF-8 ----------

func TestUTF8(t *testing.T) {
	e := New(20, 3)
	// Split "Héllo" across two writes to test partial-sequence handling.
	e.Write([]byte("H\xc3")) // incomplete rune
	e.Write([]byte("\xa9llo"))
	sc, _ := e.Render()
	rows := renderRows(sc)
	if rows[0] != "Héllo" {
		t.Errorf("utf8: want %q got %q", "Héllo", rows[0])
	}
}

// ---------- CUP and relative cursor moves ----------

func TestCUP(t *testing.T) {
	e := New(20, 10)
	write(e, csi("5;10", "H")+"X")
	sc, _ := e.Render()
	if sc.Cursor.Y != 4 || sc.Cursor.X != 10 {
		t.Errorf("CUP: cursor want (10,4) got (%d,%d)", sc.Cursor.X, sc.Cursor.Y)
	}
	if cellAt(sc, 4, 9).Rune != 'X' {
		t.Errorf("CUP: expected X at (4,9)")
	}
}

func TestCursorMoves(t *testing.T) {
	e := New(20, 10)
	// Place cursor at (5,5) then move in each direction.
	write(e, csi("6;6", "H")) // CUP row=6,col=6 → (5,5)
	sc := write(e, csi("2", "A")) // CUU 2 → row 3
	if sc.Cursor.Y != 3 {
		t.Errorf("CUU: want Y=3 got Y=%d", sc.Cursor.Y)
	}
	sc = write(e, csi("1", "B")) // CUD 1 → row 4
	if sc.Cursor.Y != 4 {
		t.Errorf("CUD: want Y=4 got Y=%d", sc.Cursor.Y)
	}
	sc = write(e, csi("3", "C")) // CUF 3 → col 8
	if sc.Cursor.X != 8 {
		t.Errorf("CUF: want X=8 got X=%d", sc.Cursor.X)
	}
	sc = write(e, csi("2", "D")) // CUB 2 → col 6
	if sc.Cursor.X != 6 {
		t.Errorf("CUB: want X=6 got X=%d", sc.Cursor.X)
	}
}

func TestCNLCPL(t *testing.T) {
	e := New(20, 10)
	write(e, csi("5;5", "H"))   // position (4,4)
	sc := write(e, csi("2", "E")) // CNL 2 → row 6, col 0
	if sc.Cursor.Y != 6 || sc.Cursor.X != 0 {
		t.Errorf("CNL: want (0,6) got (%d,%d)", sc.Cursor.X, sc.Cursor.Y)
	}
	sc = write(e, csi("3", "F")) // CPL 3 → row 3, col 0
	if sc.Cursor.Y != 3 || sc.Cursor.X != 0 {
		t.Errorf("CPL: want (0,3) got (%d,%d)", sc.Cursor.X, sc.Cursor.Y)
	}
}

func TestCHA(t *testing.T) {
	e := New(20, 5)
	write(e, csi("3;3", "H"))
	sc := write(e, csi("10", "G")) // CHA col=10 → X=9
	if sc.Cursor.X != 9 {
		t.Errorf("CHA: want X=9 got X=%d", sc.Cursor.X)
	}
}

func TestVPA(t *testing.T) {
	e := New(20, 10)
	write(e, csi("5;5", "H"))
	sc := write(e, csi("8", "d")) // VPA row=8 → Y=7
	if sc.Cursor.Y != 7 {
		t.Errorf("VPA: want Y=7 got Y=%d", sc.Cursor.Y)
	}
}

// ---------- Erase ----------

func TestEDMode0(t *testing.T) {
	e := New(5, 3)
	write(e, "AAAAA\r\nBBBBB\r\nCCCCC")
	write(e, csi("2;3", "H")) // row=2,col=3 → (1,2) 0-based wait that's (Y=1,X=2)
	sc := write(e, csi("", "J")) // ED 0 — from cursor to end
	rows := renderRows(sc)
	if rows[0] != "AAAAA" {
		t.Errorf("ED0: row0 want AAAAA got %q", rows[0])
	}
	if rows[1] != "BB" {
		t.Errorf("ED0: row1 want BB got %q", rows[1])
	}
	if rows[2] != "" {
		t.Errorf("ED0: row2 want empty got %q", rows[2])
	}
}

func TestEDMode1(t *testing.T) {
	e := New(5, 3)
	write(e, "AAAAA\r\nBBBBB\r\nCCCCC")
	write(e, csi("2;3", "H")) // (Y=1,X=2)
	sc := write(e, csi("1", "J")) // ED 1 — from start to cursor
	rows := renderRows(sc)
	if rows[0] != "" {
		t.Errorf("ED1: row0 want empty got %q", rows[0])
	}
	if rows[2] != "CCCCC" {
		t.Errorf("ED1: row2 want CCCCC got %q", rows[2])
	}
}

func TestEDMode2(t *testing.T) {
	e := New(5, 3)
	write(e, "AAAAA\r\nBBBBB\r\nCCCCC")
	sc := write(e, csi("2", "J")) // ED 2 — erase all
	rows := renderRows(sc)
	for i, r := range rows {
		if r != "" {
			t.Errorf("ED2: row%d want empty got %q", i, r)
		}
	}
}

func TestELModes(t *testing.T) {
	e := New(10, 3)
	write(e, "ABCDEFGHIJ")
	write(e, csi("1;5", "H")) // (Y=0,X=4)

	// EL 0: from cursor to end of line.
	e2 := New(10, 3)
	write(e2, "ABCDEFGHIJ")
	write(e2, csi("1;5", "H"))
	sc := write(e2, csi("0", "K"))
	rows := renderRows(sc)
	if rows[0] != "ABCD" {
		t.Errorf("EL0: want ABCD got %q", rows[0])
	}

	// EL 1: from start to cursor.
	e3 := New(10, 3)
	write(e3, "ABCDEFGHIJ")
	write(e3, csi("1;5", "H"))
	sc = write(e3, csi("1", "K"))
	rows = renderRows(sc)
	if rows[0] != "     FGHIJ" {
		t.Errorf("EL1: want '     FGHIJ' got %q", rows[0])
	}

	// EL 2: erase entire line.
	e4 := New(10, 3)
	write(e4, "ABCDEFGHIJ")
	write(e4, csi("1;5", "H"))
	sc = write(e4, csi("2", "K"))
	rows = renderRows(sc)
	if rows[0] != "" {
		t.Errorf("EL2: want empty got %q", rows[0])
	}
}

// ---------- SGR ----------

func TestSGRBoldRedFG(t *testing.T) {
	e := New(10, 3)
	// SGR 1;31 = bold + fg red (palette 0 = black, 1 = red)
	e.Write([]byte(csi("1;31", "m") + "X"))
	sc, _ := e.Render()
	c := cellAt(sc, 0, 0)
	if c.Rune != 'X' {
		t.Errorf("SGR bold red: rune want X got %q", c.Rune)
	}
	if c.FG != 2 { // palette index+1: red=1 → 2
		t.Errorf("SGR bold red: FG want 2 got %d", c.FG)
	}
	if c.Attrs&terminal.AttrBold == 0 {
		t.Errorf("SGR bold red: expected AttrBold set")
	}
}

func TestSGR256Color(t *testing.T) {
	e := New(10, 3)
	// 38;5;200 = 256-colour FG, index 200
	e.Write([]byte(csi("38;5;200", "m") + "Y"))
	sc, _ := e.Render()
	c := cellAt(sc, 0, 0)
	if c.FG != 201 { // paletteIndex(200) = 201
		t.Errorf("SGR 256-color FG: want 201 got %d", c.FG)
	}
}

func TestSGRTruecolor(t *testing.T) {
	e := New(10, 3)
	// 38;2;255;128;0 = truecolour FG orange
	e.Write([]byte(csi("38;2;255;128;0", "m") + "Z"))
	sc, _ := e.Render()
	c := cellAt(sc, 0, 0)
	wantFG := terminal.ColorTruecolorFlag | (255 << 16) | (128 << 8)
	if c.FG != wantFG {
		t.Errorf("SGR truecolor FG: want %d got %d", wantFG, c.FG)
	}
}

func TestSGRInverse(t *testing.T) {
	e := New(10, 3)
	e.Write([]byte(csi("7", "m") + "I"))
	sc, _ := e.Render()
	c := cellAt(sc, 0, 0)
	if c.Attrs&terminal.AttrInverse == 0 {
		t.Errorf("SGR inverse: expected AttrInverse set")
	}
}

func TestSGRReset(t *testing.T) {
	e := New(10, 3)
	e.Write([]byte(csi("1;31;41", "m"))) // bold, fg red, bg red
	e.Write([]byte(csi("0", "m") + "R"))
	sc, _ := e.Render()
	c := cellAt(sc, 0, 0)
	if c.Attrs != 0 {
		t.Errorf("SGR reset: expected Attrs=0 got %d", c.Attrs)
	}
	if c.FG != terminal.ColorDefault {
		t.Errorf("SGR reset: expected FG=default got %d", c.FG)
	}
	if c.BG != terminal.ColorDefault {
		t.Errorf("SGR reset: expected BG=default got %d", c.BG)
	}
}

func TestSGRBrightColors(t *testing.T) {
	e := New(10, 3)
	// 90 = bright black fg (palette 8) → paletteIndex(8) = 9
	e.Write([]byte(csi("90", "m") + "B"))
	sc, _ := e.Render()
	c := cellAt(sc, 0, 0)
	if c.FG != 9 {
		t.Errorf("SGR bright fg 90: want FG=9 got %d", c.FG)
	}
}

func TestSGRBGColor(t *testing.T) {
	e := New(10, 3)
	// 44 = blue bg (palette 4) → paletteIndex(4) = 5
	e.Write([]byte(csi("44", "m") + "Q"))
	sc, _ := e.Render()
	c := cellAt(sc, 0, 0)
	if c.BG != 5 {
		t.Errorf("SGR bg 44: want BG=5 got %d", c.BG)
	}
}

func TestSGR256BG(t *testing.T) {
	e := New(10, 3)
	// 48;5;100 = 256-colour BG, index 100
	e.Write([]byte(csi("48;5;100", "m") + "W"))
	sc, _ := e.Render()
	c := cellAt(sc, 0, 0)
	if c.BG != 101 {
		t.Errorf("SGR 256-color BG: want 101 got %d", c.BG)
	}
}

// ---------- Scrolling ----------

func TestScrollAtBottom(t *testing.T) {
	e := New(5, 3)
	write(e, "AA\r\nBB\r\nCC\r\nDD") // 4 lines on 3-row screen → scroll
	sc, _ := e.Render()
	rows := renderRows(sc)
	if rows[0] != "BB" {
		t.Errorf("scroll: row0 want BB got %q", rows[0])
	}
	if rows[1] != "CC" {
		t.Errorf("scroll: row1 want CC got %q", rows[1])
	}
	if rows[2] != "DD" {
		t.Errorf("scroll: row2 want DD got %q", rows[2])
	}
}

func TestDECSTBMScroll(t *testing.T) {
	e := New(5, 5)
	// Fill screen.
	write(e, "AAAAA\r\nBBBBB\r\nCCCCC\r\nDDDDD\r\nEEEEE")
	// Set scroll region rows 2–4 (1-based), cursor goes home.
	write(e, csi("2;4", "r"))
	// Position cursor at bottom of region (row 4 = Y=3) and do LF.
	write(e, csi("4;1", "H"))
	sc := write(e, "\n")
	rows := renderRows(sc)
	// Row 1 (AAAAA) should be untouched.
	if rows[0] != "AAAAA" {
		t.Errorf("DECSTBM scroll: row0 want AAAAA got %q", rows[0])
	}
	// Row 5 (EEEEE) should be untouched.
	if rows[4] != "EEEEE" {
		t.Errorf("DECSTBM scroll: row4 want EEEEE got %q", rows[4])
	}
	// Within the region, BBBBB should have scrolled off; CCCCC and DDDDD moved up.
	if rows[1] != "CCCCC" {
		t.Errorf("DECSTBM scroll: row1 want CCCCC got %q", rows[1])
	}
	if rows[2] != "DDDDD" {
		t.Errorf("DECSTBM scroll: row2 want DDDDD got %q", rows[2])
	}
	if rows[3] != "" {
		t.Errorf("DECSTBM scroll: row3 want blank got %q", rows[3])
	}
}

// ---------- Alternate screen ----------

func TestAltScreen(t *testing.T) {
	e := New(10, 3)
	write(e, "primary")
	// Enter alt screen (?1049h).
	write(e, esc("[?1049h"))
	sc := write(e, "altcontent")
	rows := renderRows(sc)
	if rows[0] != "altcontent" {
		t.Errorf("altscreen: want altcontent got %q", rows[0])
	}

	// Leave alt screen (?1049l) — primary should be restored.
	sc = write(e, esc("[?1049l"))
	rows = renderRows(sc)
	if rows[0] != "primary" {
		t.Errorf("altscreen leave: want primary got %q", rows[0])
	}
}

// ---------- Cursor visibility ----------

func TestCursorVisibility(t *testing.T) {
	e := New(10, 3)
	sc, _ := e.Render()
	if !sc.Cursor.Visible {
		t.Error("cursor should start visible")
	}
	e.Write([]byte(esc("[?25l")))
	sc, _ = e.Render()
	if sc.Cursor.Visible {
		t.Error("cursor should be hidden after ?25l")
	}
	e.Write([]byte(esc("[?25h")))
	sc, _ = e.Render()
	if !sc.Cursor.Visible {
		t.Error("cursor should be visible after ?25h")
	}
}

// ---------- Save/restore cursor ----------

func TestSaveRestoreCursorDECSC(t *testing.T) {
	e := New(20, 10)
	write(e, csi("5;5", "H")) // (Y=4,X=4)
	write(e, esc("7"))        // DECSC
	write(e, csi("1;1", "H")) // move to origin
	sc := write(e, esc("8")) // DECRC
	if sc.Cursor.X != 4 || sc.Cursor.Y != 4 {
		t.Errorf("DECRC: want (4,4) got (%d,%d)", sc.Cursor.X, sc.Cursor.Y)
	}
}

func TestSaveRestoreCursorCSI(t *testing.T) {
	e := New(20, 10)
	write(e, csi("3;7", "H")) // (Y=2,X=6)
	write(e, csi("", "s"))    // CSI s — save
	write(e, csi("1;1", "H"))
	sc := write(e, csi("", "u")) // CSI u — restore
	if sc.Cursor.X != 6 || sc.Cursor.Y != 2 {
		t.Errorf("CSI u: want (6,2) got (%d,%d)", sc.Cursor.X, sc.Cursor.Y)
	}
}

// ---------- Split-write sequences ----------

func TestSplitEscapeSequence(t *testing.T) {
	e := New(10, 3)
	// Split CSI sequence: write "ESC [" in one call, "1;31m" + "X" in the next.
	e.Write([]byte("\x1b["))
	e.Write([]byte("1;31mX"))
	sc, _ := e.Render()
	c := cellAt(sc, 0, 0)
	if c.Rune != 'X' {
		t.Errorf("split CSI: rune want X got %q", c.Rune)
	}
	if c.Attrs&terminal.AttrBold == 0 {
		t.Errorf("split CSI: expected bold")
	}
	if c.FG != 2 {
		t.Errorf("split CSI: expected FG=2 (red) got %d", c.FG)
	}
}

func TestSplitOSC(t *testing.T) {
	// OSC sequences should be consumed without printing garbage.
	e := New(20, 3)
	e.Write([]byte("\x1b]0;window title"))
	e.Write([]byte("\x07text")) // BEL terminates OSC; "text" is printable
	sc, _ := e.Render()
	rows := renderRows(sc)
	if rows[0] != "text" {
		t.Errorf("OSC split: want 'text' got %q", rows[0])
	}
}

// ---------- Revision tracking ----------

func TestRevIncrementsOnChange(t *testing.T) {
	e := New(10, 3)
	_, rev0 := e.Render()
	e.Write([]byte("A"))
	_, rev1 := e.Render()
	if rev1 <= rev0 {
		t.Errorf("rev should increase after write: was %d now %d", rev0, rev1)
	}
}

func TestRevStableOnNoOp(t *testing.T) {
	e := New(10, 3)
	e.Write([]byte("A"))
	_, rev1 := e.Render()
	_, rev2 := e.Render() // second Render without any Write
	if rev2 != rev1 {
		t.Errorf("rev should not change on no-op Render: was %d now %d", rev1, rev2)
	}
}

func TestRevBellNoChange(t *testing.T) {
	e := New(10, 3)
	_, rev0 := e.Render()
	e.Write([]byte("\a")) // BEL — no visible change
	_, rev1 := e.Render()
	if rev1 != rev0 {
		t.Errorf("rev should not change on BEL: was %d now %d", rev0, rev1)
	}
}

// ---------- Resize ----------

func TestResizeSmallerNoPanic(t *testing.T) {
	e := New(20, 10)
	write(e, "hello world\r\nsecond line")
	e.Resize(5, 3) // shrink
	sc, _ := e.Render()
	if sc.Cols != 5 || sc.Rows != 3 {
		t.Errorf("resize: want 5×3 got %d×%d", sc.Cols, sc.Rows)
	}
	// Cursor must be within bounds.
	if sc.Cursor.X >= 5 || sc.Cursor.Y >= 3 {
		t.Errorf("resize: cursor OOB (%d,%d)", sc.Cursor.X, sc.Cursor.Y)
	}
}

func TestResizeLargerNoPanic(t *testing.T) {
	e := New(5, 3)
	write(e, "AB")
	e.Resize(20, 10) // grow
	sc, _ := e.Render()
	if sc.Cols != 20 || sc.Rows != 10 {
		t.Errorf("resize: want 20×10 got %d×%d", sc.Cols, sc.Rows)
	}
	rows := renderRows(sc)
	if rows[0] != "AB" {
		t.Errorf("resize grow: row0 want AB got %q", rows[0])
	}
}

// ---------- Insert/delete characters ----------

func TestIL(t *testing.T) {
	e := New(5, 3)
	write(e, "AAAAA\r\nBBBBB\r\nCCCCC")
	write(e, csi("2;1", "H")) // row=2,col=1 → (Y=1,X=0)
	sc := write(e, csi("1", "L")) // IL 1: insert 1 line at row 1
	rows := renderRows(sc)
	if rows[0] != "AAAAA" {
		t.Errorf("IL: row0 want AAAAA got %q", rows[0])
	}
	if rows[1] != "" {
		t.Errorf("IL: row1 want blank got %q", rows[1])
	}
	if rows[2] != "BBBBB" {
		t.Errorf("IL: row2 want BBBBB got %q", rows[2])
	}
}

func TestDL(t *testing.T) {
	e := New(5, 3)
	write(e, "AAAAA\r\nBBBBB\r\nCCCCC")
	write(e, csi("1;1", "H")) // (Y=0,X=0)
	sc := write(e, csi("1", "M")) // DL 1: delete 1 line at row 0
	rows := renderRows(sc)
	if rows[0] != "BBBBB" {
		t.Errorf("DL: row0 want BBBBB got %q", rows[0])
	}
	if rows[1] != "CCCCC" {
		t.Errorf("DL: row1 want CCCCC got %q", rows[1])
	}
	if rows[2] != "" {
		t.Errorf("DL: row2 want blank got %q", rows[2])
	}
}

func TestICH(t *testing.T) {
	e := New(10, 3)
	write(e, "ABCDE")
	write(e, csi("1;3", "H")) // (Y=0,X=2)
	sc := write(e, csi("2", "@")) // ICH 2: insert 2 blanks at col 2
	rows := renderRows(sc)
	if rows[0] != "AB  CDE" {
		t.Errorf("ICH: want 'AB  CDE' got %q", rows[0])
	}
}

func TestDCH(t *testing.T) {
	e := New(10, 3)
	write(e, "ABCDE")
	write(e, csi("1;2", "H")) // (Y=0,X=1)
	sc := write(e, csi("2", "P")) // DCH 2: delete 2 chars at col 1
	rows := renderRows(sc)
	if rows[0] != "ADE" {
		t.Errorf("DCH: want ADE got %q", rows[0])
	}
}

// ---------- SU / SD ----------

func TestSUSD(t *testing.T) {
	e := New(5, 3)
	write(e, "AAAAA\r\nBBBBB\r\nCCCCC")
	sc := write(e, csi("1", "S")) // SU 1: scroll up 1
	rows := renderRows(sc)
	if rows[0] != "BBBBB" {
		t.Errorf("SU: row0 want BBBBB got %q", rows[0])
	}
	if rows[1] != "CCCCC" {
		t.Errorf("SU: row1 want CCCCC got %q", rows[1])
	}
	if rows[2] != "" {
		t.Errorf("SU: row2 want blank got %q", rows[2])
	}

	e2 := New(5, 3)
	write(e2, "AAAAA\r\nBBBBB\r\nCCCCC")
	sc = write(e2, csi("1", "T")) // SD 1: scroll down 1
	rows = renderRows(sc)
	if rows[0] != "" {
		t.Errorf("SD: row0 want blank got %q", rows[0])
	}
	if rows[1] != "AAAAA" {
		t.Errorf("SD: row1 want AAAAA got %q", rows[1])
	}
	if rows[2] != "BBBBB" {
		t.Errorf("SD: row2 want BBBBB got %q", rows[2])
	}
}

// ---------- Realistic byte sequences ----------

func TestShellPromptColoredOutput(t *testing.T) {
	// Simulate a colored shell prompt + "ls --color" style output.
	// Sequence: bold green "user@host" + reset + ":" + bold blue "~/proj" + reset + "$ "
	// followed by "file.go" in cyan.
	prompt := csi("1;32", "m") + "user@host" + csi("0", "m") + ":" +
		csi("1;34", "m") + "~/proj" + csi("0", "m") + "$ "
	lsOutput := "\r\n" + csi("36", "m") + "file.go" + csi("0", "m")

	e := New(40, 5)
	e.Write([]byte(prompt + lsOutput))
	sc, _ := e.Render()
	rows := renderRows(sc)

	// Row 0: the prompt text
	if !strings.Contains(rows[0], "user@host") {
		t.Errorf("prompt: expected user@host in %q", rows[0])
	}
	if !strings.Contains(rows[0], "~/proj") {
		t.Errorf("prompt: expected ~/proj in %q", rows[0])
	}

	// Row 1: ls output — "file.go" in cyan (palette 6, → paletteIndex(6)=7)
	if rows[1] != "file.go" {
		t.Errorf("ls output: want 'file.go' got %q", rows[1])
	}
	fileCell := cellAt(sc, 1, 0)
	if fileCell.FG != 7 { // cyan = palette 6 → paletteIndex = 7
		t.Errorf("ls output: file.go FG want 7 (cyan) got %d", fileCell.FG)
	}

	// The "user@host" text should be bold.
	userCell := cellAt(sc, 0, 0)
	if userCell.Attrs&terminal.AttrBold == 0 {
		t.Errorf("prompt: user@host should be bold")
	}
}

func TestOSCTitleSequenceIgnored(t *testing.T) {
	// OSC 0 ; title BEL (xterm window title) must not corrupt the screen.
	e := New(20, 3)
	e.Write([]byte("\x1b]0;My Terminal Title\x07"))
	e.Write([]byte("normal"))
	sc, _ := e.Render()
	rows := renderRows(sc)
	if rows[0] != "normal" {
		t.Errorf("OSC ignored: want 'normal' got %q", rows[0])
	}
}

// ---------- Render deep-copy isolation ----------

func TestRenderDeepCopy(t *testing.T) {
	e := New(10, 3)
	write(e, "hello")
	sc1, _ := e.Render()
	// Mutate the returned screen directly.
	sc1.Cells[0][0].Rune = 'Z'
	// The emulator's internal state must not have changed.
	sc2, _ := e.Render()
	if sc2.Cells[0][0].Rune == 'Z' {
		t.Error("Render should return a deep copy; mutation leaked back")
	}
}

// ---------- Edge cases ----------

func TestEmptyWrite(t *testing.T) {
	e := New(10, 3)
	e.Write(nil)
	e.Write([]byte{})
	sc, rev := e.Render()
	if rev != 0 {
		t.Errorf("empty write: rev should be 0 got %d", rev)
	}
	_ = sc
}

func TestCursorClampOnEdges(t *testing.T) {
	e := New(5, 3)
	// Try to move cursor far out of bounds.
	write(e, csi("999;999", "H"))
	sc, _ := e.Render()
	if sc.Cursor.Y >= 3 || sc.Cursor.X >= 5 {
		t.Errorf("CUP OOB: cursor (%d,%d) should be clamped", sc.Cursor.X, sc.Cursor.Y)
	}
}

func TestUnknownSequenceIgnored(t *testing.T) {
	e := New(10, 3)
	// Various unknown/unsupported sequences; none should panic or corrupt text.
	e.Write([]byte("\x1b[999z"))   // unknown CSI final
	e.Write([]byte("\x1b#8"))      // DECDHL (unsupported ESC intermediate)
	e.Write([]byte("OK"))
	sc, _ := e.Render()
	rows := renderRows(sc)
	if rows[0] != "OK" {
		t.Errorf("unknown seq: want 'OK' got %q", rows[0])
	}
}

// ---------- Table-driven SGR ----------

func TestSGRTable(t *testing.T) {
	type tc struct {
		name      string
		seq       string
		wantFG    int
		wantBG    int
		wantAttrs uint16
	}
	tests := []tc{
		{"default", csi("0", "m"), terminal.ColorDefault, terminal.ColorDefault, 0},
		{"bold", csi("1", "m"), terminal.ColorDefault, terminal.ColorDefault, terminal.AttrBold},
		{"italic", csi("3", "m"), terminal.ColorDefault, terminal.ColorDefault, terminal.AttrItalic},
		{"underline", csi("4", "m"), terminal.ColorDefault, terminal.ColorDefault, terminal.AttrUnderline},
		{"blink", csi("5", "m"), terminal.ColorDefault, terminal.ColorDefault, terminal.AttrBlink},
		{"inverse", csi("7", "m"), terminal.ColorDefault, terminal.ColorDefault, terminal.AttrInverse},
		{"strike", csi("9", "m"), terminal.ColorDefault, terminal.ColorDefault, terminal.AttrStrike},
		{"fg30", csi("30", "m"), 1, terminal.ColorDefault, 0},  // black
		{"fg37", csi("37", "m"), 8, terminal.ColorDefault, 0},  // white
		{"fg90", csi("90", "m"), 9, terminal.ColorDefault, 0},  // bright black
		{"fg97", csi("97", "m"), 16, terminal.ColorDefault, 0}, // bright white
		{"bg40", csi("40", "m"), terminal.ColorDefault, 1, 0},
		{"bg47", csi("47", "m"), terminal.ColorDefault, 8, 0},
		{"bg100", csi("100", "m"), terminal.ColorDefault, 9, 0},
		{"bg107", csi("107", "m"), terminal.ColorDefault, 16, 0},
		{"fg39", csi("31;39", "m"), terminal.ColorDefault, terminal.ColorDefault, 0},
		{"bg49", csi("41;49", "m"), terminal.ColorDefault, terminal.ColorDefault, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := New(10, 3)
			e.Write([]byte(tc.seq + " ")) // space to deposit pen as a cell
			sc, _ := e.Render()
			c := cellAt(sc, 0, 0)
			if c.FG != tc.wantFG {
				t.Errorf("FG: want %d got %d", tc.wantFG, c.FG)
			}
			if c.BG != tc.wantBG {
				t.Errorf("BG: want %d got %d", tc.wantBG, c.BG)
			}
			if c.Attrs != tc.wantAttrs {
				t.Errorf("Attrs: want %d got %d", tc.wantAttrs, c.Attrs)
			}
		})
	}
}

// ---------- IL/DL in scroll region ----------

func TestILDLInScrollRegion(t *testing.T) {
	e := New(5, 5)
	write(e, "11111\r\n22222\r\n33333\r\n44444\r\n55555")
	write(e, csi("2;4", "r")) // scroll region rows 2–4 (1-based)
	write(e, csi("3;1", "H")) // position at row 3 (Y=2)
	sc := write(e, csi("1", "L")) // IL 1 at row 3 within region
	rows := renderRows(sc)
	if rows[0] != "11111" {
		t.Errorf("IL in region: row0 want 11111 got %q", rows[0])
	}
	if rows[4] != "55555" {
		t.Errorf("IL in region: row4 want 55555 got %q", rows[4])
	}
	if rows[2] != "" { // new blank line inserted at row 3
		t.Errorf("IL in region: row2 want blank got %q", rows[2])
	}
	if rows[3] != "33333" {
		t.Errorf("IL in region: row3 want 33333 got %q", rows[3])
	}
}

// ---------- ECH ----------

func TestECH(t *testing.T) {
	e := New(10, 3)
	write(e, "ABCDEFGHIJ")
	write(e, csi("1;3", "H")) // (Y=0, X=2)
	sc := write(e, csi("3", "X")) // ECH 3: erase 3 chars from col 2
	rows := renderRows(sc)
	if rows[0] != "AB   FGHIJ" {
		t.Errorf("ECH: want 'AB   FGHIJ' got %q", rows[0])
	}
}

// ---------- ICH/DCH large-n safety ----------

// TestICHLargeN verifies that ICH with n far larger than the screen width
// does not panic and produces a sane grid (cursor unchanged, chars shifted within bounds).
func TestICHLargeN(t *testing.T) {
	cols, rows := 10, 3
	e := New(cols, rows)
	write(e, "ABCDE")
	write(e, csi("1;3", "H")) // (Y=0, X=2)

	// ESC[999@ — n much larger than cols-cx (which is 8)
	e.Write([]byte(csi("999", "@")))
	sc, _ := e.Render()

	// Cursor must be unchanged (X=2, Y=0).
	if sc.Cursor.X != 2 || sc.Cursor.Y != 0 {
		t.Errorf("ICH large-n: cursor want (2,0) got (%d,%d)", sc.Cursor.X, sc.Cursor.Y)
	}
	// All cells must be within bounds (no index panic means the grid is valid).
	for y := 0; y < rows; y++ {
		if len(sc.Cells[y]) != cols {
			t.Errorf("ICH large-n: row %d has %d cells, want %d", y, len(sc.Cells[y]), cols)
		}
	}
	// After insert with n clamped to cols-cx=8, positions 2..9 are blank.
	// 'A' and 'B' (cols 0,1) remain; 'C','D','E' were pushed off the right edge.
	r := renderRows(sc)
	if r[0] != "AB" {
		t.Errorf("ICH large-n: row0 want 'AB' got %q", r[0])
	}
}

// TestDCHLargeN verifies that DCH with n far larger than the screen width
// does not panic and produces a sane grid.
func TestDCHLargeN(t *testing.T) {
	cols, rows := 10, 3
	e := New(cols, rows)
	write(e, "ABCDE")
	write(e, csi("1;2", "H")) // (Y=0, X=1)

	// ESC[100P — n much larger than cols-cx (which is 9)
	e.Write([]byte(csi("100", "P")))
	sc, _ := e.Render()

	// Cursor must be unchanged (X=1, Y=0).
	if sc.Cursor.X != 1 || sc.Cursor.Y != 0 {
		t.Errorf("DCH large-n: cursor want (1,0) got (%d,%d)", sc.Cursor.X, sc.Cursor.Y)
	}
	// All cells must be within bounds.
	for y := 0; y < rows; y++ {
		if len(sc.Cells[y]) != cols {
			t.Errorf("DCH large-n: row %d has %d cells, want %d", y, len(sc.Cells[y]), cols)
		}
	}
	// After delete with n clamped to cols-cx=9, all chars from col 1 onwards are blank.
	// 'A' (col 0) remains; everything else blanked.
	r := renderRows(sc)
	if r[0] != "A" {
		t.Errorf("DCH large-n: row0 want 'A' got %q", r[0])
	}
}

// Ensure Render is safe to call from multiple goroutines (no panic / race).
func TestConcurrentRender(t *testing.T) {
	e := New(80, 24)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 500; i++ {
			e.Write([]byte(fmt.Sprintf("line %d\r\n", i)))
		}
		close(done)
	}()
	for {
		select {
		case <-done:
			return
		default:
			e.Render()
		}
	}
}
