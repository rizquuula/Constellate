package terminal

// Screen is an immutable snapshot of a terminal's current visible grid.
// It is produced by the vt emulator (adapter/secondary/vt) and consumed
// by the snapshot use case (app/snapshot) to produce transport.Snapshot records.
type Screen struct {
	Cols, Rows int
	Cursor     Cursor
	Cells      [][]Cell // [row][col]; exactly Rows rows × Cols cols
}

// Cursor is the visible text cursor (0-based; X=col, Y=row).
type Cursor struct {
	X, Y    int
	Visible bool
}

// Cell is one screen cell.
//
// FG/BG encoding:
//
//	ColorDefault (0)          = terminal default colour
//	1..256                    = palette index+1 (0..15 = ANSI, 16..255 = xterm-256)
//	>= ColorTruecolorFlag     = truecolor; RGB = value & 0xFFFFFF (0xRRGGBB)
//
// Attrs bitmask: see AttrBold … AttrStrike constants below.
// A blank cell is Cell{Rune: ' ', FG: ColorDefault, BG: ColorDefault, Attrs: 0}.
type Cell struct {
	Rune  rune
	FG    int
	BG    int
	Attrs uint16
}

// Attribute bitmask constants for Cell.Attrs.
const (
	AttrBold      uint16 = 1 << 0 // SGR 1
	AttrFaint     uint16 = 1 << 1 // SGR 2
	AttrItalic    uint16 = 1 << 2 // SGR 3
	AttrUnderline uint16 = 1 << 3 // SGR 4
	AttrBlink     uint16 = 1 << 4 // SGR 5
	AttrInverse   uint16 = 1 << 5 // SGR 7
	AttrHidden    uint16 = 1 << 6 // SGR 8
	AttrStrike    uint16 = 1 << 7 // SGR 9
)

// Colour sentinel values for Cell.FG and Cell.BG.
const (
	// ColorDefault means "use the terminal's default foreground/background".
	ColorDefault = 0

	// ColorTruecolorFlag marks a truecolor value. RGB is encoded in the lower
	// 24 bits: value = ColorTruecolorFlag | (r<<16) | (g<<8) | b.
	ColorTruecolorFlag = 0x1000000
)

// BlankCell is the zero value of a visible cell: a space, default colours, no attrs.
var BlankCell = Cell{Rune: ' '}
