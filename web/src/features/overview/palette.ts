// xterm-256 palette → CSS hex strings.
// Index 0–15: ANSI 16 colours (dark then light variants).
// Index 16–231: 6×6×6 colour cube.
// Index 232–255: 24-step grayscale ramp.

const ANSI16: string[] = [
  '#000000', '#aa0000', '#00aa00', '#aa5500',
  '#0000aa', '#aa00aa', '#00aaaa', '#aaaaaa',
  '#555555', '#ff5555', '#55ff55', '#ffff55',
  '#5555ff', '#ff55ff', '#55ffff', '#ffffff',
]

function buildPalette(): string[] {
  const p: string[] = new Array(256)

  // 0–15: ANSI 16
  for (let i = 0; i < 16; i++) p[i] = ANSI16[i]

  // 16–231: 6×6×6 cube
  const levels = [0, 95, 135, 175, 215, 255]
  for (let i = 0; i < 216; i++) {
    const r = levels[Math.floor(i / 36) % 6]
    const g = levels[Math.floor(i / 6) % 6]
    const b = levels[i % 6]
    p[16 + i] = `#${r.toString(16).padStart(2, '0')}${g.toString(16).padStart(2, '0')}${b.toString(16).padStart(2, '0')}`
  }

  // 232–255: grayscale ramp
  for (let i = 0; i < 24; i++) {
    const v = 8 + i * 10
    const hex = v.toString(16).padStart(2, '0')
    p[232 + i] = `#${hex}${hex}${hex}`
  }

  return p
}

export const PALETTE: string[] = buildPalette()

// Terminal default colors matching the xterm theme in useTerminal.ts
export const DEFAULT_FG = '#e0e0e0'
export const DEFAULT_BG = '#0f0f13'

// Decode a packed color value to a CSS color string or null for default.
// 0 / undefined → null (use default)
// 1..256        → PALETTE[value - 1]
// >= 0x1000000  → truecolor rgb = value & 0xFFFFFF
export function decodeColor(value: number | undefined): string | null {
  if (!value || value === 0) return null
  if (value >= 0x1000000) {
    const rgb = value & 0xffffff
    const r = (rgb >> 16) & 0xff
    const g = (rgb >> 8) & 0xff
    const b = rgb & 0xff
    return `#${r.toString(16).padStart(2, '0')}${g.toString(16).padStart(2, '0')}${b.toString(16).padStart(2, '0')}`
  }
  // palette index 1-based
  const idx = value - 1
  if (idx >= 0 && idx < 256) return PALETTE[idx]
  return null
}

// Attrs bitmask constants
export const ATTR_BOLD      = 1
export const ATTR_FAINT     = 2
export const ATTR_ITALIC    = 4
export const ATTR_UNDERLINE = 8
export const ATTR_BLINK     = 16
export const ATTR_INVERSE   = 32
export const ATTR_HIDDEN    = 64
export const ATTR_STRIKE    = 128
