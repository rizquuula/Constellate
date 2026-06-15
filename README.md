# Constellate

> A self-hosted control plane for a fleet of developer machines: one web UI, served from a public
> hub, giving a single operator live terminal access to every machine they own — organized by
> project, persistent across reconnects, with a mission-control overview of every running terminal at
> a glance.

**Status:** M2 (persistent terminals) · **Module:** `github.com/rizquuula/Constellate` ·
**Go:** 1.25+

See [`DESIGN.md`](DESIGN.md) for the canonical architecture and the full milestone roadmap.

---

## What works today (through M2)

A **persistent, live interactive shell in the browser**, end to end:

- **Agents dial home** (outbound only) to the hub over a WebSocket, run **yamux** over it, and send
  `Hello` + periodic `Heartbeat` on a control stream — reconnecting automatically with backoff.
- The **hub** registers agents in a live `machineID → connection` registry, persists machine + session
  metadata to **SQLite**, and brokers a browser WebSocket ↔ a yamux **data stream** ↔ a PTY on the agent.
- The **agent** spawns a real PTY per session, keeps a **bounded scrollback buffer**, pipes raw bytes
  both ways, and applies resizes.
- A **React + xterm.js** app (embedded in the hub binary) lets you pick an online machine, open a shell,
  type, see output, and resize. PTYs **survive a tab close**, and **re-attaching or switching sessions
  replays scrollback** — history repaints instantly, then continues live.
- When an agent **process restarts**, its orphaned sessions are marked **`lost`** (the session list
  stays honest); a transient reconnect of the same process does not.

No mission-control overview, projects, or auth hardening yet — those arrive in M3–M5 (see `DESIGN.md` §18).

> **Security note:** M0 runs on `localhost` / a private network only, over plain `ws://`. The hub does
> **not** face the public internet until the auth milestone (M5). The `CONSTELLATE_DEV_TOKEN` path is
> a development shortcut, removed at M5.

---

## Prerequisites

- **Go 1.25+.** If your system Go is older, set `GOTOOLCHAIN=auto` (the `Makefile` already does) so the
  toolchain is fetched automatically.
- **Node 18+ / npm** — to build the web app (`make web`, run automatically by `make build`).
- **Docker + Compose v2** — only for the Dockerized topology test (`make test-docker`).

## Quickstart (two terminals)

```bash
# Build both binaries (version-stamped) into ./bin
make build

# Terminal 1 — run the hub (auto-migrates SQLite, serves on 127.0.0.1:8080)
CONSTELLATE_DEV_TOKEN=devtoken ./bin/constellate-hub serve

# Terminal 2 — run an agent that dials home
CONSTELLATE_HUB_URL=ws://127.0.0.1:8080/ws/agent \
CONSTELLATE_DEV_TOKEN=devtoken \
CONSTELLATE_NAME=my-laptop \
  ./bin/constellate-agent connect
```

Then open <http://127.0.0.1:8080>: the machine appears **online** in the sidebar. Click **New shell**
to open a live terminal — type, run `ls`/`top`, resize the window. Close the tab (or switch to another
session) and re-open it; the shell is still running and **its history repaints instantly** before
continuing live.

Stop the agent (Ctrl-C) and the machine flips offline; restart it and it comes back with the same id.

Configuration can also come from a YAML file (`--config`); see [`configs/`](configs/) for samples.
Per-secret `CONSTELLATE_*` env vars override file values.

## CLI

```
constellate-hub   serve | migrate | version      # serve is the default
constellate-agent connect | status | version     # connect is the default
```

Both `version` commands print the binary version, git commit, and wire protocol version.

## Testing

```bash
make test          # unit + integration + in-process E2E (dial-home + the full terminal lifecycle)
make test-e2e      # Playwright: a real browser opens a shell on an agent, types, reads, resizes
make test-docker   # hub + 2 agent containers on a Docker network — dial-home across real boundaries
make lint          # golangci-lint
```

Acceptance checks: **online → offline → online** (`test/integration/topology_test.go`, `test/docker/`)
and the **terminal lifecycle** — create → attach → type → read → resize → detach → re-attach → close —
both in-process (`test/integration/terminal_test.go`) and in a real browser (`test/e2e/`).

**CI:** the cheap checks (lint, vet, race tests, frontend typecheck/build) run automatically on push/PR
(`.github/workflows/ci.yaml`). The heavy tiers (Playwright browser + Docker topology) are
**manual-trigger only** (`e2e.yaml`, run from the Actions tab) to conserve Actions minutes.

## Shell integration (activity badges)

Constellate shows per-session activity (`active` / `idle` / `awaiting input`) in the sidebar,
overview grid, and dashboard. Accuracy improves with optional OSC 133 prompt markers — see
[`docs/shell-integration.md`](docs/shell-integration.md) for setup snippets (bash + zsh).

## Layout

Two bounded contexts in one module, each its own hexagon (`internal/hub`, `internal/agent`), sharing
only `internal/transport` (the wire protocol) and `internal/platform` (logging, ids, config, version).
See `DESIGN.md` §11–§12 for the full layering and folder tree.
