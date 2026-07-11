// 20 visually-distinct colors, ordered so adjacent indices contrast strongly.
// Window color is keyed to window POSITION (1-based), wrapping every 20.
export const WINDOW_PALETTE = [
  '#e6194b', '#3cb44b', '#4363d8', '#f58231', '#911eb4',
  '#ffe119', '#42d4f4', '#f032e6', '#469990', '#9a6324',
  '#808000', '#000075', '#fabed4', '#aaffc3', '#bfef45',
  '#ffd8b1', '#800000', '#dcbeff', '#a9a9a9', '#fffac8',
] as const

function fgFor(hex: string): string {
  const n = parseInt(hex.slice(1), 16)
  const r = (n >> 16) & 255, g = (n >> 8) & 255, b = n & 255
  const lum = 0.299 * r + 0.587 * g + 0.114 * b // perceived luminance, 0..255
  return lum > 140 ? '#1a1a1a' : '#ffffff'
}

// ordinal = window tab position, 1-based.
export function windowColor(ordinal: number): { bg: string; fg: string } {
  const bg = WINDOW_PALETTE[(ordinal - 1) % WINDOW_PALETTE.length]
  return { bg, fg: fgFor(bg) }
}
