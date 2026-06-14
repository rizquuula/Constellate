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

Next: M6 (progress dashboard — activity/status rollups across machines and projects). Milestone roadmap in `DESIGN.md` §18.

## M1 conventions worth knowing
- Control stream: agent-opened/hub-accepted. **Data streams: hub-opened/agent-accepted**, first line is
  `transport.AttachHeader{sessionID}`, then raw PTY bytes. `/ws/term` uses **binary** frames for terminal
  I/O and **text** frames (`{"type":"resize",...}`) for resize.
- Frontend builds to `web/dist` (gitignored except `.gitkeep`) and embeds via `web/embed.go`
  (`//go:embed all:dist`). `make build` runs `make web` first; the hub Docker image has a node stage.
