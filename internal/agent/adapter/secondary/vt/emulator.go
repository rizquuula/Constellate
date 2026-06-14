// Package vt implements an ANSI/VT terminal emulator that consumes raw PTY output
// bytes and maintains the current visible screen as a full-colour cell grid plus
// cursor. It is designed for the Constellate overview pipeline (§7.2 of DESIGN.md):
// the agent feeds each session's output through an Emulator and periodically calls
// Render to snapshot the visible screen for transmission to the hub.
//
// Design lineage: the state machine follows the canonical Paul Williams ANSI parser
// (https://vt100.net/emu/dec_ansi_parser) and ECMA-48 / VT100 semantics — the same
// public documentation lineage that libraries such as tonistiigi/vt100 and
// vito/midterm descend from. All code is original.
package vt

import (
	"sync"
	"unicode/utf8"

	"github.com/rizquuula/Constellate/internal/agent/domain/terminal"
)

// Emulator is a stateful ANSI/VT terminal emulator.
// Create with New; feed PTY output with Write; snapshot the visible screen with Render.
type Emulator struct {
	mu sync.Mutex

	cols, rows int

	// primary and alternate screen buffers
	primary screen
	alt     screen
	useAlt  bool // true when the alternate screen is active

	// active screen pointer (always &primary or &alt, never nil)
	cur *screen

	// parser state (persists between Write calls)
	ps parser

	// cursor visibility
	cursorVisible bool

	// saved cursor positions (DECSC/DECRC + CSI s/u)
	savedCursor    cursorState
	savedCursorAlt cursorState

	// revision tracking
	rev   uint64
	dirty bool

	// utf8Buf holds an incomplete multibyte UTF-8 sequence that arrived at
	// the end of a Write call. Up to 3 bytes can be buffered (max UTF-8 = 4).
	utf8Buf [4]byte
	utf8Len int
}

// cursorState captures cursor position and attributes for save/restore.
type cursorState struct {
	x, y  int
	fg, bg int
	attrs uint16
}

// New creates an Emulator with the given initial screen size (cols × rows).
// cols and rows must both be > 0; if either is <= 0 it is clamped to 1.
func New(cols, rows int) *Emulator {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	e := &Emulator{
		cols:          cols,
		rows:          rows,
		cursorVisible: true,
	}
	e.primary = newScreen(cols, rows)
	e.alt = newScreen(cols, rows)
	e.cur = &e.primary
	e.ps.init()
	return e
}

// Write feeds raw PTY output bytes into the emulator, updating the screen.
// It never errors; malformed or unsupported sequences are silently consumed.
// Safe to call concurrently with Resize and Render.
func (e *Emulator) Write(p []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.writeUnlocked(p)
}

func (e *Emulator) writeUnlocked(p []byte) {
	for len(p) > 0 {
		b := p[0]
		if b < 0x80 {
			// ASCII byte — flush any buffered partial UTF-8 first.
			if e.utf8Len > 0 {
				// Discard the incomplete sequence; it will never complete.
				e.utf8Len = 0
			}
			a, _ := e.ps.feedByte(b)
			p = p[1:]
			e.dispatch(a, p)
		} else {
			// High byte: part of a multibyte UTF-8 sequence. Accumulate in the
			// utf8Buf until we have a complete rune (or evidence of an error).
			p = e.feedUTF8(p)
		}
	}
}

