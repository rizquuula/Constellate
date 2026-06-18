# Session Survival Across Agent Restart — Implementation Plan

> Status: planning only (no code yet). Architecture is **decided**; this document grounds it in
> the real code and gives ordered, file-accurate steps. Reverses locked decision **D6**
> (`DESIGN.md:88`).

## 1. Goal & scope

Make shell sessions (live PTY + scrollback) survive an **agent restart** — specifically a restart
of the volatile, hub-facing process. Today the whole agent is one process: `cmd/agent` generates a
fresh `instanceID` on every `connect` start (`cmd/agent/main.go:199`) and owns the
`session.Manager` + all PTYs (`cmd/agent/main.go:201`). When that process dies, the PTYs die and the
hub marks the machine's sessions `lost` (`internal/hub/adapter/primary/wsagent/inbound.go:74-77` →
`internal/hub/app/sessions/usecase.go:143-144` → `session_store.go:96-106`).

**In scope (survives):**
- The hub-facing relay process (renamed/role-split as **connect**) crashing, being restarted by
  systemd, or being replaced by `agent update`.
- Network drops, hub restarts, browser detach (already survive today via scrollback replay).

**Out of scope (acceptable loss, unchanged):**
- **Machine reboot** — PTY children die with the host; sessions go `lost`. No disk persistence of
  PTY state (scrollback lives in host RAM only).
- The **session-host process itself** dying (crash/OOM/kill) — its PTYs die; new host → new
  `instanceID` → sessions correctly `lost`.

## 2. Current state (file-accurate)

### PTY ownership & the Manager
- `session.Manager` owns every `liveSession{pty, sb (scrollback), screen}`:
  `internal/agent/app/session/manager.go:14-23,30-58`.
- `Open` spawns the PTY via `PTYFactory` and starts `readPump`:
  `manager.go:79-110`; `readPump` continuously drains the PTY into scrollback + vt screen
  regardless of any attach: `manager.go:325-354`.
- `Attach` replays scrollback from `Oldest()` then streams live, copying client input into the PTY:
  `manager.go:116-172`.
- `Resize` / `Close` / `Shutdown`: `manager.go:175-208`.
- Snapshot/activity production reads off the Manager: `RunningScreenRevs`/`RenderScreen`
  (`manager.go:243-269`), `Activities` (`manager.go:280-309`).
- PTY factory adapter: `internal/agent/adapter/secondary/pty/` (`PTYFactory`/`PTY` ports at
  `internal/agent/app/session/ports.go:27-39`).

### Scrollback (backpressure already solved — HARD PART #6)
- `terminal.Scrollback` is a **bounded broadcast byte buffer** with absolute offsets and
  cancelable blocking reads: `internal/agent/domain/terminal/scrollback.go:7-16`. `Write` evicts
  oldest bytes past the cap and broadcasts: `scrollback.go:32-52`. `readPump` writes into it on
  every PTY read with **no dependency on an attached client** (`manager.go:328-334`). **Confirmed:
  the host keeps filling scrollback while connect/hub/browser are all absent** — this is exactly the
  property session-survival needs, and it requires no change.

### instanceID (the identity lever — HARD PART, key change)
- Generated fresh per `connect` invocation: `instanceID := id.New()` at `cmd/agent/main.go:199`,
  passed into `hubclient.Config.InstanceID` (`main.go:208`, field at
  `internal/agent/adapter/primary/hubclient/client.go:66,88,127-128`).
- Sent in `Hello`: `sendHello` → `transport.NewHello(machineID, instanceID, …)`
  (`hubclient/control.go:13-23`; `Hello.InstanceID` field at `internal/transport/messages.go:28`,
  constructor `messages.go:124-135`).

### Hub restart/"lost" logic (must stay nearly untouched)
- `wsagent.handleControl` reads `Hello`, calls `registry.Register`, and if `restarted` marks
  sessions lost: `inbound.go:32-77`.
