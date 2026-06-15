# CLAUDE.md

Self-hosted control plane: a hub (public VPS) brokers browser ↔ per-machine agents that **dial home**
and own the PTYs. **[`DESIGN.md`](DESIGN.md) is canonical** — read it before non-trivial work.

## Must know
- **Go 1.25**, but local toolchain may be older → always `export GOTOOLCHAIN=auto` before `go` commands
  (the Makefile/CI/Dockerfiles already do).
- **`CGO_ENABLED=0`** everywhere — deps are pure Go (incl. `modernc.org/sqlite`), so binaries are static
  and run on distroless/scratch. Keep it that way; don't add cgo deps.
- Two hexagons in one module — `internal/hub` and `internal/agent` — sharing only `internal/transport`
  (wire protocol) and `internal/platform` (log/id/config/version). **Neither context imports the other.**
- Layering: `domain/` is pure stdlib; `app/<usecase>` is glue and declares the SPI it needs in its own
  `ports.go`; adapters split `primary/` (driving) and `secondary/` (driven) and translate DTOs at the
  boundary. `cmd/*/main.go` is the only wiring.
- M5 landed **auth + TLS**: dev token is gone. Agents enroll via `agent enroll --hub … --token …`
  (Ed25519 keypair; hub stores only the public key; dial-home is a signed bearer assertion). Operator
  logs in via TOTP + recovery codes + WebAuthn passkey (`hub operator add` for bootstrap). Hub serves
  behind Caddy or direct HTTPS; agent verifies hub cert via `hub_ca`. The `constellate_session` cookie
  gates all `/api/*` + `/ws/*` routes.

## Commands
- `make build` · `make test` (unit + integration + in-proc E2E) · `make test-docker` (hub + 2 agent
  containers) · `make lint` (golangci-lint **v2** config).
- Binaries: `constellate-hub serve|migrate|version` · `constellate-agent connect|status|version`.

## Status
Protocol is now **4** (window [1,4]): `Heartbeat` carries an optional `metrics` object
(`cpuPercent`, `memUsedMB`, `memTotalMB`) sampled via gopsutil; hub stashes it in-memory on
the live `Conn` and surfaces it on the machine DTO; sidebar shows `12% · 5.4/16 GB` under the
machine name while online. Additive; older peers ignore it.