// feedUTF8 consumes the leading multibyte UTF-8 sequence from p, buffering
// across Write calls if p ends mid-sequence. Returns remaining unconsumed bytes.
func (e *Emulator) feedUTF8(p []byte) []byte {
	for len(p) > 0 {
		b := p[0]

		if b < 0x80 {
			// Non-continuation byte ends any pending partial sequence.
			// Discard the incomplete sequence silently.
			e.utf8Len = 0
			return p // caller will handle this ASCII byte
		}

		// Accumulate into utf8Buf.
		if e.utf8Len < len(e.utf8Buf) {
			e.utf8Buf[e.utf8Len] = b
			e.utf8Len++
		}
		p = p[1:]

		// Try to decode what we have so far.
		r, sz := utf8.DecodeRune(e.utf8Buf[:e.utf8Len])
		if r == utf8.RuneError && sz == 1 {
			// Could be an incomplete sequence — check if we need more bytes.
			// utf8.RuneError with sz==1 means either a bad byte or need-more-data.
			// We distinguish by checking whether the lead byte promises more bytes.
			need := utf8RuneLen(e.utf8Buf[0])
			if e.utf8Len >= need {
				// We had enough bytes but got an error → bad encoding, discard.
				e.utf8Len = 0
				return p
			}
			// Need more bytes — continue accumulating.
			// If continuation bytes keep coming (high bit set), stay in this loop.
			// If the next byte is ASCII, the outer loop will handle it.
			continue
		}
		// Successfully decoded a rune (or RuneError with sz>1 means a valid
		// replacement character encoding — treat as the rune).
		e.printRune(r)
		e.utf8Len = 0
		return p
	}
	// p exhausted with an incomplete sequence in utf8Buf — it will be completed
	// or flushed on the next Write call.
	return p
}

// utf8RuneLen returns the expected total byte count for a UTF-8 sequence
// starting with lead byte b, or 1 if b is invalid.
func utf8RuneLen(b byte) int {
	switch {
	case b&0x80 == 0:
		return 1
	case b&0xE0 == 0xC0:
		return 2
	case b&0xF0 == 0xE0:
		return 3
	case b&0xF8 == 0xF0:
		return 4
	default:
		return 1
	}
}

// Resize changes the screen dimensions. Cursor is clamped; content is preserved
// as much as practical (rows truncated or padded with blank lines; columns
// truncated or padded with blanks). Concurrency-safe.
func (e *Emulator) Resize(cols, rows int) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if cols == e.cols && rows == e.rows {
		return
	}
	e.cols = cols
	e.rows = rows
	e.primary.resize(cols, rows)
	e.alt.resize(cols, rows)
	e.dirty = true
}

// Rev returns the current revision counter without rendering the screen.
// The rev advances whenever the visible grid (cells or cursor) changes.
// Callers can use this to cheaply check whether a full Render is needed.
// Concurrency-safe.
func (e *Emulator) Rev() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	// If dirty, the next Render will advance the rev — return the prospective value.
	if e.dirty {
		return e.rev + 1
	}
	return e.rev
}

// Render returns an immutable copy of the current visible screen and a revision
// counter that strictly increases whenever the visible grid (cells or cursor)
// changed since the previous render-relevant write. If nothing changed the same
// rev is returned. Concurrency-safe.
func (e *Emulator) Render() (terminal.Screen, uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.dirty {
		e.rev++
		e.dirty = false
	}

	s := e.cur
	snap := terminal.Screen{
		Cols: e.cols,
		Rows: e.rows,
		Cursor: terminal.Cursor{
			X:       s.cx,
			Y:       s.cy,
			Visible: e.cursorVisible,
		},
		Cells: make([][]terminal.Cell, e.rows),
	}
	for y := 0; y < e.rows; y++ {
		row := make([]terminal.Cell, e.cols)
		copy(row, s.cells[y])
		snap.Cells[y] = row
	}
	return snap, e.rev
}

// ---------- screen type ----------

// screen is a mutable cell grid with a cursor and current SGR pen.
type screen struct {
	cols, rows int
	cells      [][]terminal.Cell
	cx, cy     int // cursor position (0-based)

	// SGR pen for new characters
	fg, bg int
	attrs  uint16

	// scroll region (1-based rows, inclusive); zero means "use full screen"
	scrollTop, scrollBottom int
}

func newScreen(cols, rows int) screen {
	s := screen{cols: cols, rows: rows}
	s.cells = allocGrid(cols, rows)
	s.scrollTop = 1
	s.scrollBottom = rows
	return s
}

func allocGrid(cols, rows int) [][]terminal.Cell {
	grid := make([][]terminal.Cell, rows)
	for i := range grid {
		grid[i] = blankRow(cols)
	}
	return grid
}