- `registry.Register` sets `restarted` when a prior record has a **different non-empty
  instanceID**: `internal/hub/app/registry/usecase.go:56-72` (the comparison at line 61).
- `MarkMachineSessionsLost` → `sessions.UseCase.MarkMachineSessionsLost`
  (`usecase.go:141-144`) → `SessionStore.MarkRunningLost` SQL
  (`internal/hub/adapter/secondary/sqlite/session_store.go:96-106`; memory mirror
  `secondary/memory/session_store.go:100-106`).
- Machine domain stores instanceID: `machine.New(id, instanceID, …)`
  (`internal/hub/domain/machine/machine.go:18`) and `Machine.InstanceID()` (`machine.go:62`).

### Hub→agent control + data streams (what the local protocol must mirror)
- Control commands the hub sends: `OpenSession`/`Resize`/`CloseSession`/`EnableSnaps`
  (handled agent-side at `hubclient/control.go:26-103`).
- Hub opens **data** streams (yamux, hub→agent), writes `AttachHeader`, then raw bytes:
  `agentlink/gateway.go:OpenDataStream` (`gateway.go` bottom) +
  `agentlink/registry.go:openDataStream` (`registry.go:98-100`); agent accepts and pipes:
  `hubclient/client.go:430-439` → `hubclient/streams.go:12-26` → `Manager.Attach`.
- Agent→hub control: `Hello`/`Heartbeat`/`SessionOpened`/`SessionExited`/`Error`
  (`hubclient/client.go` heartbeat loop `client.go:381-414`; `control.go`; notifier
  `client.go:165-174`).
- Snapshot stream: agent-opened, NDJSON, one per connection: `client.go:184-243` (producer at
  `internal/agent/app/snapshot/producer.go`).

### Transport reuse points
- yamux wrappers: `internal/transport/mux.go:17-24` (`Server`/`Client` over any `net.Conn`).
- NDJSON codec: `internal/transport/codec.go` (`Encoder`/`Decoder`/`Frame`/`Unmarshal`).
- Attach header: `internal/transport/attach.go`. Message DTOs + constructors:
  `internal/transport/messages.go`. Protocol gate: `internal/transport/protocol.go:16-25`.

### Update / lifecycle today
- `agent update` downloads + runs a verified `update.sh` and restarts the systemd service:
  `cmd/agent/update.go:19-129` (`--no-restart` flag `update.go:24`; restart is delegated to
  `update.sh` via `CONSTELLATE_NO_RESTART`, `update.go:174-176`).
- `agent install` writes a systemd unit whose `ExecStart` is `<bin> connect` and `Restart=always`:
  `cmd/agent/main.go:478-512,514-656`. **There is no `deploy/systemd/` dir today** (DESIGN.md's
  tree at `DESIGN.md:614` lists one, but units are generated programmatically by `install`).
- Agent config has no runtime-dir/socket field: `internal/platform/config/agent.go:11-20`.

## 3. Target architecture

```
                 one machine (host process group A)          process group B (connect, restartable)
   ┌───────────────────────────────────────────────┐     ┌──────────────────────────────────────┐
   │  constellate-session-host  (DURABLE)           │     │  constellate-agent connect (VOLATILE) │
   │   owns:                                         │ UDS │   - dial-home + Ed25519 auth          │
   │   - instanceID (generated at host start)        │◀───▶│   - yamux to hub (unchanged)          │
   │   - session.Manager + PTYs + scrollback rings   │byte │   - gopsutil host metrics             │
   │   - vt emulator, snapshot producer, activity    │relay│   - PURE RELAY: hub streams ⇄ UDS     │
   │   - listens on unix socket (0600, runtime dir)  │     │   - sources instanceID FROM host      │
   └───────────────────────────────────────────────┘     └──────────────────────────────────────┘
       survives connect restart (RAM scrollback)              restarts freely (systemd / update)
```

