# Constellate

> A self-hosted control plane for a fleet of developer machines: one web UI, served from a public
> hub, giving a single operator live terminal access to every machine they own — organized by
> project, persistent across reconnects, with a mission-control overview of every running terminal at
> a glance.

**Status:** M0 (scaffold + dial-home topology) · **Module:** `github.com/rizquuula/Constellate` ·
**Go:** 1.25+

See [`DESIGN.md`](DESIGN.md) for the canonical architecture and the full milestone roadmap.

---

## What works today (M0)

M0 stands up the monorepo + hexagonal skeleton and **proves the dial-home topology end to end**:

- **Agents dial home** (outbound only) to the hub over a WebSocket, run **yamux** over it, and send
  `Hello` + periodic `Heartbeat` on a control stream — reconnecting automatically with backoff.
- The **hub** authenticates agents (shared dev token, M0), registers them in a live
  `machineID → connection` registry, persists machine metadata to **SQLite**, and serves
  `GET /api/machines` plus an embedded status page.
- A machine shows **online** while its connection is live and flips **offline** the moment it drops.

No PTYs, terminals, web app, or auth hardening yet — those arrive in M1–M5 (see `DESIGN.md` §18).

> **Security note:** M0 runs on `localhost` / a private network only, over plain `ws://`. The hub does
> **not** face the public internet until the auth milestone (M5). The `CONSTELLATE_DEV_TOKEN` path is
> a development shortcut, removed at M5.

---

## Prerequisites

- **Go 1.25+.** If your system Go is older, set `GOTOOLCHAIN=auto` (the `Makefile` already does) so the
  toolchain is fetched automatically.
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

Then open <http://127.0.0.1:8080> to watch the machine appear **online**, or:

```bash
curl -s 127.0.0.1:8080/api/machines    # → [{"id":"...","name":"my-laptop","online":true,...}]
```

Stop the agent (Ctrl-C) and the machine flips to `"online":false`; restart it and it comes back online
with the same id.

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
make test          # unit + integration + in-process E2E (online→offline→online over loopback)
make test-docker   # hub + 2 agent containers on a Docker network — dial-home across real boundaries
make lint          # golangci-lint
```

The M0 acceptance check — **online → offline → online** — runs both in-process
(`test/integration/topology_test.go`) and across containers (`test/docker/`).

## Layout

Two bounded contexts in one module, each its own hexagon (`internal/hub`, `internal/agent`), sharing
only `internal/transport` (the wire protocol) and `internal/platform` (logging, ids, config, version).
See `DESIGN.md` §11–§12 for the full layering and folder tree.