M7 done (AI-session awareness): the agent derives per-session **activity** (active/idle/
awaiting-input) from output timing (~2 s window), **OSC 133** shell-integration prompt markers
parsed by the vt emulator, and a short-line screen-tail question heuristic; it ships in each
`Heartbeat` (`SessionStat.activity`) — **protocol bumped to 3**, window **[1,3]** (additive). The
hub persists it to `sessions.activity` (best-effort), surfaces it on the session DTO, and the
dashboard adds active/idle/awaiting-input totals + an `awaiting_input` attention kind. Frontend
shows an **activity badge** (active/idle/**needs input**, colorblind-distinct, reduced-motion-safe)
on sidebar rows, overview tiles, and the dashboard. Opt-in OSC 133 shell hook documented in
`docs/shell-integration.md`. Decisions folded into `DESIGN.md` §6/§18.

M6 done (progress dashboard): a server-side aggregation use case (`app/dashboard`) composes the
machine/session/project/audit read ports + live-agent presence into one `View` — fleet totals,
per-machine + per-project **status rollups** (running/exited/lost, with an "Ungrouped" bucket), an
**attention list** (lost sessions; offline machines with running sessions), and the 20 most recent
audit events. Session-gated `GET /api/dashboard`; frontend adds a third **Dashboard** view
(summary cards, rollups, attention banner, activity feed) polling only while active. Per-session
*activity* (active/idle/awaiting-input) is deferred to M7. Decisions folded into `DESIGN.md` §18.

M5 done (auth + audit hardening): **agent enrollment** — `hub enroll-token` + `agent enroll`
generates an Ed25519 keypair; hub stores only the public key; dial-home presents a
**agent-signed bearer assertion** (`v1.<machineID>.<ts>.<sig>`) — hub holds no signing secret.
Revocation is soft (`machines.revoked_at`); `hub machines` / `hub revoke` / `agent reset` wired.
**Protocol stays at 2** (credential rides `Authorization` header, not `Hello`). **Operator auth** —
TOTP (`pquerna/otp`) + single-use recovery codes + WebAuthn passkeys (`go-webauthn`); server-side
sessions in `operator_sessions` (migration 0004); opaque cookie `constellate_session` (HttpOnly,
SameSite=Lax, Secure, 24 h). Rate limiting (per-IP + global) + TOTP single-use anti-replay.
**Auth middleware** gates all `/api/*` + `/ws/*`; explicit allowlist for unauthenticated paths.
**Audit log** wired via `AuditSink` port in `attach`, `sessions`, `enroll`, `auth` use cases.
**TLS** via Caddy (`deploy/caddy/Caddyfile` + `deploy/compose.yaml`) or optional in-app
(`tls.{cert,key}`). Dev token removed everywhere. New tables: `enroll_tokens` (0003),
`operator_sessions` (0004), `machine_credentials` (stores Ed25519 public key). Decisions folded
into `DESIGN.md` §5.1/§6/§8/§10/§13/§14.

M4 done (mission-control overview): agent runs an **in-repo pure-Go VT emulator**
(`adapter/secondary/vt`, Williams parser + ECMA-48/VT100) producing a full-color cell grid; a
throttled, change-gated **snapshot producer** (`app/snapshot`) ships **RLE full-color** `Snapshot`
records over an **agent-opened NDJSON snapshot stream** when the hub enables it via `EnableSnaps`. Hub
**overview** context (`app/overview`) caches latest-per-session + fans out to `GET /ws/overview`;
viewer presence gates `EnableSnaps` (zero snapshot bandwidth when nobody watches). Frontend adds a
**Workspace↔Overview** toggle, a color tile grid, and **click-to-dive**. Protocol bumped to **2**
(window [1,2], backward compatible). Decisions folded into `DESIGN.md` §4.3/§6/§7.2/§13.

M3 done (multi-session + projects): new **project** bounded context (`domain/project`, `app/projects`,
sqlite + memory `ProjectStore`); REST `GET/POST /api/projects` and session rename `PATCH
/api/sessions/{id}` (metadata only, no wire change). Sessions may be **project-less** (nullable
`project_id`, an "Ungrouped" bucket); **no project delete in M3**. Frontend is a **recursive
split-pane terminal workspace** (react-resizable-panels) — each pane leaf binds one live session,
split H/V — plus a project-grouped sidebar. Decisions/innovations folded into `DESIGN.md` §9/§18/§19.

M2 (persistent terminals): agent keeps a per-session **scrollback ring buffer** and **replays it on
attach**; session manager is broadcast-buffer + per-attach drain. Agent **process restart** → its
`running` sessions marked `lost`, detected via a per-process `instanceID` in `Hello`.

Post-M7: **project delete** added — `DELETE /api/projects/{id}` (session-gated) removes a project;
the use case **refuses with 409** (`projects.ErrHasSessions`) if the project still owns any
session, so sessions are never orphaned or cascade-deleted (reassign/close them first). Sidebar
project headers gained a trash button with inline confirm + 409-aware error. Persistence ports
grew `ProjectStore.Delete` and a `SessionCounter` (sessions-by-project) SPI.

Next: all roadmap milestones (M0–M7) are complete **and the full test matrix has been run
end-to-end and passes** — `make test` (unit + integration + in-proc E2E), `make test-e2e`
(single-machine Playwright), and `make test-docker` (hub + 2 agent containers). Milestone roadmap
in `DESIGN.md` §18.

## M1 conventions worth knowing
- Control stream: agent-opened/hub-accepted. **Data streams: hub-opened/agent-accepted**, first line is
  `transport.AttachHeader{sessionID}`, then raw PTY bytes. `/ws/term` uses **binary** frames for terminal
  I/O and **text** frames (`{"type":"resize",...}`) for resize.
- Frontend builds to `web/dist` (gitignored except `.gitkeep`) and embeds via `web/embed.go`
  (`//go:embed all:dist`). `make build` runs `make web` first; the hub Docker image has a node stage.