func blankRow(cols int) []terminal.Cell {
	row := make([]terminal.Cell, cols)
	for i := range row {
		row[i] = terminal.BlankCell
	}
	return row
}

// resize adjusts the grid dimensions. Existing content is preserved where
// possible; new cells are blank.
func (s *screen) resize(cols, rows int) {
	newGrid := allocGrid(cols, rows)
	copyRows := rows
	if copyRows > s.rows {
		copyRows = s.rows
	}
	for y := 0; y < copyRows; y++ {
		copyCols := cols
		if copyCols > s.cols {
			copyCols = s.cols
		}
		copy(newGrid[y][:copyCols], s.cells[y][:copyCols])
	}
	s.cols = cols
	s.rows = rows
	s.cells = newGrid
	// Clamp cursor.
	if s.cx >= cols {
		s.cx = cols - 1
	}
	if s.cy >= rows {
		s.cy = rows - 1
	}
	// Reset scroll region to full screen on resize.
	s.scrollTop = 1
	s.scrollBottom = rows
}

// effectiveScrollRegion returns the top and bottom row indices (0-based,
// inclusive) of the active scroll region.
func (s *screen) effectiveScrollRegion() (top, bot int) {
	top = s.scrollTop - 1
	bot = s.scrollBottom - 1
	if top < 0 {
		top = 0
	}
	if bot >= s.rows {
		bot = s.rows - 1
	}
	return top, bot
}

// penCell returns a blank cell coloured with the current SGR pen.
func (s *screen) penCell() terminal.Cell {
	return terminal.Cell{Rune: ' ', FG: s.fg, BG: s.bg, Attrs: s.attrs}
}

// ---------- dispatch ----------

// dispatch executes a parser action against the emulator's current screen.
func (e *Emulator) dispatch(a action, remaining []byte) {
	switch a.kind {
	case actPrint:
		e.printRune(rune(a.ch))
	case actExecute:
		e.executeC0(a.ch)
	case actCSI:
		e.dispatchCSI(a.final, a.params, a.intermediate)
	case actESC:
		e.dispatchESC(a.final, a.intermediate)
	case actOSC, actDCS:
		// Discard: OSC/DCS strings are parsed and ignored.
	}
}

// ---------- printable character ----------

func (e *Emulator) printRune(r rune) {
	// Note: wide-rune (CJK, emoji) width is treated as 1 for M4 simplicity.
	// Correctness of East-Asian Width is not required for M4.
	s := e.cur
	if s.cx >= s.cols {
		// Autowrap: move to the next line.
		s.cx = 0
		e.lineFeed()
	}
	c := terminal.Cell{Rune: r, FG: s.fg, BG: s.bg, Attrs: s.attrs}
	s.cells[s.cy][s.cx] = c
	s.cx++
	e.dirty = true
}

// ---------- C0 controls ----------

func (e *Emulator) executeC0(b byte) {
	s := e.cur
	switch b {
	case '\r': // CR
		s.cx = 0
		e.dirty = true
	case '\n', '\v', '\f': // LF / VT / FF
		e.lineFeed()
	case '\t': // HT — advance to next 8-column tab stop
		next := (s.cx/8 + 1) * 8
		if next >= s.cols {
			next = s.cols - 1
		}
		s.cx = next
		e.dirty = true
	case '\b': // BS
		if s.cx > 0 {
			s.cx--
			e.dirty = true
		}
	case '\a': // BEL — ignore
	}
}

// lineFeed moves the cursor down one line, scrolling the scroll region if needed.
func (e *Emulator) lineFeed() {
	s := e.cur
	_, bot := s.effectiveScrollRegion()
	top, _ := s.effectiveScrollRegion()
	if s.cy < bot {
		s.cy++
	} else if s.cy == bot {
		// Scroll up: remove top line of region, insert blank at bottom.
		e.scrollUp(top, bot, 1)
	}
	e.dirty = true
}

