# Hub keyboard shortcuts

Keyboard shortcuts for the Constellate web UI (the hub). All shortcuts use
**physical key positions** (`KeyboardEvent.code`), so they work regardless of
keyboard layout or the character a modifier rewrites the key into.

## View switching

Jump straight to a top-level view. Works from anywhere once signed in.

| Shortcut | Action |
|---|---|
| `Alt`/`⌘` + `1` | Workspace |
| `Alt`/`⌘` + `2` | Overview |
| `Alt`/`⌘` + `3` | Dashboard |

## Pane controls (Workspace)

These act on the **focused pane** and only fire in the **Workspace** view. They
work even while a terminal is focused — the shortcut is captured before xterm
sees the keystroke, so it won't be swallowed or sent to the shell.

| Shortcut | Action |
|---|---|
| `Shift` + `Alt` + `-` | Split the focused pane **horizontally** (side by side) |
| `Shift` + `Alt` + `=` | Split the focused pane **vertically** (stacked) |
| `Shift` + `Alt` + `W` | **Close** the focused pane |
| `Shift` + `Alt` + `E` | **Detach** the session from the focused pane (keeps it running in the sidebar; leaves an empty pane) |

Notes:

- **Detach** unbinds the session from the pane without closing the shell — the
  session keeps running and stays reachable from the sidebar. The pane remains in
  the layout as an empty leaf.
- **Close** removes the pane from the layout; the sibling pane expands to fill
  the space. Closing the last remaining pane leaves a single empty pane.
- The same actions are available from the per-pane buttons in the pane title bar
  (split ▥/▤, detach ⏏, close ✕).
