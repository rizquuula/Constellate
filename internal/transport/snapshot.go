package transport

// Snapshot is a compact, full-color copy of a session's current visible screen,
// produced by the agent's vt emulator and fanned out to overview viewers.
//
// Snapshots travel on a dedicated agent-opened "snapshot" yamux stream whose
// first NDJSON line is a SnapStreamHeader; every subsequent line is a Snapshot.
// The stream is screen-sized and rate-capped (see the agent's snapshot use
// case), so bandwidth stays bounded regardless of how busy the shells are.
//
// Rows are run-length encoded: contiguous cells that share fg/bg/attrs collapse
// into a single Run, which keeps even full-color frames small for typical
// terminal output.
type Snapshot struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"sessionID"`
	MachineID string      `json:"machineID"`
	Cols      int         `json:"cols"`
	Rows      int         `json:"rows"`
	Cursor    Cursor      `json:"cursor"`
	Lines     []SnapLine  `json:"lines"` // len == Rows; top-to-bottom
	Rev       uint64      `json:"rev"`   // monotonic per session; bumps only on visible change
}

// Cursor is the visible text cursor position (0-based, col=X row=Y).
type Cursor struct {
	X       int  `json:"x"`
	Y       int  `json:"y"`
	Visible bool `json:"visible"`
}

// SnapLine is one screen row, run-length encoded left-to-right. The concatenated
// run text spans exactly Cols columns (trailing blanks may be omitted: a viewer
// pads the remainder of the row with default-styled spaces).
type SnapLine struct {
	Runs []SnapRun `json:"runs"`
}

// SnapRun is a maximal span of adjacent cells sharing the same style.
//
// Color encoding (FG/BG), a single int:
//
//	0            → terminal default color
//	1..256       → palette index (value-1: 0..15 = ANSI, 16..255 = xterm-256)
//	>=0x1000000  → 24-bit truecolor; RGB = value & 0xFFFFFF (0xRRGGBB)
//
// Attrs is a bitmask:
//
//	1   bold
//	2   faint/dim
//	4   italic
//	8   underline
//	16  blink
//	32  inverse   (viewer swaps fg/bg)
//	64  hidden
//	128 strikethrough
type SnapRun struct {
	Text  string `json:"t"`
	FG    int    `json:"f,omitempty"`
	BG    int    `json:"b,omitempty"`
	Attrs uint16 `json:"a,omitempty"`
}

// Snapshot attribute bits (mirror the SnapRun.Attrs doc above).
const (
	AttrBold      uint16 = 1 << 0
	AttrFaint     uint16 = 1 << 1
	AttrItalic    uint16 = 1 << 2
	AttrUnderline uint16 = 1 << 3
	AttrBlink     uint16 = 1 << 4
	AttrInverse   uint16 = 1 << 5
	AttrHidden    uint16 = 1 << 6
	AttrStrike    uint16 = 1 << 7
)

// ColorDefault is the FG/BG value meaning "use the terminal default color".
const ColorDefault = 0

// ColorTruecolorFlag marks a truecolor value; the low 24 bits are 0xRRGGBB.
const ColorTruecolorFlag = 0x1000000

// PaletteColor encodes a 0..255 palette index as a SnapRun color value.
func PaletteColor(index int) int { return index + 1 }

// TruecolorColor encodes an r,g,b triple as a SnapRun color value.
func TruecolorColor(r, g, b uint8) int {
	return ColorTruecolorFlag | int(r)<<16 | int(g)<<8 | int(b)
}

// SnapStreamHeader is the first NDJSON line on the agent-opened snapshot stream.
// It identifies the stream kind so the hub can route it (data streams use
// AttachHeader instead).
type SnapStreamHeader struct {
	Type MessageType `json:"type"`
}

// NewSnapStreamHeader constructs a SnapStreamHeader with the Type field pre-set.
func NewSnapStreamHeader() SnapStreamHeader {
	return SnapStreamHeader{Type: TypeSnapStream}
}
