# Constellate — Design

> A self-hosted control plane for a fleet of developer machines: one web UI, served from a
> public hub, giving a single operator live terminal access to every machine they own —
> organized by project, persistent across reconnects, with a mission-control overview of every
> running terminal at a glance.

**Status:** M0–M7 complete + project delete (post-M7); full test matrix (unit/integration/in-proc E2E, `make test-e2e`, `make test-docker`) run and passing · **Module:** `github.com/rizquuula/Constellate` · **Go:** 1.25+

---

## Table of contents
1. [Problem & motivation](#1-problem--motivation)
2. [Goals / Non-goals](#2-goals--non-goals)
3. [Locked decisions](#3-locked-decisions)
4. [System architecture](#4-system-architecture)
5. [Connection & session lifecycle](#5-connection--session-lifecycle)
6. [Wire protocol](#6-wire-protocol)
7. [Persistence & the overview pipeline](#7-persistence--the-overview-pipeline)
8. [Data model](#8-data-model)
9. [HTTP / WebSocket API surface](#9-http--websocket-api-surface)
10. [Security model](#10-security-model)
11. [Hexagonal design & layering](#11-hexagonal-design--layering)
12. [Target folder tree & CLI commands](#12-target-folder-tree--cli-commands)
13. [Technology stack](#13-technology-stack)
14. [Configuration](#14-configuration)
15. [Observability](#15-observability)
16. [Testing & environments](#16-testing--environments)
17. [Build, release & deployment](#17-build-release--deployment)
18. [Milestone roadmap](#18-milestone-roadmap)
19. [Glossary](#19-glossary)

---

## 1. Problem & motivation

The operator works across **4 machines** and many projects. Today that means juggling terminal
tabs along two independent axes at once: one per project, and one per SSH session into each
machine. Switching context means hunting for the right tab, re-establishing SSH after a sleep or
network change, and losing track of what is running where.

Constellate collapses both axes into **one browser tab**: pick a machine or a project, get a live
shell; see every running terminal on a single overview; reconnect from any device without losing
in-flight work.

It is explicitly a **single-operator** tool for **machines you already own** — not a multi-tenant
PaaS, not an environment provisioner, not a web IDE.

---

## 2. Goals / Non-goals

### Goals (v1)
- **G1 — Web terminal per machine.** Open the web app, pick a machine, get a live interactive
  shell in the browser.
- **G2 — Persistent sessions.** Shells survive disconnects, tab close, laptop sleep, and switching
  client devices (tmux-like).
- **G3 — Projects.** Sessions are grouped by project (name + machine + working dir) across the
  fleet, not as a flat list of hosts.
- **G4 — Mission-control overview.** A single page renders every live terminal as a tile; click a
  tile to dive into the full interactive session.
- **G5 — Progress dashboard.** At-a-glance activity/status rollups across machines and projects.

### Non-goals
- Multi-user / teams / RBAC — single operator.
- Provisioning machines or environments — Constellate connects to *existing* machines; it is not
  Coder/Gitpod.
- A full web IDE — terminals and orchestration, not an editor.
- File sync / large file transfer — use scp/rsync.

### Quality bars
- A cold reconnect to a busy session restores scrollback and resumes live in **< 1 s** on a LAN.
- The overview stays responsive with **all 4 machines × ~10 sessions** visible.
- The agent is a **single static binary** with no runtime dependencies beyond a shell.
- Nothing faces the public internet until the security milestone (M5) lands.

---

## 3. Locked decisions

| # | Area | Choice | Why | Rejected alternative |
|---|------|--------|-----|----------------------|
| D1 | Topology | **Agents dial home** (outbound only) | Works behind NAT/residential firewalls; zero inbound ports on dev boxes; central auth | Hub SSHes *out* → needs reachable machines + parks keys on the public box |
| D2 | Languages | **Go** for hub + agent (one module) | One language, strong PTY/websocket/concurrency story, static binaries | Rust (slower iteration); split stacks (two toolchains) |
| D3 | Transport | **TLS WebSocket + yamux** mux | Simple, proxy-friendly, one transport family; many logical streams over one conn | gRPC (browser can't bidi-stream); NATS (extra infra); SSH broker (re-wraps what we replace) |
| D4 | Frontend | **React + xterm.js** | Largest ecosystem, best terminal/websocket precedent | Flutter web (immature xterm); SolidJS (small community) |
| D5 | Store | **SQLite** on the hub (metadata/history only) | Single VPS, single operator; zero extra services | Postgres (operational weight); bbolt (weak relational queries) |
| D6 | Persistence | ~~**In-process PTY** on the agent + scrollback buffer~~ **superseded by D8** | *See D8.* | — |
| D7 | Auth | **Passkey (WebAuthn) + TOTP** fallback | Phishing-resistant, no shared secret | Password (weak); OIDC (ties login to a provider) — kept as an option |
| D8 | Persistence | **Split agent: durable `session-host` (owns PTYs/scrollback/instanceID) + volatile `connect` relay** | Sessions survive a connect restart or `agent update`; only session-host death or machine reboot loses them. Pure-Go UDS relay — no extra deps, no tmux, no fd-passing. | tmux-backed PTYs (extra dep, external process lifecycle), SCM_RIGHTS fd-passing (non-portable, fragile), keeping the in-process model (restart-drops-sessions — D6's accepted trade-off, now rejected). *Supersedes D6.* |

Decisions D1–D8 are settled. The detail *below* each (protocol framing, schema, package
boundaries) is what this review is for.

---

## 4. System architecture

### 4.1 Components

**Hub** (Go, runs on the public VPS) — the only public surface.
- Serves the React app (embedded in the binary) and a JSON REST API over HTTPS.
- Terminates **two** browser WebSocket roles: terminal-attach and the overview feed.
- Accepts **inbound agent connections** (agents dial home) and authenticates them.
- **Brokers**: routes an authenticated browser terminal ↔ the right agent ↔ the right PTY.
- Owns the SQLite store, the machine/project/session registry, the audit log, and operator auth.
- Holds **no shells itself** — it is a pure control plane and relay.

**Agent** (Go, one static binary per machine) — outbound only. The agent process is split into two cooperating roles (D8):
- **`constellate-agent session-host`** (durable) — generates the machine's `instanceID` once at start, owns all PTYs and scrollback ring buffers, runs the vt emulator and snapshot producer, and listens on a Unix domain socket (UDS) under `$XDG_RUNTIME_DIR/constellate/host.sock` (or `~/.constellate/run/host.sock`). Survives `connect` restarts.
- **`constellate-agent connect`** (volatile) — dials home to the hub over a single multiplexed WebSocket (auto-reconnect/backoff); sources the `instanceID` from the session-host via a local handshake; relays all hub commands (OpenSession/Resize/CloseSession/EnableSnaps) to the session-host over the UDS; relays output, snapshots, and activity signals back to the hub. Safe to kill and restart — the session-host keeps running.
- Sessions survive browser disconnects **and** connect restarts: PTYs keep running in the host; re-attach replays scrollback then resumes live. Only session-host death (crash or machine reboot) loses sessions.

**Web app** (React + xterm.js, served by the hub).
- **Overview** grid (the signature view), project-grouped **sidebar**, live **terminal** view, and
  the **dashboard**.

### 4.2 Deployment topology

```
                         Public VPS
   ┌───────────────────────────────────────────────────────────┐
   │                          HUB                                │
   │   ┌─────────────────────────────────────────────────────┐  │
   │   │ httpapi    REST + embedded React app        (HTTPS)  │  │
   │   │ wsbrowser  /ws/term     browser terminal attach      │  │
   │   │ wsbrowser  /ws/overview snapshot feed         (M4)   │  │
   │   │ wsagent    /ws/agent    agent dial-home endpoint     │  │
   │   │ agentlink  live machineID → connection registry      │  │
   │   │ sqlite     machines · projects · sessions · audit    │  │
   │   └───────────────▲─────────────────────▲───────────────┘  │
   └───────────────────┼─────────────────────┼──────────────────┘
        browser (HTTPS │ WSS)                 │ one TLS WebSocket per agent
   ┌───────────────────┴───────┐             │ (outbound dial-home, yamux-muxed)
   │  Browser + xterm.js        │      ┌──────┴───────┬──────────────┬───────────┐
   │  overview · terminal · dash│      │              │              │           │
   └────────────────────────────┘  ┌───┴───┐     ┌────┴───┐     ┌────┴───┐   ┌───┴───┐
                                   │Machine1│     │Machine2│ ... │Machine4│   │  ...  │
                                   │ AGENT  │     │ AGENT  │     │ AGENT  │   │       │
                                   │ ├ PTYs │     │ ├ PTYs │     │ ├ PTYs │   │       │
                                   │ └ vt   │     │ └ vt   │     │ └ vt   │   │       │
                                   └────────┘     └────────┘     └────────┘   └───────┘
```

The hub **never initiates** connections into machines. Every machine link is an outbound dial from
the agent; the hub multiplexes browser sessions onto whichever agent connection is live.

### 4.3 Stream model (yamux over one WebSocket)

Each agent holds exactly one WebSocket to the hub. That socket is wrapped as a `net.Conn` and a
**yamux** session is run over it. Logical streams:

| Stream | Direction | Carries |
|--------|-----------|---------|
| **control** (first stream) | bidirectional | NDJSON control messages: `Hello`, `Heartbeat`, `OpenSession`, `Resize`, `CloseSession`, `SessionExited`, errors |
| **data** (one per attached session) | bidirectional | raw PTY bytes; first line is an `AttachHeader{sessionID}` |
| **snapshot** (M4, one per agent) | agent → hub | NDJSON full-color screen snapshots for the overview; opened by the agent, gated by `EnableSnaps` |

Multiplexing over one socket means a single TLS handshake, a single auth check, and one thing to
reconnect — while still giving every terminal its own independent, back-pressured byte pipe.

### 4.4 Local protocol (connect ⇄ session-host, D8)

Inside the machine, connect and session-host communicate over a **Unix domain socket** (UDS) using
the same yamux + NDJSON codec as the hub wire protocol — no second encoding layer. The local stream
model mirrors the hub model:

| Stream | Direction | Carries |
|--------|-----------|---------|
| **local-control** (first stream) | bidirectional | `HostHello` (connect→host), `HostInfo` (host→connect: instanceID + session list), `OpenSession`, `Resize`, `CloseSession`, `EnableSnaps`, `LocalStat` (host→connect: activity), errors |
| **local-data** (one per attached session) | connect-opened / host-accepted | `AttachHeader{sessionID}` + raw PTY bytes (same format as hub data streams) |
| **local-snapshot** (one per connect) | host-opened / connect-accepted | NDJSON `Snapshot` frames relayed to hub snapshot stream |

The local control stream opens with a version handshake: connect sends `HostHello{localProtocol}`,
host replies `HostInfo{instanceID, localProtocol, sessions}`. Both sides negotiate
`min(localProtocol)` and gate features on the negotiated version. `LocalProtocolVersion = 2`
(v1 = basic relay; v2 = adds `LocalStat` + host-side snapshot production).

The socket is at `<runtime_dir>/host.sock` (default `$XDG_RUNTIME_DIR/constellate/host.sock`),
created `0600` under a `0700` directory. The session-host verifies the connecting process's UID
via `SO_PEERCRED` (Linux) and accepts at most one client at a time.

---

## 5. Connection & session lifecycle

### 5.1 Agent enrollment (M5)
1. Operator runs `constellate-hub enroll-token [--ttl]`; the hub mints a one-time token (stores
   only its SHA-256 in `enroll_tokens`, migration 0003) and prints it.
2. Operator runs `constellate-agent enroll --hub <url> --token <tok>` on the target machine.
   The agent generates an **Ed25519 keypair**, POSTs the public key to `POST /api/enroll`
   (unauthenticated, protected solely by the single-use token). Token is now spent.
3. Hub stores the Ed25519 **public key** in `machine_credentials`, assigns a `machineID` (ULID),
   audits `enroll`, and returns the machineID. The agent writes the machineID to `id_file` and
   the PKCS8 PEM private key to `cred_file`.
4. Every later dial-home presents an **agent-signed bearer assertion** (see §6). Revocable from
   the hub with `constellate-hub revoke <machineID>` (soft: sets `machines.revoked_at`;
   `Authenticate` rejects revoked machines).

### 5.2 Dial-home
1. `connect` dials the session-host UDS and performs the local handshake (§4.4), sourcing the
   stable `instanceID` generated by the session-host at startup.
2. `connect` opens `wss://hub/ws/agent`, presenting its credential.
3. Hub validates, registers the connection in **agentlink** (`machineID → conn`), starts the yamux
   server side, accepts the control stream.
4. `connect` sends `Hello{…, instanceID, agentVersion, protocolVersion}`; the hub checks the protocol
   is in its supported range (else rejects with a clear error), upserts the machine, and marks it
   **online**. If the `instanceID` matches the previous connection, sessions stay `running`; a
   different `instanceID` (session-host was restarted) triggers `MarkMachineSessionsLost`.
5. `connect` sends `Heartbeat` every ~5 s with light session stats; hub updates `last_seen_at`.
6. On socket close or missed heartbeats (≥ N intervals), hub marks the machine **offline** and
   tears down routes; `connect` reconnects with exponential backoff + jitter (cap ~30 s).

### 5.3 Open / attach a terminal
1. Browser opens `wss://hub/ws/term?session=<id|new>` (authenticated).
2. If new: the **attach** use case asks **agentlink** to `OpenSession{sessionID, cwd, shell,
   cols, rows}`; the agent spawns a PTY and replies `SessionOpened`.
3. Hub opens a yamux **data** stream to the agent, writes `AttachHeader{sessionID}`.
4. Agent matches the stream to the PTY, **replays the scrollback buffer**, then pipes PTY ↔ stream.
5. Hub pipes browser WS ↔ data stream (binary, bidirectional). Keystrokes flow up, output flows
   down.
6. **Resize**: browser → hub → control `Resize{sessionID, cols, rows}` → agent applies `TIOCSWINSZ`.

### 5.4 Detach vs close
- **Detach** (tab close, network drop): hub closes the data stream only. The **PTY stays alive** on
  the agent. Re-attach repeats §5.3 from step 3 — scrollback replay then live.
- **Close** (explicit): hub sends control `CloseSession`; agent sends `SIGHUP`, reaps the process,
  emits `SessionExited{exitCode}`; hub marks the session `exited`.
- **Lost**: sessions are marked `lost` only when the **session-host**'s `instanceID` changes — i.e. when the session-host process itself dies (crash, OOM, SIGKILL) or the machine reboots and a new host starts with a fresh `instanceID`. A connect restart or `agent update` does **not** mark sessions lost because the session-host — and therefore the `instanceID` — keeps running. The hub's restart detection in `registry.Register` keys entirely on `instanceID` difference (D8).

---

## 6. Wire protocol

**Control stream** — newline-delimited JSON, each line `{"type": "...", ...}`. NDJSON is chosen for
M0–M4 for debuggability; the `codec` package isolates it so msgpack/protobuf can replace it without
touching use cases.

Agent → Hub:
```
Hello         { machineID, name, os, arch, agentVersion, protocolVersion }
Heartbeat     { ts, sessions: [{ id, status, bytesOut }], metrics? }   // metrics: { cpuPercent, memUsedMB, memTotalMB } — added in protocol v4
SessionOpened { sessionID, pid }
SessionExited { sessionID, exitCode }
Error         { sessionID?, code, message }
```
Hub → Agent:
```
OpenSession   { sessionID, cwd, shell, cols, rows, env? }
Resize        { sessionID, cols, rows }
CloseSession  { sessionID }
EnableSnaps   { enabled }                 // M4
```

**Data stream** — first line is `AttachHeader{ sessionID }` (NDJSON); everything after is raw,
unframed PTY bytes in both directions (it is a byte pipe — xterm speaks straight to the shell).

**Agent dial-home credential (M5)** — rides the `Authorization: Bearer` header on the WebSocket
upgrade (works behind any TLS-terminating proxy; no wire-protocol change; protocol stays **2**).
Format: `v1.<machineID>.<unixTs>.<base64url-sig>` where the agent signs the canonical string
`v1:<machineID>:<unixTs>` with its Ed25519 private key. The hub verifies with the stored public
key (±120 s skew). **The hub holds no signing secret** — only public keys — strengthening §10's
"the hub stores no long-lived shell credentials."

**Snapshot stream (M4)** — **NDJSON**, agent → hub. The stream is **agent-opened/hub-accepted**
(the mirror of data streams, which are hub-opened/agent-accepted). Its first line is a
`SnapStreamHeader{type:"SnapStream"}` so the hub can tell it from any other agent-opened stream;
every line after is one `Snapshot`. One snapshot stream per agent connection carries snapshots for
**all** of that agent's sessions (each record is self-identifying). NDJSON was chosen over the
originally-planned length-prefixed framing so the snapshot stream reuses the exact same `codec`
(one (de)serializer to maintain, debuggable on the wire) as the control stream.

Snapshots are **full-color** and **run-length encoded** per row (adjacent cells sharing fg/bg/attrs
collapse to one run), which keeps even colored frames small:
```
SnapStreamHeader { type:"SnapStream" }
Snapshot { sessionID, machineID, cols, rows, cursor:{x,y,visible}, lines:[ {runs:[ {t,f?,b?,a?} ]} ], rev }
```
- `t` = run text (UTF-8); `f`/`b` = fg/bg color; `a` = attribute bitmask (omitted when default/zero).
- **Color encoding** (one int): `0` = terminal default; `1..256` = palette index+1 (0–15 ANSI,
  16–255 xterm-256); `>= 0x1000000` = truecolor, RGB = value & 0xFFFFFF.
- **Attr bits**: bold 1, faint 2, italic 4, underline 8, blink 16, inverse 32, hidden 64, strike 128.
- `rev` increases only when the visible screen changed, so the agent sends a session's snapshot
  only on actual change (plus one initial frame when a viewer connects).

**Snapshot gating** — `EnableSnaps{enabled}` (hub → agent, control stream) turns the snapshot stream
on/off. The agent **always** feeds PTY output into its vt emulator (cheap; keeps the screen current),
but only **sends** snapshots while enabled. The hub enables snapshots when the first `/ws/overview`
viewer connects and disables them when the last leaves (and enables a freshly-(re)connected agent if
viewers are already present), so the stream costs nothing when nobody is watching.

Frame envelopes, type tags, and (de)serialization all live in `internal/transport`, imported by
**both** hub and agent. Protocol DTOs are translated into each side's domain types at the adapter
boundary and never leak into a use case (the agent's vt screen and the hub's overview snapshot are
distinct types from `transport.Snapshot`).

---

## 7. Persistence & the overview pipeline

### 7.1 Scrollback (G2)
Each session owns a **bounded ring buffer** (default 256 KiB, configurable) of recent output, held in the **session-host's RAM**. On attach, the agent writes the buffer to the data stream before going live, so a reconnecting browser repaints history instantly. The buffer is byte-oriented (raw terminal output incl. escape sequences) so replay is faithful; only the cap is enforced, oldest bytes dropped first.

Scrollback survives a `connect` restart automatically — the session-host's ring buffer keeps filling while connect is absent (the `readPump` writes to it continuously, independent of any attached client). Scrollback is **not** persisted to disk; a machine reboot or session-host death empties it — by design.

### 7.2 Screen state & snapshots (G4, M4)
Rendering N full live terminals at once would overwhelm the browser, so the overview does **not**
attach N data streams. Instead:

1. The agent feeds each session's output through an **ANSI/vt emulator** (`adapter/secondary/vt`)
   that maintains the **current visible screen** (a grid of full-color cells + cursor). The emulator
   is an **in-repo, pure-Go** implementation (a Williams ANSI state machine + ECMA-48/VT100
   semantics) — deliberately *not* a third-party dependency, to keep `CGO_ENABLED=0` static builds
   and a self-contained module; it sits behind the `Screen` port so it is swappable. Feeding is
   **always-on** (cheap, and gives an instantly-correct screen when a viewer connects).
2. On a throttled tick (~4 fps, only when the screen's `rev` changed since last sent), the agent
   serializes the grid into a **run-length-encoded, full-color** `Snapshot` (§6) and — **only while
   `EnableSnaps` is on** — sends it on the snapshot stream. This split (always parse, send only when
   watched) is what keeps bandwidth at zero when nobody has the overview open.
3. The hub's **overview** use case caches the latest snapshot per session and fans snapshots out to
   any browser subscribed to `/ws/overview`; a newly-subscribed browser is first replayed the cache
   so every tile populates immediately. Subscriber count drives `EnableSnaps` on the agents.
4. Each browser **tile** renders a snapshot cheaply as **styled HTML** (rows of colored `<span>`
   runs — not an xterm instance per tile, which would not scale to a full grid).
5. **Click-to-dive**: clicking a tile switches to the workspace view and loads that session into the
   focused pane — a normal data-stream attach (§5.3), full fidelity, input enabled.

Bandwidth stays bounded and roughly constant regardless of how busy the shells are, because
snapshots are screen-sized, RLE-compressed, rate-capped, and change-gated — not a copy of the
output stream — and are only produced while someone is watching.

---

## 8. Data model

SQLite on the hub. **Metadata and history only** — live PTY state (scrollback, screen, PTYs) lives in the **session-host process RAM** and is never persisted to disk. The `instanceID` advertised in `Hello` is generated once by the session-host at startup and is stable for its lifetime; connect sources it via the local handshake. Timestamps are unix seconds.

```sql
-- one row per enrolled agent
CREATE TABLE machines (
    id            TEXT PRIMARY KEY,        -- ULID, assigned at enrollment
    name          TEXT NOT NULL,
    os            TEXT NOT NULL,
    arch          TEXT,
    agent_version TEXT,
    enrolled_at   INTEGER NOT NULL,
    last_seen_at  INTEGER,                 -- bumped on heartbeat
    revoked_at    INTEGER                  -- soft revoke (M5)
);

-- long-lived agent credential (M5): stores the agent's Ed25519 public key
CREATE TABLE machine_credentials (
    machine_id TEXT PRIMARY KEY REFERENCES machines(id),
    public_key BLOB NOT NULL,             -- Ed25519 public key (raw bytes)
    created_at INTEGER NOT NULL
);

-- single-use enrollment tokens (M5, migration 0003): hub stores only the SHA-256
CREATE TABLE enroll_tokens (
    id         TEXT PRIMARY KEY,          -- ULID
    token_hash TEXT NOT NULL UNIQUE,      -- SHA-256 of the one-time token
    expires_at INTEGER NOT NULL,
    used_at    INTEGER                    -- set when spent; NULL = still valid
);

-- logical grouping of sessions, bound to a machine + working dir
CREATE TABLE projects (
    id         TEXT PRIMARY KEY,           -- ULID
    machine_id TEXT NOT NULL REFERENCES machines(id),
    name       TEXT NOT NULL,
    path       TEXT NOT NULL,              -- working dir on the machine
    color      TEXT,                       -- UI hint
    created_at INTEGER NOT NULL,
    UNIQUE (machine_id, path)
);

-- terminal session metadata (live I/O is NOT here)
CREATE TABLE sessions (
    id             TEXT PRIMARY KEY,       -- ULID; also the wire id
    project_id     TEXT REFERENCES projects(id),
    machine_id     TEXT NOT NULL REFERENCES machines(id),
    title          TEXT,
    shell          TEXT,
    status         TEXT NOT NULL,          -- running | exited | lost
    activity       TEXT,                   -- reserved for M7: active | idle | awaiting-input
    exit_code      INTEGER,
    created_at     INTEGER NOT NULL,
    last_active_at INTEGER
);
CREATE INDEX idx_sessions_machine ON sessions(machine_id);
CREATE INDEX idx_sessions_project ON sessions(project_id);

-- security-relevant actions (M5)
CREATE TABLE audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    ts         INTEGER NOT NULL,
    actor      TEXT NOT NULL,              -- operator id | "system"
    action     TEXT NOT NULL,              -- login | enroll | attach | open | close | revoke
    machine_id TEXT,
    session_id TEXT,
    detail     TEXT                        -- JSON
);
CREATE INDEX idx_audit_ts ON audit_log(ts);

-- operator auth (M5): passkey + TOTP + recovery
CREATE TABLE operator_credentials (
    id           TEXT PRIMARY KEY,
    kind         TEXT NOT NULL,            -- webauthn | totp | recovery
    data         BLOB NOT NULL,
    created_at   INTEGER NOT NULL,
    last_used_at INTEGER                   -- TOTP: last matched step (prevents replay)
);

-- server-side operator sessions (M5, migration 0004)
CREATE TABLE operator_sessions (
    id         TEXT PRIMARY KEY,           -- opaque random cookie value
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
```

Migrations are embedded SQL (`//go:embed`) applied at hub startup.
`machines.revoked_at` (designed in from M0) is now written by `hub revoke <machineID>` (M5);
`Authenticate` rejects any machine with a non-NULL `revoked_at`.

---

## 9. HTTP / WebSocket API surface

Browser-facing (hub). All REST is JSON; all auth via session cookie (M5).

**REST**
```
GET    /api/machines                     list machines + online status
GET    /api/machines/{id}/sessions       sessions on a machine
GET    /api/projects                     list projects
POST   /api/projects                     create a project {machineID, name, path, color?}
DELETE /api/projects/{id}                delete a project (409 if it still owns sessions)
GET    /api/sessions                     all sessions (overview index)
POST   /api/sessions                     open a session {machineID, projectID?, cwd, shell, cols, rows, title?}
PATCH  /api/sessions/{id}                 rename a session {title}   (metadata only; no wire-protocol change)
DELETE /api/sessions/{id}                close a session
POST   /api/auth/webauthn/begin|finish   passkey login           (M5)
POST   /api/auth/totp                    TOTP fallback           (M5)
```

**WebSocket**
```
GET /ws/term?session={id|new}            browser terminal attach (binary, bidirectional)
GET /ws/overview                         snapshot feed for the grid (server push)   (M4)
GET /ws/agent                            agent dial-home endpoint (credential auth; NOT browser-facing)
```

**Static**
```
GET /  and /assets/*                     the React app, embedded in the hub binary via go:embed
```

DTOs live in `adapter/primary/httpapi`; domain errors map to status codes in one place
(`httpapi/errors.go`).

---

## 10. Security model

The hub is a **remote-code-execution gateway to every machine**. If it is compromised, the attacker
owns the fleet. The model is built around that fact.

**Trust boundaries**
- Browser ↔ hub: untrusted client → authenticated operator session.
- Hub ↔ agent: mutually authenticated; the agent trusts only its enrolled hub, the hub trusts only
  enrolled agents.
- Agent ↔ shell: full local privilege of the agent's user (by design — it *is* your shell).

**Controls** *(items marked **[M5]** are implemented)*
- **TLS end to end** **[M5]** — hub serves HTTPS directly when `tls.cert`/`tls.key` are set
  (`Server.StartTLS`); otherwise plain HTTP behind a TLS-terminating **Caddy** in production
  (`deploy/caddy/Caddyfile` + `deploy/compose.yaml`). Agents verify the hub cert via `hub_ca`
  PEM (defaults to system roots).
- **Operator auth** **[M5]** — **TOTP** (`pquerna/otp`) primary with **recovery codes**
  (single-use, SHA-256-hashed in `operator_credentials`); **WebAuthn passkeys** (`go-webauthn`)
  additive (registration requires an existing session). Bootstrap: `hub operator add` prints the
  `otpauth://` URI + one-time recovery codes. Server-side sessions (`operator_sessions` table,
  migration 0004) with an opaque random cookie `constellate_session` (HttpOnly, SameSite=Lax,
  Secure derived from `https` `public_url`, 24 h). Every terminal attach (`/ws/term`,
  `/ws/overview`) re-checks the session via the auth middleware. **Brute-force hardening:**
  per-IP (~5/min) + global (~15/min) in-memory rate limiting with HTTP 429 + `Retry-After` on
  TOTP/recovery endpoints; TOTP single-use (matched 30 s step recorded in `last_used_at`;
  same-or-earlier step replay rejected).
- **Agent enrollment** **[M5]** — one-time token → **Ed25519 keypair** per machine; hub stores
  only the public key; credential is an **agent-signed bearer assertion** (§6). The hub holds no
  signing secret. Revocation is soft: `revoked_at` set on the machine row; `Authenticate` rejects
  revoked machines. `constellate-agent reset` removes local id/cred.
- **Auth middleware** **[M5]** — gates all `/api/*` and `/ws/*` routes behind a valid session
  cookie; explicit allowlist: `POST /api/enroll`, `POST /api/auth/{totp,recovery,logout}`,
  `GET /api/auth/status`, `POST /api/auth/webauthn/login/{begin,finish}`. A nil auth service
  disables gating (test-only affordance; `cmd/hub` always wires a real one).
- **Audit log** **[M5]** — login, enroll, attach, open/close, revoke wired via `AuditSink`
  consumer port in the `attach`, `sessions`, `enroll`, and `auth` use cases; stored in
  `audit_log` (sqlite + memory stores).
- **Least exposure** — only the hub is public; agents open **no** inbound ports.
- **Dev token removed** **[M5]** — `CONSTELLATE_DEV_TOKEN` / `dev_token` config removed from hub,
  agent, `cmd/*`, and all compose/e2e files. Tests authenticate via real enrollment (in-proc/
  integration: mint + enroll a keypair, dial with a signed token).

M0–M4 ran on localhost or a private network only. **M5 is complete** — the hub can now safely face
the public internet.

---

## 11. Hexagonal design & layering

Two bounded contexts in one module — `internal/hub` and `internal/agent` — each its own hexagon.
They share only `internal/transport` (the wire protocol) and `internal/platform` (logging, ids,
config). Neither context imports the other.

**Rules (enforced by review + lint):**
- **Domain** (`*/domain/*`) is pure: stdlib only, business types with behavior and unexported
  fields, no infra imports. Domain tests run in milliseconds.
- **App** (`*/app/<usecase>`) holds one `UseCase` type per package — *glue, not logic*. Each package
  declares the **SPI interfaces it needs** in its own `ports.go` (consumer-side; no central
  `port/` package).
- **Ports are shaped by the core's needs**, not a vendor API. `MachineStore.ByID(ctx, id)` — never
  `Query(ctx, sql, args...)`.
- **Adapters** split into `primary/` (driving — http, browser WS, agent endpoint) and `secondary/`
  (driven — sqlite, agentlink, pty, vt). Secondary adapters wrap infra errors into domain errors
  (`sql.ErrNoRows → machine.ErrNotFound`). DTOs stay in the adapter and never reach a use case.
- **`cmd/*/main.go` is the composition root** — plain constructors wire concrete adapters into use
  cases; the compiler verifies the graph.

The bidirectional agent link is the one subtlety: from the **hub's** view the same physical
connection is *both* a primary adapter (`wsagent` — agents push `Hello`/`Heartbeat`/output) and a
secondary adapter (`agentlink` — the hub calls `AgentGateway.OpenSession/Resize/Close`). They share
one connection registry but sit on opposite sides of the hexagon, which is correct: inbound events
drive use cases; use cases drive outbound commands.

---

## 12. Target folder tree & CLI commands

### Folder tree
`[Mx]` marks the milestone that introduces a node; unmarked nodes exist from **M0**.

```
Constellate/
├── DESIGN.md                       # ← this document (canonical architecture)
├── README.md                       # quickstart + run instructions
├── LICENSE
├── Makefile                        # build/run/test/lint targets (hub, agent, web)
├── go.mod                          # module github.com/rizquuula/Constellate · go 1.25
├── go.sum
├── .gitignore
├── .golangci.yml                   # linter config
├── configs/                        # hub.example.yaml · agent.example.yaml (copy + edit)
│
├── cmd/                            # entrypoints = composition roots (wiring only, no logic)
│   ├── hub/
│   │   ├── main.go                 # load config → wire internal/hub adapters → start servers
│   │   └── VERSION                 # hub's semver, stamped into the binary at build
│   └── agent/
│       ├── main.go                 # subcommand dispatch: connect · session-host · enroll · install · update …
│       │                           #   cmdConnect: auto-spawns host if needed, dials hostclient, runs hubclient
│       │                           #   cmdSessionHost: generates instanceID, owns Manager+PTYs, runs localhost server
│       ├── spawn_linux.go          # [D8] spawnHostIfNeeded: setsid-spawns session-host when UDS is absent
│       └── VERSION                 # agent's semver, stamped into the binary at build
│
├── internal/
│   │
│   ├── hub/                        # ───── HUB bounded context (control plane) ─────
│   │   ├── domain/                 # pure business; stdlib only
│   │   │   ├── machine/            # machine.go · status.go · errors.go · *_test.go
│   │   │   ├── project/            # project.go · *_test.go
│   │   │   ├── session/            # session.go (metadata + running/exited/lost state) · *_test.go
│   │   │   └── audit/              # event.go (audit value types)
│   │   ├── app/                    # use cases — one package each; ports.go = SPI it needs
│   │   │   ├── registry/           # register/heartbeat/list/mark-offline   (usecase.go·ports.go·_test)
│   │   │   ├── attach/             # broker browser ↔ agent session         (ports: AgentGateway, SessionStore, AuditSink)
│   │   │   ├── sessions/           # create/list/close session metadata
│   │   │   ├── overview/           # [M4] fan snapshots out to viewers
│   │   │   ├── enroll/             # [M5] enrollment tokens + credentials
│   │   │   └── auth/               # [M5] operator login + sessions
│   │   └── adapter/
│   │       ├── primary/            # driving — things that call the hub
│   │       │   ├── httpapi/        # server·router·machines·sessions·projects·dto·errors·middleware (auth.go [M5])
│   │       │   ├── wsbrowser/      # terminal.go (/ws/term) · overview.go (/ws/overview [M4])
│   │       │   └── wsagent/        # endpoint.go (/ws/agent, yamux server) · inbound.go (frames→use cases)
│   │       └── secondary/          # driven — things the hub calls
│   │           ├── agentlink/      # registry.go (machineID→conn) · gateway.go (AgentGateway impl)
│   │           ├── sqlite/         # db.go · *_store.go · migrations/ (0001_init.sql · embed.go)
│   │           ├── memory/         # in-memory stores for tests / M0
│   │           └── webauthn/       # [M5] passkey + TOTP credential store
│   │
│   ├── agent/                      # ───── AGENT bounded context (per machine) ─────
│   │   ├── domain/
│   │   │   └── terminal/           # session.go · scrollback.go (ring buffer) · screen.go [M4] · *_test.go
│   │   ├── app/
│   │   │   ├── session/            # open/attach/detach/resize/close PTYs  (ports: PTYFactory, Clock)
│   │   │   └── snapshot/           # [M4] produce throttled screen snapshots
│   │   └── adapter/
│   │       ├── primary/
│   │       │   ├── hubclient/      # client.go (dial-home, reconnect/backoff, yamux client)
│   │       │   │                   #   control.go (hub frames→use cases) · streams.go (data streams↔PTYs)
│   │       │   └── localhost/      # [D8] server.go — session-host UDS server (host-side primary adapter);
│   │       │                       #   accepts one connect at a time (single-client lease), runs
│   │       │                       #   transport.Server over UDS, dispatches local-control frames into
│   │       │                       #   session.Manager, verifies peer UID via SO_PEERCRED (Linux),
│   │       │                       #   socket is 0600 under a 0700 runtime dir
│   │       └── secondary/
│   │           ├── pty/            # pty.go · factory.go (creack/pty wrapper)
│   │           ├── vt/             # [M4] parser.go · screen.go (ANSI→grid)
│   │           └── hostclient/     # [D8] client.go — connect-side UDS client; dials session-host,
│   │                               #   performs HostHello/HostInfo handshake, sources instanceID,
│   │                               #   implements hubclient.SessionManager by relaying over UDS
│   │
│   ├── transport/                  # ───── SHARED wire protocol (hub + agent import) ─────
│   │   ├── frame.go                # control-frame envelope + type tags
│   │   ├── messages.go             # Hello·Heartbeat·OpenSession·Resize·Close·Snapshot…
│   │   ├── local.go                # [D8] local-protocol types: HostHello·HostInfo·ListSessions·LocalStat
│   │   ├── codec.go                # NDJSON now; swappable for msgpack/proto
│   │   ├── attach.go               # data-stream AttachHeader
│   │   ├── protocol.go             # ProtocolVersion (hub wire) + LocalProtocolVersion (UDS local)
│   │   └── mux.go                  # yamux server/client over a net.Conn
│   │
│   └── platform/                   # ───── SHARED cross-cutting infra ─────
│       ├── log/log.go              # slog setup (level/format from env)
│       ├── id/id.go                # ULID generation
│       ├── config/                 # hub.go · agent.go (typed YAML loaders)
│       └── version/version.go      # per-binary version + commit + proto (via -ldflags)
│
├── web/                            # ───── React + xterm.js (Vite + TS) ─────
│   ├── index.html · package.json · tsconfig.json · vite.config.ts · tailwind.config.ts · .eslintrc.cjs
│   ├── public/favicon.svg
│   └── src/
│       ├── main.tsx · App.tsx
│       ├── api/                    # rest.ts (typed client) · ws.ts (terminal + overview)
│       ├── features/
│       │   ├── overview/           # [M4] OverviewGrid.tsx · SessionTile.tsx · useSnapshots.ts
│       │   ├── terminal/           # TerminalView.tsx (xterm) · useTerminal.ts
│       │   ├── sidebar/            # MachineList.tsx · ProjectTree.tsx
│       │   └── dashboard/          # [M6] Dashboard.tsx
│       ├── store/index.ts          # zustand client state
│       ├── types/api.ts            # mirrors hub DTOs
│       └── styles/globals.css
│
├── deploy/                         # production deployment
│   ├── hub.Dockerfile              # multi-stage → distroless (RELEASE image; reused by tests)
│   ├── compose.yaml                # prod hub: container + Caddy (TLS)
│   ├── agent-supervisor-entrypoint.sh  # [D8] Docker supervisor: runs connect in a loop (PID 1 = shell,
│   │                               #   not connect) so killing connect leaves the setsid-spawned
│   │                               #   session-host alive — used by the connect-restart test
│   ├── systemd/
│   │   ├── constellate-agent.service          # connect relay unit (Restart=always, Requires= host unit)
│   │   └── constellate-session-host.service   # [D8] durable host unit (Restart=on-failure)
│   └── caddy/Caddyfile             # TLS reverse proxy in front of the hub
│
├── scripts/                        # dev-hub.sh · dev-agent.sh · gen-dev-certs.sh
│
├── docs/                           # deep-dives (DESIGN.md stays canonical)
│   ├── protocol.md · security.md   # wire-protocol spec · threat model
│   ├── usage.agent.md              # per-machine setup guide (two-process model, systemd units, update story)
│   └── design/                     # implementation plans (session-survival-plan.md)
│
├── .github/workflows/              # ci.yaml · e2e.yaml · release.yaml
│
└── test/
    ├── integration/                # in-process E2E (no real network)
    │   └── topology_test.go
    ├── e2e/                        # single-machine: real localhost processes + Playwright
    │   ├── e2e_test.go
    │   └── browser/                # Playwright specs
    └── docker/                     # multi-"machine": containers on a Docker network
        ├── compose.test.yaml               # 1 hub + 2–3 agents + test-runner
        ├── compose.connect-restart.yaml    # [D8] connect-restart survival scenario
        ├── agent.test.Dockerfile           # agent binary + a real shell (alpine)
        ├── agent.supervisor.Dockerfile     # [D8] supervisor-mode agent image (PID 1 = shell)
        ├── run.sh                          # main docker E2E (enrollment, dial-home, restart-loses-sessions)
        └── run_connect_restart.sh          # [D8] kills only connect process, asserts sessions stay running
```

**What M0 actually creates** (a small subset): `go.mod`, `Makefile`, `cmd/{hub,agent}/main.go`,
`internal/hub/{domain/machine, app/registry, adapter/primary/{httpapi,wsagent},
adapter/secondary/{agentlink,sqlite,memory}}`, `internal/agent/adapter/primary/hubclient`,
`internal/transport`, `internal/platform/*`, plus an embedded status page. No `vt`, `pty`, `web/`,
`webauthn`, `overview`, or auth yet — those arrive at the milestones tagged above.

---

### Binaries & CLI commands

Two binaries; both take subcommands. The agent defaults to `connect`, the hub to `serve`.
`[M]` = the milestone the command lands in. Global flags on every subcommand: `--log-level`,
`--config <file>`; `--version` short-circuits to the version string.

**`constellate-agent`** — built from `cmd/agent`, deployed on every machine:

| Command | M | Description |
|---------|---|-------------|
| `agent connect` | M0 | Volatile relay: auto-spawns the session-host if not running, sources instanceID, dials home and relays hub commands. **Default.** |
| `agent session-host` | D8 | Durable host: generates instanceID, owns PTYs + scrollback + vt + snapshots, listens on UDS. Not normally run directly — started by `connect` (auto-spawn or systemd). |
| `agent status` | M0 | Print local state: enrolled?, machine id, hub URL. |
| `agent version` | M0 | Print version + git commit + wire protocol version. |
| `agent enroll --hub <url> --token <tok>` | M5 | One-time enrollment: register, store machine id + long-lived credential, then exit. |
| `agent reset` | M5 | Remove the local id/credential — de-enrolls this machine. |
| `agent install` | M5 | Install + start both systemd units (`constellate-session-host.service` and `constellate-agent.service`) with correct ordering. |
| `agent update` | M5 | Download + verify latest release, replace binary, restart connect unit (session-host unit keeps running — sessions survive). |

**`constellate-hub`** — built from `cmd/hub`, runs on the VPS:

| Command | M | Description |
|---------|---|-------------|
| `hub serve` | M0 | Start the HTTP/WS servers (auto-runs migrations). **Default.** |
| `hub migrate` | M0 | Apply DB migrations and exit (idempotent). |
| `hub version` | M0 | Print version + git commit. |
| `hub enroll-token [--ttl 15m]` | M5 | Mint a one-time agent enrollment token and print it. |
| `hub machines` | M5 | List enrolled machines with online status + last-seen. |
| `hub revoke <machineID>` | M5 | Revoke a machine's credential (it can no longer dial home). |
| `hub operator add` | M5 | First-run setup: register the operator passkey / TOTP. |

CLI plumbing stays light — stdlib `flag` with a small subcommand dispatcher (Cobra only if the
command set grows). Long-running modes (`connect`, `serve`) trap `SIGINT`/`SIGTERM` for graceful
shutdown.

## 13. Technology stack

**Go 1.25+** (`go.mod` → `go 1.25`, with a `toolchain` directive). Hub and agent share the module.

| Concern | Choice | Notes |
|---------|--------|-------|
| WebSocket | `github.com/coder/websocket` | context-aware; `NetConn()` adapts a socket to `net.Conn` for yamux |
| Multiplexing | `github.com/hashicorp/yamux` | many streams over one conn, built-in keepalive/backpressure |
| PTY | `github.com/creack/pty` | spawn/resize/reap PTYs |
| SQLite | `modernc.org/sqlite` | **pure Go**, no cgo → static binaries |
| Migrations | embedded SQL + tiny migrator (or `pressly/goose`) | `//go:embed` |
| IDs | `github.com/oklog/ulid/v2` | time-sortable session/machine ids |
| Config | `gopkg.in/yaml.v3` | typed YAML config files via `--config` |
| Logging | stdlib `log/slog` | structured, leveled |
| WebAuthn | `github.com/go-webauthn/webauthn` | passkeys (implemented M5; pure Go, CGO_ENABLED=0 verified) |
| TOTP + recovery | `github.com/pquerna/otp` | TOTP + single-use recovery codes (implemented M5) |
| Agent credential | stdlib `crypto/ed25519` | Ed25519 keypair; hub stores only the public key |
| Lint | `golangci-lint` | `go vet` + staticcheck + more |

> **VT/ANSI emulator (M4)** is **in-repo**, not a dependency: a pure-Go terminal emulator under
> `internal/agent/adapter/secondary/vt` (Williams parser + ECMA-48/VT100 semantics) producing a
> full-color cell grid. A library (`vito/midterm`, `danielgatis/go-headless-term`, `charmbracelet/x/vt`)
> was evaluated; an in-repo implementation was chosen to keep the module self-contained with zero new
> deps and full control, kept swappable behind the `Screen` port.

**Frontend:** Vite + React + TypeScript · `@xterm/xterm` + `@xterm/addon-fit` +
`@xterm/addon-webgl` · `zustand` (client state) + TanStack Query (server state) · React Router ·
Tailwind CSS. Built to `web/dist` and embedded into the hub binary via `go:embed`.

**Build & test infra:** Docker + Compose (hub release image + the Dockerized topology tests) ·
GitHub Actions (CI + release) · Playwright (browser E2E) · `gcr.io/distroless/static` base for the
hub image.

---

### Versioning & compatibility

Two axes, deliberately separate:

- **Per-binary release version.** `cmd/hub/VERSION` and `cmd/agent/VERSION` each hold a semver,
  bumped independently. The Makefile reads the relevant file and stamps that binary via
  `-ldflags "-X …/platform/version.Version=…"` together with the git short-commit and build time;
  `hub version` / `agent version` print their own. Release tags are component-scoped —
  `hub/vX.Y.Z`, `agent/vX.Y.Z`.
- **Protocol version — the real compatibility gate.** `transport.ProtocolVersion` (an int, bumped
  on a wire-protocol change — breaking *or* a new capability worth advertising) is sent in `Hello`.
  The hub enforces a minimum supported protocol plus a **compat window**, accepting or rejecting with
  a clear message; older peers in the window keep working. This is *why* independent binary versions
  are safe: interop depends only on the negotiated protocol, never on the release labels — so hub
  `1.4.0` and agent `0.9.2` talk fine as long as their protocols are in range. M4 bumped the protocol
  to **2** (adds `EnableSnaps` + the snapshot stream) with the window held open at **[1, 2]**: the
  additions are backward compatible, so a v1 and a v2 peer still interoperate — just without the
  overview feed. Protocol is now **4** (window **[1, 4]**): v4 adds `Heartbeat.metrics` (host
  CPU/RAM via gopsutil); host metrics live in-memory on the live `Conn` and are absent when the
  agent is offline. All additions are additive; older peers ignore unknown fields.

Both `version` commands print all three for skew debugging, e.g.
`constellate-agent 0.9.2 (commit a1b2c3d, proto 4)`.

## 14. Configuration

Configuration is **YAML**. Each binary loads one file via `--config <path>` (defaults: `./hub.yaml`
then `/etc/constellate/hub.yaml` for the hub; `~/.constellate/agent.yaml` for the agent). Sample
files live in `configs/`. Individual secrets may still be overridden by `CONSTELLATE_*` env vars for
containerized deploys.

**Hub** — `hub.yaml`:
```yaml
addr: "127.0.0.1:8080"                          # listen address
public_url: "https://constellate.example.com"   # external URL (cookies, WebAuthn RP)
db_path: "./constellate.db"
enroll_token_ttl: "15m"                         # one-time agent enrollment token lifetime
tls:                                            # optional direct TLS (else terminate at Caddy)
  cert: ""                                      # PEM cert path; leave empty to run behind proxy
  key:  ""
# webauthn:                                     # derived from public_url by default
#   rp_id: "example.com"
#   origins:
#     - "https://example.com"
log:
  level: "info"                                 # debug | info | warn | error
  format: "text"                                # text | json
```

**Agent** — `agent.yaml`:
```yaml
hub_url: "ws://127.0.0.1:8080/ws/agent"
name: ""                                        # default: hostname
id_file: "~/.constellate/agent-id"
# cred_file: "~/.constellate/cred"             # enrolled Ed25519 credential (default shown)
# hub_ca: ""                                   # PEM CA/cert to verify hub; empty = system roots
default_shell: "/bin/bash"
scrollback_bytes: 262144                        # ring buffer cap per session
# runtime_dir: ""                              # dir for session-host UDS (host.sock); dir mode 0700,
                                               # socket mode 0600; default: $XDG_RUNTIME_DIR/constellate
                                               # if set, else ~/.constellate/run
                                               # env override: CONSTELLATE_RUNTIME_DIR
log:
  level: "info"                                 # debug | info | warn | error
  format: "text"                                # text | json
```

---

## 15. Observability

- **Structured logging** (`slog`) on both binaries; one log line per lifecycle transition
  (dial/online/retry, open/attach/detach/close) with `machineID`/`sessionID` fields. No secrets,
  no terminal contents at info level.
- **Correlation**: a `sessionID` threads from the browser WS through the hub to the agent PTY,
  so a single session is greppable end to end.
- **Hub metrics** (later, optional): connected agents, live sessions, attach latency, snapshot
  fps/bandwidth — exposable on a private `/metrics`.

---

## 16. Testing & environments

Stability *is* the product — a terminal you can't trust is worse than none. Tests are designed in
from M0, run on every change, and scale from microsecond unit tests up to a Dockerized replica of
the real multi-machine topology. Three things must always hold: the domain is proven in isolation,
the full vertical works on one machine, and dial-home works across real network/process boundaries.

### Test pyramid

| Tier | Scope | Where | Runs |
|------|-------|-------|------|
| **Unit** | domain entities; app use-cases with hand-written fakes | `internal/**/*_test.go` | every save / push |
| **Integration** | secondary adapters vs the real thing — SQLite file, real PTY, WS loopback | `internal/**/adapter/**/*_test.go` | every push |
| **In-process E2E** | hub+agent wired in one process, no real network; a harness stands in for the browser | `test/integration/` | every push |
| **Single-machine E2E** | hub + agent(s) as real OS processes on localhost; real WS + PTYs; Playwright drives a real browser | `test/e2e/` | pre-merge |
| **Dockerized topology E2E** | hub container + N agent containers on a Docker net — each agent a "separate machine" | `test/docker/` | pre-merge + nightly |

Core principles (from the hexagon): **no mocks in the domain**; **hand-written in-memory fakes** for
use-cases (the `secondary/memory` stores double as fakes); **adapters tested against the real thing**
(a SQL adapter tested against a mocked `*sql.DB` has tested nothing). Mock only ports we own — never
`*sql.DB` or sockets.

### Single-machine E2E — the dev-loop E2E
`test/e2e/` boots the hub and one or more agents as real processes on `127.0.0.1`, lets the agents
dial home over real loopback WebSockets, then drives the system two ways: a Go WS/HTTP client for
fast assertions and **Playwright** against a real browser for the UI (open a shell, type, read,
resize, reconnect, view the overview, click a tile). Proves real process + socket + PTY behavior
without Docker overhead — fast enough for the tight loop.

### Dockerized topology E2E — the "separate machines" E2E
`test/docker/compose.test.yaml` brings up the **hub** (release image), **2–3 agent** containers —
each a small image with a real shell, standing in for a separate machine with its own hostname/id —
and a **test-runner** (Playwright + Go client). Agents sit on an **internal Docker network that can
reach only the hub, not each other**, mimicking machines behind NAT and proving the dial-home model
(no inbound ports; the hub never connects in). Scenarios assert: enrollment/dial-home, a live shell
through the container boundary, scrollback replay on re-attach, the overview across all "machines",
and **kill an agent container → its sessions go `lost` → restart → it reconnects** (validating D8:
when the session-host dies, sessions are correctly marked lost). A separate scenario (`run_connect_restart.sh`,
`compose.connect-restart.yaml`) uses a supervisor-mode container to **kill only the connect process** while
the session-host survives: asserts the hub never marks sessions `lost` and the instanceID is unchanged
after reconnect — the end-to-end proof of D8. This tier is the executable form of the M0 acceptance
check and grows with every milestone.

### CI — GitHub Actions
- `ci.yaml` — lint + unit + integration + in-process E2E on every push/PR (fast gate).
- `e2e.yaml` — single-machine + Dockerized topology E2E on PRs and nightly (heavier).
- `release.yaml` — on a `hub/vX.Y.Z` or `agent/vX.Y.Z` tag, build and publish the artifacts in §17.

---

## 17. Build, release & deployment

Two binaries, two shapes — because they live in different places.

### Hub — released as a Docker image
Multi-stage build: compile the static Go hub with the embedded React app, then copy into a minimal
runtime (`gcr.io/distroless/static`, or `scratch`) for a tiny, dependency-free image. Published to
GHCR as `ghcr.io/rizquuula/constellate-hub:vX.Y.Z` (+ `:latest`), tag-driven from `cmd/hub/VERSION`.
Runs on the VPS via `deploy/compose.yaml` (hub container + Caddy for TLS). `deploy/hub.Dockerfile`
is the single source for both the release image and the Dockerized tests.

### Agent — released as a static binary (containerized only for tests)
The agent spawns shells on the **host** it manages, so in production it runs as a host process,
**not** a container — a containerized agent would only reach the container's own shell. It ships as
a static, cross-compiled binary per OS/arch, attached to an `agent/vX.Y.Z` GitHub Release, installed
on each machine via two systemd units — `constellate-session-host.service` (durable host, `Restart=on-failure`)
and `constellate-agent.service` (volatile connect relay, `Restart=always`, `Requires=` the host unit) —
written by `agent install` (D8). The agent **is** containerized for the Dockerized topology tests — a
test fixture, never a release artifact.

### Make targets
`make test` (unit + integration + in-proc) · `make test-e2e` (single-machine) · `make test-docker`
(topology) · `make lint` · `make build` (both binaries, version-stamped) · `make image-hub` ·
`make release` (cross-compile agent binaries + push the hub image).

---

## 18. Milestone roadmap

Each milestone is one tracked task, lands in its own commit, and is "done" only when its
acceptance check passes.

### M0 — Scaffold + prove dial-home topology
- Monorepo + hexagonal skeleton; `go.mod` (go 1.25); Makefile.
- Hub: serves an embedded status page + `GET /api/machines`; `/ws/agent` accepts agents over yamux;
  agentlink registry; SQLite `machines` table wired.
- Agent: dials home, `Hello` + `Heartbeat` on the control stream, reconnect w/ backoff.
- **Test harness established here:** unit + the in-process E2E harness, *and* `test/docker` (hub +
  2 agent containers on a Docker network) — so dial-home is proven both in-process and across real
  container boundaries from day one.
- **Done when:** the online → offline → online check passes on localhost **and** via
  `make test-docker`; `go build ./... && go vet ./...` clean.

### M1 — First live terminal
- Agent spawns a PTY per session; data-stream attach; hub relays browser WS ↔ data stream.
- React + xterm.js: pick a machine, open a shell, type and see output.
- **Done when:** a real interactive shell works in the browser, incl. resize.

### M2 — Session persistence
- Scrollback ring buffer; replay-on-attach; PTYs survive detach.
- **Done when:** close the tab mid-`top`, reopen → same session, history intact, still live.

### M3 — Multi-session + projects
- Many sessions per machine; create/rename/close; group by project; persist projects.
- Sessions may be **project-less** (an "ungrouped" bucket per machine) — `projectID` is nullable.
  Rename is metadata-only (`PATCH /api/sessions/{id}`, no agent/PTY disruption). **No project
  delete in M3** (close sessions individually; project deletion was deferred — see *Post-M7:
  project delete* below).
- **Terminal UI is a recursive split-pane workspace**: a binary split tree whose leaves are each a
  live terminal bound to one session, splittable horizontally/vertically, with a focused pane you can
  split or close. This collapses the project axis (sidebar tree) and the session axis (panes) into a
  single browser tab — the core motivation in §1.
- `sessions.activity` remains a DB-only column, unread/unwritten until M7.
- **Done when:** sessions are organized by project across machines, not a flat host list, and several
  live shells are visible at once in split panes.

### M4 — Mission-control overview
- vt parser → screen state; throttled snapshot stream; `/ws/overview` fan-out; tile grid;
  click-to-dive.
- **Done when:** one page shows every live terminal as a live-ish tile; clicking opens the full
  session; bandwidth stays bounded with all machines visible.

### M5 — Auth + audit hardening *(done)*
- **Agent enrollment:** `hub enroll-token` mints a one-time token (SHA-256 stored); `agent enroll`
  generates an Ed25519 keypair, POSTs the public key to `POST /api/enroll`, receives a machineID.
  Dial-home credential is an **agent-signed bearer assertion** (`v1.<machineID>.<ts>.<sig>`) — hub
  holds no signing secret, only public keys. Revocation is soft (`machines.revoked_at`); `hub
  machines` / `hub revoke <machineID>` / `agent reset` wired. **Protocol stays at 2** (credential
  rides the `Authorization` header, not `Hello`).
- **Operator auth:** TOTP (`pquerna/otp`) + single-use recovery codes (SHA-256-hashed) + WebAuthn
  passkeys (`go-webauthn`, registration requires an existing session). Server-side sessions in
  `operator_sessions` (migration 0004); opaque random cookie `constellate_session` (HttpOnly,
  SameSite=Lax, Secure from `https` `public_url`, 24 h). Rate limiting (per-IP + global) + TOTP
  single-use anti-replay. Bootstrap: `hub operator add`.
- **Auth middleware** gates all `/api/*` + `/ws/*` with an explicit allowlist for unauthenticated
  paths; terminal attach re-checks the session on every WS upgrade.
- **Audit log** wired via `AuditSink` port in `attach`, `sessions`, `enroll`, and `auth` use cases.
- **TLS:** hub serves direct HTTPS when `tls.{cert,key}` set; otherwise behind Caddy
  (`deploy/caddy/Caddyfile` + `deploy/compose.yaml`). Agent verifies hub cert via `hub_ca`.
- **Dev token removed** everywhere. Tests authenticate via real enrollment.
- Decisions folded into `DESIGN.md` §5.1/§6/§8/§10/§13/§14.

### M6 — Progress dashboard *(done)*
- Status/liveness rollups across machines and projects.
- **Done when:** the operator can see, at a glance, what's running/idle/needs attention fleet-wide.
- **Shipped:** a server-side aggregation use case (`app/dashboard`) composes the machine/session/
  project/audit read ports + live-agent presence into a single `View` — fleet totals
  (machines online/total, sessions running/exited/lost/total, projects), per-machine and
  per-project rollups (with an "Ungrouped" bucket for project-less sessions), an **attention list**
  (lost sessions; offline machines with still-running sessions), and the **20 most recent audit
  events**. Exposed at session-gated `GET /api/dashboard`; the frontend adds a third
  **Workspace ↔ Overview ↔ Dashboard** view (summary cards, rollup tables, attention banner,
  recent-activity feed) that polls only while active. Per-session *activity* (active/idle/
  awaiting-input) is deferred to M7 — M6 rolls up **status** only.

### M7 — AI-session awareness *(done)*
- Track per-session **activity** (active / idle / awaiting-input) from output heuristics plus an
  opt-in shell hook, surfaced on overview tiles and the dashboard. The `sessions.activity` field is
  designed in from M1, so this is additive — not a rewrite.
- **Done when:** you can see at a glance which Claude Code / agent sessions across the fleet need
  your attention.
- **Shipped:** the agent derives activity per session from (a) **output timing** (output within a
  ~2 s window → *active*), (b) **OSC 133** shell-integration prompt markers parsed by the vt
  emulator (`133;C` running, `133;A/B/D` at-prompt) to distinguish *idle at a prompt* from a
  *running command awaiting input*, and (c) a screen-tail **question heuristic** (bounded to short
  lines to avoid log false-positives). Activity is reported in each `Heartbeat`
  (`SessionStat.activity`) — **protocol bumped to 3**, window held open at **[1, 3]** (additive: a
  v2 peer ignores the field). The hub persists it to `sessions.activity` (best-effort; a missing
  session is tolerated), surfaces it on the session REST DTO, and the dashboard adds
  active/idle/awaiting-input **totals** + an **`awaiting_input` attention** kind. The frontend shows
  an **activity badge** (active/idle/**needs input**, the high-signal state, colorblind-distinct and
  reduced-motion-safe) on sidebar rows, overview tiles, and the dashboard. The **opt-in shell hook**
  (OSC 133 bash/zsh snippets) is documented in [`docs/shell-integration.md`]; without it, activity
  falls back to output-timing + the screen-tail heuristic. Decisions folded into §6/§7.2/§18.

### D8 — Session-host / connect split (2026-06-18, supersedes D6) *(done)*

Sessions survive an agent restart. The single-process agent is split into two roles sharing one static binary:

- **`session-host`** (durable): generates the machine's `instanceID` once at startup; owns all PTYs, scrollback ring buffers, the vt emulator, and the snapshot producer; listens on a Unix domain socket (`$XDG_RUNTIME_DIR/constellate/host.sock`, dir `0700`, socket `0600`). Accepts exactly one connect client at a time (single-client lease) and verifies peer UID via SO_PEERCRED (Linux). Supervised by `constellate-session-host.service` (`Restart=on-failure`).
- **`connect`** (volatile): dials the session-host UDS, performs the `HostHello`/`HostInfo` handshake (local protocol v2), sources the stable `instanceID`, then dials home to the hub and relays all hub commands and events. Restarting connect (by `agent update` or systemd) does not change the `instanceID` — the hub's `registry.Register` sees `restarted=false` and leaves sessions `running`. Supervised by `constellate-agent.service` (`Restart=always`, `Requires=constellate-session-host.service`).
- **Auto-spawn** (non-systemd / dev): if the UDS is absent when connect starts, connect `setsid`-spawns the session-host as a detached process (own process group, stdio to a log file next to the socket) and polls until it answers (up to 10 s). `spawn_linux.go`, Linux-only.
- **Local protocol** (`internal/transport/local.go`): reuses the existing NDJSON codec and yamux mux over the UDS. `LocalProtocolVersion = 2`. Message set: `HostHello` (connect→host), `HostInfo` (host→connect: instanceID + session list), `ListSessions`, `LocalStat` (host→connect: per-session activity for hub Heartbeat). Data streams use the existing `AttachHeader` + raw bytes.
- **Hub-side impact**: zero code change. `registry.Register` already keys on `instanceID` difference for restart detection.
- **Survival scope**: connect restart/update ✓ · network drop/hub restart ✓ · machine reboot ✗ (session-host dies with the OS) · session-host crash ✗ (new host = new instanceID = sessions correctly marked `lost`).
- **Rejected alternatives**: tmux-backed PTYs (extra runtime dep; external process lifecycle outside Go); SCM_RIGHTS fd-passing (Linux-only, fragile, non-portable); keeping D6's in-process model (restart-drops-sessions trade-off, now unacceptable given the update UX).
- **Test**: `test/docker/run_connect_restart.sh` + `compose.connect-restart.yaml` — supervisor-mode container kills only the connect PID; asserts hub never logs "process restart detected, marking running sessions lost" between the two agent-online events.

### Post-M7 — project delete
- Closes the M3 deferral. `DELETE /api/projects/{id}` (session-gated like the rest of `/api/*`)
  removes a project. The use case **refuses with `409` (`projects.ErrHasSessions`)** when the
  project still owns one or more sessions — they are **never orphaned or cascade-deleted**; the
  operator reassigns or closes them first. This is the most conservative of the three options
  (block / reparent-to-ungrouped / cascade) and keeps terminal history safe by default.
- **Ports:** `ProjectStore.Delete(id)` (sqlite + memory) and a new consumer-side `SessionCounter`
  SPI (`CountByProject`) satisfied by the existing session store — the project use case asks "does
  anything still reference me?" without importing the session use case.
- **Frontend:** sidebar project headers gained a trash button with an inline confirm (4 s
  auto-cancel, mirroring `SessionRow`) and a 409-aware error message.
- **Done when:** an empty project can be deleted from the sidebar; a project with live/closed
  sessions is refused with a clear message until its sessions are moved or closed.

---

## 19. Glossary

- **Hub** — the single public Go service brokering everything.
- **Agent** — the per-machine Go binary that owns PTYs and dials home. Runs as two cooperating processes: the durable *session-host* and the volatile *connect relay* (D8).
- **Session-host** — the durable agent process that owns PTYs, scrollback, and the `instanceID`. Survives connect restarts.
- **Connect** — the volatile agent process that dials home to the hub and relays commands to the session-host over the local UDS protocol.
- **Session** — one terminal (one PTY on the agent; metadata row on the hub).
- **Project** — a named grouping of sessions bound to a machine + working dir; uniqueness is
  `(machine_id, path)`. A session may be project-less (ungrouped).
- **Pane** — one leaf of the terminal split tree: a single live session rendered in part of the
  viewport. Panes split horizontally/vertically; the workspace is the tree of panes.
- **Dial-home** — the agent's outbound connection to the hub (no inbound ports on machines).
- **agentlink** — the hub's live `machineID → connection` registry + outbound `AgentGateway`.
- **Snapshot** — a compact, rate-capped copy of a session's visible screen, for the overview.
- **Scrollback** — the bounded ring buffer of recent output, replayed on re-attach.
```