**Packages that MOVE to the host** (run inside `constellate-session-host`):
- `internal/agent/app/session` (Manager), `internal/agent/app/snapshot` (Producer),
  `internal/agent/adapter/secondary/pty`, `internal/agent/adapter/secondary/vt`,
  `internal/agent/domain/terminal` (already pure).
- instanceID generation (moves from `cmd/agent/main.go:199` to host start).

**Stays in connect:**
- `internal/agent/adapter/primary/hubclient` (yamux-to-hub client; auth, reconnect, backoff),
  `internal/agent/adapter/secondary/sysmetrics` (gopsutil), credential/enroll/install/update CLI.

**New packages (hexagonal layering per CLAUDE.md):**
- `internal/agent/adapter/primary/localhost` — **host-side primary adapter**: accepts the connect
  UDS connection, runs `transport.Server` over it, dispatches local-control frames into the existing
  `session.Manager` (the same dispatch logic as `hubclient/control.go`, reused), and serves
  data/snapshot streams. This is the host's "driving" edge.
- `internal/agent/adapter/secondary/hostclient` — **connect-side secondary adapter**: dials the UDS,
  runs `transport.Client`, exposes the Manager-shaped interface that `hubclient` already consumes
  (`hubclient.SessionManager` at `client.go:25-32` + `SnapshotSink`/`MetricsSampler`), so connect
  relays hub commands to the host and host events back to the hub. This is connect's "driven" edge.
- `cmd/agent` gains a **`session-host`** subcommand (composition root for the host), wired in
  `cmd/agent/main.go` dispatch (`main.go:70-89`).

## 4. Local (connect ⇄ host) protocol

**Transport reuse.** Run the existing `transport.Server`/`transport.Client`
(`internal/transport/mux.go`) over the UDS `net.Conn`, with the existing NDJSON `codec` for control
and the existing `AttachHeader` + raw bytes for data — so `OpenSession` / `AttachHeader+bytes` /
`Resize` / `CloseSession` / `Heartbeat`-stats / `Snapshot` map ~1:1 to the hub protocol. The relay
is **byte-level**, not fd-passing.

**Message set (mostly the existing `transport` DTOs, plus a small local-only addition):**

| Direction | Messages |
|-----------|----------|
| connect → host | `OpenSession`, `Resize`, `CloseSession`, `EnableSnaps`, **`HostHello`** (new, local-only), **`ListSessions`** (new, local-only request) |
| host → connect | `SessionOpened`, `SessionExited`, `Error`, **`HostInfo`** (new: `{instanceID, protocolVersion, sessionList[]}`), `Snapshot` (snapshot stream), per-session activity (folded into a periodic local `Heartbeat`-style stat frame) |
| both, data streams | `AttachHeader{sessionID}` + raw PTY bytes (connect-opened, host-accepted — mirror of hub→agent) |

New local-only types live in a small `internal/transport` addition (e.g. `local.go`) reusing the
same codec: `HostHello{localProtocol}`, `HostInfo{instanceID, localProtocol, sessions[]}`,
`ListSessions{}`. Keeping them in `transport` honors "shared wire protocol" and avoids a second
codec.

