# Constellate

> A self-hosted control plane for a fleet of developer machines: one web UI, served from a public
> hub, giving a single operator live terminal access to every machine they own — organized by
> project, persistent across reconnects, with a mission-control overview of every running terminal at
> a glance.

**Status:** all roadmap features complete · **Module:** `github.com/rizquuula/Constellate` ·
**Go:** 1.25+

See [`DESIGN.md`](DESIGN.md) for the canonical architecture and the full roadmap.

---

## What works today

A **persistent, live interactive shell in the browser**, end to end — plus the mission-control,
project, and auth layers on top:

- **Agents dial home** (outbound only) to the hub over a WebSocket, run **yamux** over it, and send
  `Hello` + periodic `Heartbeat` on a control stream — reconnecting automatically with backoff.
- The **hub** registers agents in a live `machineID → connection` registry, persists machine, project,
  session, and audit metadata to **SQLite**, and brokers a browser WebSocket ↔ a yamux **data stream**
  ↔ a PTY on the agent.
- The **agent** spawns a real PTY per session, keeps a **bounded scrollback buffer**, pipes raw bytes
  both ways, and applies resizes. PTYs **survive a tab close**; re-attaching **replays scrollback** —
  history repaints instantly, then continues live. An agent **process restart** marks its orphaned
  sessions **`lost`**.
- **Projects** group sessions (across machines) into a project-grouped sidebar; sessions may be
  ungrouped. The terminal UI is a **recursive split-pane workspace** — many live shells visible at once.
- A **mission-control overview** renders every live terminal as a colored tile from rate-capped screen
  snapshots; **click a tile to dive** into the full interactive session.
- A **progress dashboard** rolls up fleet/per-machine/per-project status, an attention list, and recent
  audit events, plus per-session **activity** badges (active / idle / **needs input**).
- **Auth + TLS**: agents enroll with an **Ed25519 keypair** (the hub stores only the public key); the
  operator logs in with **TOTP + recovery codes + WebAuthn passkeys**; all `/api/*` and `/ws/*` routes
  are gated by a session cookie; the hub serves HTTPS directly or behind Caddy.

- A **React + xterm.js** app (embedded in the hub binary) serves all of the above.

> **Security note:** the hub is a remote-code-execution gateway to every enrolled machine. It is built
> to face the public internet — over HTTPS, behind operator auth — but treat the deployment
> accordingly. See [`DESIGN.md`](DESIGN.md) §10 for the threat model.

---

## Prerequisites

- **Go 1.25+.** If your system Go is older, set `GOTOOLCHAIN=auto` (the `Makefile` already does) so the
  toolchain is fetched automatically.
- **Node 18+ / npm** — to build the web app (`make web`, run automatically by `make build`).
- **Docker + Compose v2** — only for the Dockerized topology test (`make test-docker`).

## Quickstart

The fastest way to try it is the **one-command Docker demo** — it builds the images, bootstraps an
operator, enrolls two agent "machines", and prints a login code:

```bash
./deploy/dev-up.sh
open http://localhost:8080      # log in with the printed code → pick an agent → "New shell"
```

To run the **binaries directly** (two terminals), the flow is: build, start the hub, bootstrap an
operator, mint an enrollment token, enroll the agent, then run it:

```bash
make build                                             # builds both binaries into ./bin

./bin/constellate-hub serve                            # Terminal 1: serves on 127.0.0.1:8080
./bin/constellate-hub operator add                     # bootstrap: prints TOTP URI + recovery codes
./bin/constellate-hub enroll-token                     # mint a one-time agent enrollment token

./bin/constellate-agent enroll --hub http://127.0.0.1:8080 --token <token>   # Terminal 2: one-time
./bin/constellate-agent connect                        # dial home and serve
```

Then open <http://127.0.0.1:8080>, log in with your 6-digit TOTP code, and the machine appears
**online** in the sidebar. Click **New shell** to open a live terminal — type, run `ls`/`top`, resize.
Close the tab and re-open it; the shell is still running and **its history repaints instantly**.

See **[`docs/usage.binary.md`](docs/usage.binary.md)** and **[`docs/usage.docker.md`](docs/usage.docker.md)**
for the full walkthroughs (config, TLS, passkeys, multiple machines). Configuration comes from a YAML
file (`--config`; samples in [`configs/`](configs/)); per-secret `CONSTELLATE_*` env vars override
file values.

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