// scrollUp scrolls the region [top, bot] (0-based, inclusive) up by n lines.
// New blank lines are inserted at the bottom of the region.
func (e *Emulator) scrollUp(top, bot, n int) {
	s := e.cur
	if n <= 0 {
		return
	}
	if n > bot-top+1 {
		n = bot - top + 1
	}
	// Shift lines up.
	copy(s.cells[top:bot+1], s.cells[top+n:bot+1])
	// Blank the vacated lines at the bottom.
	for y := bot - n + 1; y <= bot; y++ {
		s.cells[y] = blankRow(s.cols)
	}
}

// scrollDown scrolls the region [top, bot] (0-based, inclusive) down by n lines.
// New blank lines are inserted at the top of the region.
func (e *Emulator) scrollDown(top, bot, n int) {
	s := e.cur
	if n <= 0 {
		return
	}
	if n > bot-top+1 {
		n = bot - top + 1
	}
	// Shift lines down.
	for y := bot; y >= top+n; y-- {
		s.cells[y] = s.cells[y-n]
	}
	// Blank the vacated lines at the top.
	for y := top; y < top+n; y++ {
		s.cells[y] = blankRow(s.cols)
	}
}

// ---------- ESC sequences ----------

func (e *Emulator) dispatchESC(final byte, inter []byte) {
	switch {
	case len(inter) == 0:
		switch final {
		case '7': // DECSC — save cursor
			e.saveCursor()
		case '8': // DECRC — restore cursor
			e.restoreCursor()
		case 'M': // RI — reverse index (scroll down)
			s := e.cur
			top, bot := s.effectiveScrollRegion()
			if s.cy > top {
				s.cy--
			} else {
				e.scrollDown(top, bot, 1)
			}
			e.dirty = true
		case 'c': // RIS — reset to initial state
			e.hardReset()
		}
		// All other ESC sequences are silently ignored.
	}
}

func (e *Emulator) saveCursor() {
	s := e.cur
	cs := &e.savedCursor
	if e.useAlt {
		cs = &e.savedCursorAlt
	}
	cs.x, cs.y = s.cx, s.cy
	cs.fg, cs.bg, cs.attrs = s.fg, s.bg, s.attrs
}

func (e *Emulator) restoreCursor() {
	s := e.cur
	cs := &e.savedCursor
	if e.useAlt {
		cs = &e.savedCursorAlt
	}
	s.cx, s.cy = cs.x, cs.y
	s.fg, s.bg, s.attrs = cs.fg, cs.bg, cs.attrs
	e.clampCursor()
	e.dirty = true
}

func (e *Emulator) clampCursor() {
	s := e.cur
	if s.cx >= s.cols {
		s.cx = s.cols - 1
	}
	if s.cy >= s.rows {
		s.cy = s.rows - 1
	}
	if s.cx < 0 {
		s.cx = 0
	}
	if s.cy < 0 {
		s.cy = 0
	}
}

func (e *Emulator) hardReset() {
	e.primary = newScreen(e.cols, e.rows)
	e.alt = newScreen(e.cols, e.rows)
	if e.useAlt {
		e.cur = &e.alt
	} else {
		e.cur = &e.primary
	}
	e.cursorVisible = true
	e.dirty = true
}

// ---------- CSI sequences ----------