**Version negotiation (HARD PART #2 enabler).** Add `LocalProtocolVersion` constant alongside
`ProtocolVersion` in `internal/transport/protocol.go`. Handshake: connect sends `HostHello{localProtocol}`,
host replies `HostInfo{instanceID, localProtocol, sessions}`. Each side computes
`min(localProtocol)` and only uses features in the negotiated window. **An old host serving a new
connect** works because: (a) the message set is NDJSON with `type` tags and `Unmarshal` ignores
unknown fields (`codec.go:77-83`), exactly like the hub protocol's additive evolution
(`protocol.go:1-25`); (b) connect must not *require* any frame the negotiated version lacks. New
connect features that an old host can't serve are gated on the negotiated local version.

## 5. Identity / instanceID change & hub touch points

**The change:** the host generates `instanceID` once at start; connect fetches it via `HostInfo`
during the local handshake and reports it in `Hello`.

- **Move** `id.New()` from `cmd/agent/main.go:199` into the new `session-host` composition root.
- **connect** no longer generates an instanceID; it sources it from `hostclient` (`HostInfo.instanceID`)
  and passes it to `hubclient.Config.InstanceID` (`client.go:66`). If the host restarts, connect
  re-handshakes and picks up the new instanceID, then reconnects to the hub with it.

**Hub-side touch points — these should be NONE (zero code change):**
- `registry.Register` restart detection (`registry/usecase.go:61`) already keys on instanceID
  difference. connect-restart-with-host-alive → same instanceID → `restarted=false` → sessions stay
  `running`. host-death → new instanceID → `restarted=true` → `MarkMachineSessionsLost`. **This is
  exactly the desired semantics with no hub change.**
- `wsagent/inbound.go:62-77`, `sessions/usecase.go:143-144`, `session_store.go:96-106`,
  `machine.go:18,62` — all unchanged.

**Verification to perform during Phase 1:** confirm no other code path regenerates or caches the
instanceID per-WebSocket (grep for `InstanceID`/`instanceID` across `internal/agent` and
`internal/hub`). Current grep shows the only generation site is `main.go:199`.

## 6. Phased plan

### Phase 1 — Extract host + local relay; connect becomes a pure relay (sessions survive connect restart)

Steps:
1. **`internal/transport/local.go`** (new): add `LocalProtocolVersion` to `protocol.go`; add
   `HostHello`, `HostInfo`, `ListSessions` types + constructors mirroring `messages.go` style.
2. **`internal/agent/adapter/primary/localhost/`** (new): `server.go` accepts a UDS `net.Conn`, runs
   `transport.Server`, accepts a control stream, performs the local handshake (replies `HostInfo`
   with the host-owned instanceID + current `ListSessions` from `Manager`), then dispatches frames
   into the existing `session.Manager`. **Reuse the dispatch logic** that currently lives in
   `hubclient/control.go:26-103` (OpenSession/Resize/CloseSession/EnableSnaps) — extract it into a
   shared helper both adapters call, or duplicate the thin switch. Data streams: accept connect-opened
   streams, read `AttachHeader`, call `Manager.Attach` (mirror of `hubclient/streams.go:12-26`).
3. **`internal/agent/adapter/secondary/hostclient/`** (new): `client.go` dials the UDS, runs
   `transport.Client`, performs the handshake, and implements the interfaces `hubclient` already
   consumes: `hubclient.SessionManager` (`client.go:25-32`) by **opening local data streams to the
   host** for `Attach`, and forwarding `Open`/`Resize`/`Close`; plus `SnapshotSink` and
   `MetricsSampler` shims. Exposes `InstanceID()` from `HostInfo`.
4. **`cmd/agent/main.go`**: add `session-host` subcommand (`main.go:70-89` dispatch + usage at
   `main.go:92-112`). Its composition root builds `session.NewManager(pty.Factory{}, …)`
   (moved from `main.go:201`), sets the vt screen factory + snapshot producer (moved from
   `main.go:216-219`), generates the instanceID, and runs the `localhost` server on the UDS.
5. **Rewire `cmdConnect`** (`main.go:150-234`): instead of constructing the Manager locally, construct
   a `hostclient` (dial UDS), source instanceID from it, and pass it as `hubclient.Config.Sessions`
   /`Metrics`/snapshot sink. The snapshot producer (`snapshot.New`) **moves to the host** (see Phase
   2); for Phase 1 connect can keep relaying snapshot frames received from the host onto the hub's
   snapshot stream.
6. **Auto-spawn (dev/non-systemd, HARD PART #1):** if the UDS does not respond, connect double-forks /
   `setsid`-spawns `constellate-session-host` so the host is **not in connect's process group** and
   survives connect exit. Implementation: `exec.Command(self, "session-host", …)` with
   `SysProcAttr{Setsid: true}` and detached stdio; poll the socket until it answers.
   Document this in `usage.agent.md`.
7. **Config:** add `runtime_dir` (and derived `socket_path`) to
   `internal/platform/config/agent.go:11-20` + env override in `applyAgentEnv` (`agent.go:61-77`);
   default to `$XDG_RUNTIME_DIR/constellate/host.sock` else a configured dir.

**Acceptance test (Phase 1):** open a session, start `top`, kill the **connect** process; within
backoff, connect (or systemd) restarts, re-handshakes the still-running host (same instanceID), the
hub never marks the session `lost` (stays `running`), the browser reattaches and scrollback replays
with `top` still live. Add as a `make test-docker` scenario (see §8).

### Phase 2 — Move snapshot / activity / heartbeat production into the host

- Move `snapshot.New(mgr, sink, …)` construction and `mgr.SetScreenFactory` /
  `mgr.SetNotifier` wiring from `cmd/agent/main.go:216-219` into the `session-host` root. The host
  runs the snapshot `Producer` (`producer.go`) against its own Manager and pushes `Snapshot` frames to
  connect over the local snapshot stream; connect relays them onto the hub snapshot stream
  (`hubclient/client.go:184-243`).
- Activity (`Manager.Activities`, `manager.go:280-309`) is computed on the host and shipped to connect
  in a periodic local stat frame; connect folds it into the hub `Heartbeat`
  (`client.go:391-401`). `EnableSnaps` from the hub is relayed connect→host
  (`control.go:77-89` toggle logic moves behind the local protocol).
- **Resync on connect restart (HARD PART #5):** the Phase-1 handshake already returns `ListSessions`
  in `HostInfo`; on every (re)connect, connect re-subscribes so heartbeat/activity/snapshot
  production resyncs. `EnableSnaps{true}` is re-sent if the hub has viewers
  (`inbound.go:80-85`), and the producer's `SetEnabled(true)` clears `lastRev`
  (`producer.go:52-59`) so tiles repaint — this existing mechanism covers the relay restart for free.

### Phase 3 — Lifecycle hardening

- **systemd unit (HARD PART #1, BOTH approach):** add `deploy/systemd/constellate-session-host.service`
  (durable; `Restart=on-failure`) and make the connect unit (generated by `renderUnit` at
  `main.go:478-512`) declare `Requires=`/`After=constellate-session-host.service`. Update `install`
  (`cmdInstall`, `main.go:514-656`) to write both units. Host must run in its own group (systemd gives
  this naturally; the auto-spawn path uses `setsid`).
- **`agent update` host-preserving path (HARD PART #2):** `cmd/agent/update.go` restarts **connect
  only**; the host keeps running. Add a deliberate `--restart-host` / host-drain path for when a
  host upgrade is required: connect signals the host to drain (stop accepting new sessions, optionally
  finish), then the new host starts and connect re-handshakes. Old-host-serves-new-connect is
  guaranteed by the negotiated `LocalProtocolVersion` (§4). `update.sh` and `--no-restart`
  (`update.go:24,174`) gain host-awareness.
- **Single-client lease (HARD PART #3):** the `localhost` server accepts exactly one connect at a
  time; a second connect is rejected (or queued) with a clear local `Error`. Implement as a mutex/lease
  on the accept loop in `localhost/server.go`.
- **Socket security (HARD PART #4):** create the UDS under `$XDG_RUNTIME_DIR` (or configured
  `runtime_dir`) with the dir `0700` and the socket `0600`, owned by the service user; verify peer
  uid on accept (SO_PEERCRED) and reject mismatches.
- **Version negotiation** finalized and tested (§4) including the skew matrix (§8).

### Phase 4 — DESIGN.md update + docs + tests

- Update `DESIGN.md` (see §7 below), `docs/usage.agent.md` (host/connect split, auto-spawn, units),
  add an ADR `docs/adr/0004-session-host-split.md`.
- Land the test matrix (§8).

## 7. DESIGN.md changes (reverses D6)

- **`DESIGN.md:88` (D6 row):** change the decision from "In-process PTY on the agent + scrollback
  buffer … an agent restart dropping its sessions is an accepted trade-off" to "**Split agent:
  durable session-host (owns PTYs/scrollback) + volatile connect relay** — sessions survive a connect
  restart/update; only host death or machine reboot loses them." Note that it **supersedes** the
  original D6 and reference the new decision record.
- **§5.4 "Detach vs close / Lost" (`DESIGN.md:199-205`):** rewrite the **Lost** bullet — sessions are
  marked `lost` only when the **session-host** instanceID changes (host death / reboot), not on a
  connect restart. Keep Detach/Close unchanged.
- **§7 / §8 persistence notes (`DESIGN.md:275-314`):** clarify scrollback lives in **host RAM**, survives
  connect restart automatically, and is still **not** persisted to disk (reboot loses it — by design).
- **§4.1 / §12 tree:** document the two host-side process roles and add
  `deploy/systemd/constellate-session-host.service` + the local-protocol packages
  (`adapter/primary/localhost`, `adapter/secondary/hostclient`).
- **§18:** add a milestone/decision record entry for the split.
- **New decision record** (append to the locked-decisions table or §18): "**D8 — session-host/connect
  split**, supersedes D6," with rationale (full survival, pure-Go, no tmux, no fd-passing) and the
  rejected alternatives (tmux, SCM_RIGHTS fd-passing).

## 8. Test matrix

| Test | Where | Asserts |
|------|-------|---------|
| connect-restart survival | `test/docker/run.sh` (new scenario) + `test/integration` | Open session + `top`; kill the connect process; session stays `running` on the hub (never `lost`); browser reattaches; scrollback replays live. Mirror the existing restart scenario at `test/docker/run.sh:86-96` but kill **connect only**, host stays up. |
| host-death → lost | `test/docker` | Kill the **session-host**; restart it (new instanceID); hub marks sessions `lost` (validates `registry/usecase.go:61` still fires). This replaces the old "restart-loses-sessions" semantics. |
| version skew (new connect, old host) | `test/integration` | Handshake with mismatched `LocalProtocolVersion`; assert connect operates within the negotiated window and an old host serves a new connect's core commands. |
| single-client lease | `test/integration` | Second connect to the UDS is rejected/queued. |
| socket perms | unit/integration | UDS is `0600` under the runtime dir; non-owner connection refused. |
| auto-spawn | `test/e2e` (non-systemd) | Starting connect with no host spawns a detached host (own process group); killing connect leaves the host alive. |

Add the connect-restart scenario as a first-class `make test-docker` case (the Makefile target
exists; `test/docker/run.sh` is the harness — `DESIGN.md:817-826` describes this tier).

## 9. Open questions / risks

1. **Snapshot relay overhead in connect (Phase 1 interim).** Before Phase 2 moves the producer to the
   host, connect would relay snapshot frames; keep Phase 1 minimal (host produces, connect relays) to
   avoid a throwaway design.
2. **Data-stream backpressure across two yamux hops.** The hub→connect→host path now has two muxed
   links; verify yamux flow control composes (it should — `mux.go` uses default config with built-in
   backpressure) and that a slow browser can't stall the host's `readPump` (it can't today —
   scrollback is decoupled, `manager.go:325-354`).
3. **`agent reset` / re-enroll** while the host holds live sessions — define behavior (host keeps
   PTYs; connect just loses credentials).
4. **Windows/macOS** UDS + `setsid` portability; `install` is Linux-only today
   (`main.go:525-528`), so the host-split lands Linux-first.
5. **Host drain semantics** on `update --restart-host`: finish-in-flight vs. immediate — needs an
   operator-facing decision.
6. **SO_PEERCRED availability** for the lease/perms check is Linux-specific; gate behind build tags.
