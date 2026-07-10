// Working-directory display helpers for the pane header.

// cropPwd shortens a working-directory path for the pane header, keeping only
// the last 8 characters behind a leading ellipsis so long paths stay compact.
// Paths of 8 characters or fewer are returned unchanged (no ellipsis).
//
//   cropPwd('/home/amm/dev/Constellate') → '…stellate'
//   cropPwd('/home/amm/dev/api')          → '…/dev/api'  (exactly 8 kept)
//   cropPwd('/etc')                       → '/etc'       (shorter than 8)
//   cropPwd('')                           → ''
export function cropPwd(p: string): string {
  return p.length <= 8 ? p : '…' + p.slice(-8)
}