func (e *Emulator) dispatchCSI(final byte, params []int, inter []byte) {
	s := e.cur
	p := func(idx, def int) int {
		if idx < len(params) && params[idx] != 0 {
			return params[idx]
		}
		return def
	}
	// p0 is like p but treats 0 as 0 (not defaulted). Used when 0 is meaningful.
	p0 := func(idx int) int {
		if idx < len(params) {
			return params[idx]
		}
		return 0
	}

	switch {
	case len(inter) == 0:
		switch final {
		case 'A': // CUU — cursor up
			n := p(0, 1)
			s.cy -= n
			if s.cy < 0 {
				s.cy = 0
			}
			e.dirty = true
		case 'B': // CUD — cursor down
			n := p(0, 1)
			s.cy += n
			if s.cy >= s.rows {
				s.cy = s.rows - 1
			}
			e.dirty = true
		case 'C': // CUF — cursor forward (right)
			n := p(0, 1)
			s.cx += n
			if s.cx >= s.cols {
				s.cx = s.cols - 1
			}
			e.dirty = true
		case 'D': // CUB — cursor back (left)
			n := p(0, 1)
			s.cx -= n
			if s.cx < 0 {
				s.cx = 0
			}
			e.dirty = true
		case 'E': // CNL — cursor next line
			n := p(0, 1)
			s.cy += n
			if s.cy >= s.rows {
				s.cy = s.rows - 1
			}
			s.cx = 0
			e.dirty = true
		case 'F': // CPL — cursor preceding line
			n := p(0, 1)
			s.cy -= n
			if s.cy < 0 {
				s.cy = 0
			}
			s.cx = 0
			e.dirty = true
		case 'G': // CHA — cursor horizontal absolute
			col := p(0, 1) - 1
			if col < 0 {
				col = 0
			}
			if col >= s.cols {
				col = s.cols - 1
			}
			s.cx = col
			e.dirty = true
		case 'H', 'f': // CUP / HVP — cursor position
			row := p(0, 1) - 1
			col := p(1, 1) - 1
			if row < 0 {
				row = 0
			}
			if col < 0 {
				col = 0
			}
			if row >= s.rows {
				row = s.rows - 1
			}
			if col >= s.cols {
				col = s.cols - 1
			}
			s.cy, s.cx = row, col
			e.dirty = true
		case 'J': // ED — erase in display
			e.eraseInDisplay(p0(0))
		case 'K': // EL — erase in line
			e.eraseInLine(p0(0))
		case 'L': // IL — insert lines
			n := p(0, 1)
			top, bot := s.effectiveScrollRegion()
			if s.cy >= top && s.cy <= bot {
				e.scrollDown(s.cy, bot, n)
			}
			e.dirty = true
		case 'M': // DL — delete lines
			n := p(0, 1)
			top, bot := s.effectiveScrollRegion()
			if s.cy >= top && s.cy <= bot {
				e.scrollUp(s.cy, bot, n)
			}
			e.dirty = true
		case 'P': // DCH — delete characters
			n := p(0, 1)
			e.deleteChars(n)
		case 'S': // SU — scroll up
			n := p(0, 1)
			top, bot := s.effectiveScrollRegion()
			e.scrollUp(top, bot, n)
			e.dirty = true
		case 'T': // SD — scroll down
			n := p(0, 1)
			top, bot := s.effectiveScrollRegion()
			e.scrollDown(top, bot, n)
			e.dirty = true
		case 'X': // ECH — erase characters
			n := p(0, 1)
			e.eraseChars(n)
		case 'd': // VPA — vertical position absolute
			row := p(0, 1) - 1
			if row < 0 {
				row = 0
			}
			if row >= s.rows {
				row = s.rows - 1
			}
			s.cy = row
			e.dirty = true
		case 'm': // SGR — select graphic rendition
			applySGR(s, params)
			// SGR changes the pen, not a cell, so no dirty mark needed.
		case 'r': // DECSTBM — set scrolling region
			top := p(0, 1)
			bot := p(1, s.rows)
			if top < 1 {
				top = 1
			}
			if bot > s.rows {
				bot = s.rows
			}
			if top < bot {
				s.scrollTop = top
				s.scrollBottom = bot
				// Move cursor to home on DECSTBM.
				s.cx, s.cy = 0, 0
				e.dirty = true
			}
		case 's': // SCP / ANSI SC — save cursor position
			e.saveCursor()
		case 'u': // RCP / ANSI RC — restore cursor position
			e.restoreCursor()
		case '@': // ICH — insert characters (blank cells)
			n := p(0, 1)
			e.insertChars(n)
		case 'h': // SM — set mode (only private handled below)
		case 'l': // RM — reset mode (only private handled below)
		}

	case len(inter) == 1 && inter[0] == '?':
		// DEC private modes.
		switch final {
		case 'h': // set mode
			for _, pm := range params {
				e.setDecMode(pm, true)
			}
		case 'l': // reset mode
			for _, pm := range params {
				e.setDecMode(pm, false)
			}
		}
	}
}

