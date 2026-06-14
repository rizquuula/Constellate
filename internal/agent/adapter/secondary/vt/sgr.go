package vt

import "github.com/rizquuula/Constellate/internal/agent/domain/terminal"

// applySGR applies SGR (Select Graphic Rendition) parameters to the screen's
// current pen attributes (fg, bg, attrs). The params slice corresponds to
// the integers in CSI ... m. An empty params slice, or a single zero param,
// means reset.
//
// Supported codes:
//   0        — reset all
//   1        — bold
//   2        — faint
//   3        — italic
//   4        — underline
//   5        — blink (slow)
//   7        — inverse
//   8        — hidden (conceal)
//   9        — strike-through
//   22       — normal intensity (remove bold + faint)
//   23       — remove italic
//   24       — remove underline
//   25       — remove blink
//   27       — remove inverse
//   28       — remove hidden
//   29       — remove strikethrough
//   30–37    — set ANSI foreground colour (palette 0–7)
//   38;5;n   — set 256-colour foreground
//   38;2;r;g;b — set truecolour foreground
//   39       — default foreground
//   40–47    — set ANSI background colour (palette 0–7)
//   48;5;n   — set 256-colour background
//   48;2;r;g;b — set truecolour background
//   49       — default background
//   90–97    — bright ANSI foreground (palette 8–15)
//   100–107  — bright ANSI background (palette 8–15)

func applySGR(s *screen, params []int) {
	if len(params) == 0 {
		resetSGR(s)
		return
	}

	i := 0
	for i < len(params) {
		code := params[i]
		i++

		switch {
		case code == 0:
			resetSGR(s)

		// Attributes — set.
		case code == 1:
			s.attrs |= terminal.AttrBold
		case code == 2:
			s.attrs |= terminal.AttrFaint
		case code == 3:
			s.attrs |= terminal.AttrItalic
		case code == 4:
			s.attrs |= terminal.AttrUnderline
		case code == 5, code == 6: // 6 = rapid blink, treat same as blink
			s.attrs |= terminal.AttrBlink
		case code == 7:
			s.attrs |= terminal.AttrInverse
		case code == 8:
			s.attrs |= terminal.AttrHidden
		case code == 9:
			s.attrs |= terminal.AttrStrike

		// Attributes — reset.
		case code == 22:
			s.attrs &^= terminal.AttrBold | terminal.AttrFaint
		case code == 23:
			s.attrs &^= terminal.AttrItalic
		case code == 24:
			s.attrs &^= terminal.AttrUnderline
		case code == 25:
			s.attrs &^= terminal.AttrBlink
		case code == 27:
			s.attrs &^= terminal.AttrInverse
		case code == 28:
			s.attrs &^= terminal.AttrHidden
		case code == 29:
			s.attrs &^= terminal.AttrStrike

		// Foreground: ANSI 8 colours (30–37).
		case code >= 30 && code <= 37:
			s.fg = paletteIndex(code - 30)

		// Foreground: extended colour.
		case code == 38:
			n, newFG := parseExtendedColor(params, i)
			i += n
			s.fg = newFG

		// Foreground: default.
		case code == 39:
			s.fg = terminal.ColorDefault

		// Background: ANSI 8 colours (40–47).
		case code >= 40 && code <= 47:
			s.bg = paletteIndex(code - 40)

		// Background: extended colour.
		case code == 48:
			n, newBG := parseExtendedColor(params, i)
			i += n
			s.bg = newBG

		// Background: default.
		case code == 49:
			s.bg = terminal.ColorDefault

		// Bright foreground: ANSI colours 8–15 (90–97).
		case code >= 90 && code <= 97:
			s.fg = paletteIndex(code - 90 + 8)

		// Bright background: ANSI colours 8–15 (100–107).
		case code >= 100 && code <= 107:
			s.bg = paletteIndex(code - 100 + 8)

		// All other codes are silently ignored.
		}
	}
}

// resetSGR resets all graphic attributes to their defaults.
func resetSGR(s *screen) {
	s.fg = terminal.ColorDefault
	s.bg = terminal.ColorDefault
	s.attrs = 0
}

// paletteIndex converts a 0-based ANSI colour index (0–255) to the Cell
// FG/BG encoding: palette index+1 (so 0 remains ColorDefault).
func paletteIndex(idx int) int {
	return idx + 1 // 1..256
}

// parseExtendedColor parses 256-colour or truecolour sub-parameters that
// follow a code 38 or 48. It reads from params[start:] and returns how many
// extra elements were consumed and the encoded colour value.
//
// Supported forms:
//   38;5;n          — 256-colour (xterm palette)
//   38;2;r;g;b      — truecolour (24-bit RGB)
func parseExtendedColor(params []int, start int) (consumed int, color int) {
	if start >= len(params) {
		return 0, terminal.ColorDefault
	}
	switch params[start] {
	case 5: // 256-colour: next param is index 0–255
		if start+1 < len(params) {
			idx := params[start+1]
			if idx < 0 {
				idx = 0
			}
			if idx > 255 {
				idx = 255
			}
			return 2, paletteIndex(idx)
		}
		return 1, terminal.ColorDefault

	case 2: // truecolour: next three params are R, G, B
		if start+3 < len(params) {
			r := clamp8(params[start+1])
			g := clamp8(params[start+2])
			b := clamp8(params[start+3])
			return 4, terminal.ColorTruecolorFlag | (r << 16) | (g << 8) | b
		}
		return 1, terminal.ColorDefault

	default:
		return 1, terminal.ColorDefault
	}
}

func clamp8(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}