func (e *Emulator) setDecMode(mode int, set bool) {
	switch mode {
	case 25: // DECTCEM — cursor visibility
		e.cursorVisible = set
		e.dirty = true
	case 47, 1047: // alternate screen (simple)
		e.switchAltScreen(set)
	case 1049: // alternate screen + save/restore cursor
		if set {
			e.saveCursor()
			e.switchAltScreen(true)
			e.eraseInDisplay(2) // clear alt screen on enter
		} else {
			e.switchAltScreen(false)
			e.restoreCursor()
		}
	}
}

func (e *Emulator) switchAltScreen(useAlt bool) {
	if e.useAlt == useAlt {
		return
	}
	e.useAlt = useAlt
	if useAlt {
		e.cur = &e.alt
	} else {
		e.cur = &e.primary
	}
	e.dirty = true
}

// ---------- Erase operations ----------

func (e *Emulator) eraseInDisplay(mode int) {
	s := e.cur
	switch mode {
	case 0: // erase from cursor to end of screen
		// Clear remainder of current row.
		for x := s.cx; x < s.cols; x++ {
			s.cells[s.cy][x] = s.penCell()
		}
		// Clear all rows below.
		for y := s.cy + 1; y < s.rows; y++ {
			s.cells[y] = blankPenRow(s)
		}
	case 1: // erase from start of screen to cursor
		// Clear all rows above.
		for y := 0; y < s.cy; y++ {
			s.cells[y] = blankPenRow(s)
		}
		// Clear beginning of current row.
		for x := 0; x <= s.cx; x++ {
			s.cells[s.cy][x] = s.penCell()
		}
	case 2, 3: // erase entire screen
		for y := 0; y < s.rows; y++ {
			s.cells[y] = blankPenRow(s)
		}
	}
	e.dirty = true
}

func blankPenRow(s *screen) []terminal.Cell {
	row := make([]terminal.Cell, s.cols)
	pen := s.penCell()
	for i := range row {
		row[i] = pen
	}
	return row
}

func (e *Emulator) eraseInLine(mode int) {
	s := e.cur
	switch mode {
	case 0: // erase from cursor to end of line
		for x := s.cx; x < s.cols; x++ {
			s.cells[s.cy][x] = s.penCell()
		}
	case 1: // erase from start of line to cursor
		for x := 0; x <= s.cx; x++ {
			s.cells[s.cy][x] = s.penCell()
		}
	case 2: // erase entire line
		for x := 0; x < s.cols; x++ {
			s.cells[s.cy][x] = s.penCell()
		}
	}
	e.dirty = true
}

func (e *Emulator) eraseChars(n int) {
	s := e.cur
	for i := 0; i < n && s.cx+i < s.cols; i++ {
		s.cells[s.cy][s.cx+i] = s.penCell()
	}
	e.dirty = true
}

// ---------- Insert/delete character operations ----------

func (e *Emulator) insertChars(n int) {
	s := e.cur
	// Clamp n so row[s.cx+n:] is always within bounds.
	max := s.cols - s.cx
	if n < 1 {
		n = 1
	}
	if n > max {
		n = max
	}
	if max <= 0 {
		return
	}
	row := s.cells[s.cy]
	// Shift existing chars to the right, truncating at cols.
	copy(row[s.cx+n:], row[s.cx:])
	// Fill inserted positions with blank pen cells.
	for i := 0; i < n; i++ {
		row[s.cx+i] = s.penCell()
	}
	e.dirty = true
}

func (e *Emulator) deleteChars(n int) {
	s := e.cur
	// Clamp n so row[s.cx+n:] is always within bounds.
	max := s.cols - s.cx
	if n < 1 {
		n = 1
	}
	if n > max {
		n = max
	}
	if max <= 0 {
		return
	}
	row := s.cells[s.cy]
	// Shift existing chars to the left.
	copy(row[s.cx:], row[s.cx+n:])
	// Fill vacated positions at the end with blank pen cells.
	for i := s.cols - n; i < s.cols; i++ {
		row[i] = s.penCell()
	}
	e.dirty = true
}
