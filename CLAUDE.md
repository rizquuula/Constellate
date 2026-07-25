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
- **Auth + TLS** are in place: there is no dev token. Agents enroll via `agent enroll --hub … --token …`
  (Ed25519 keypair; hub stores only the public key; dial-home is a signed bearer assertion). Operator
  logs in via TOTP + recovery codes + WebAuthn passkey (`hub operator add` for bootstrap). Hub serves
  behind Caddy or direct HTTPS; agent verifies hub cert via `hub_ca`. The `constellate_session` cookie
  gates all `/api/*` + `/ws/*` routes.

## Commands
- `make build` · `make test` (unit + integration + in-proc E2E) · `make test-docker` (hub + 2 agent
  containers) · `make lint` (golangci-lint **v2** config).
- Binaries: `constellate-hub serve|migrate|version` · `constellate-agent connect|status|update|version`.

## Releasing
The hub and agent **version independently** via `cmd/hub/VERSION` and `cmd/agent/VERSION` (plain semver,
e.g. `0.1.1`); the Makefile reads them and bakes them in via `-ldflags`. A push of a **datetime
"release-train" tag** `v<YYYYMMDD>-<HHMM>` (e.g. `v20260615-1546`) triggers `.github/workflows/release.yaml`,
which builds binaries + GHCR images and cuts a GitHub Release. The tag is a neutral umbrella — the real
per-binary versions are read from the two `VERSION` files at build time. To cut a release:
1. Bump `cmd/hub/VERSION` and/or `cmd/agent/VERSION` (only those that changed).
2. Commit the bump (`chore(release): …`).
3. Tag the release commit with the datetime format: `git tag "v$(date -u +%Y%m%d-%H%M)"`.
4. Push commit then tag: `git push origin HEAD && git push origin <tag>` — the workflow starts on the tag push.

**Before any push, run `make lint` (golangci-lint v2) and ensure it passes.**

## Status
All planned features are implemented; the full test matrix has been run end-to-end and passes —
`make test` (unit + integration + in-proc E2E), `make test-e2e` (single-machine Playwright), and
`make test-docker` (hub + 2 agent containers). **Wire protocol is 6** (supported window [1,6]); the
milestone roadmap and decision history live in `DESIGN.md` §18.

**Live terminals + persistence.** The agent spawns a PTY per session, keeps a per-session
**scrollback ring buffer** and **replays it on attach** (session manager = broadcast-buffer +
per-attach drain). An agent **process restart** marks its `running` sessions `lost`, detected via a
per-process `instanceID` in `Hello`. A mere **link drop** is different: it parks that machine's
`running` sessions in a fourth status, **`disconnected`** (PTY presumed alive, machine unreachable),
and a same-`instanceID` reconnect restores them to `running`. `MarkRunningLost` and
`AutoRelaunchSessions` therefore match `status IN ('running','disconnected')`.

**Multi-session + projects.** A **project** bounded context (`domain/project`, `app/projects`,
sqlite + memory `ProjectStore`); REST `GET/POST /api/projects` and session rename `PATCH
/api/sessions/{id}` (metadata only, no wire change). Sessions may be **project-less** (nullable
`project_id`, an "Ungrouped" bucket). Frontend is a **multi-window** workspace: several named
**windows** (bottom tab strip, `features/terminal/windowList.ts`), each owning its own **recursive
split-pane tree** (react-resizable-panels) where each leaf binds one live session, split H/V — plus a
project-grouped sidebar. The whole thing (windows + focus + split sizes) persists to `localStorage`
under `constellate.workspace` at schema v2; the old `constellate.paneRoot`/`…focusedPaneId` keys are
read once for a one-time migration, then deleted. **Project delete**: `DELETE /api/projects/{id}` (session-gated) **refuses with 409**
(`projects.ErrHasSessions`) if the project still owns any session (never orphaned or
cascade-deleted — reassign/close first); sidebar trash button with inline confirm + 409-aware error.
Persistence ports include `ProjectStore.Delete` and a `SessionCounter` (sessions-by-project) SPI.

**Mission-control overview.** The agent runs an **in-repo pure-Go VT emulator**
(`adapter/secondary/vt`, Williams parser + ECMA-48/VT100) producing a full-color cell grid; a
throttled, change-gated **snapshot producer** (`app/snapshot`) ships **RLE full-color** `Snapshot`
records over an **agent-opened NDJSON snapshot stream** when the hub enables it via `EnableSnaps`. Hub
**overview** context (`app/overview`) caches latest-per-session + fans out to `GET /ws/overview`;
viewer presence gates `EnableSnaps` (zero snapshot bandwidth when nobody watches). Frontend has a
**Workspace↔Overview** toggle, a color tile grid, and **click-to-dive**.

**Auth + audit hardening.** **Agent enrollment** — `hub enroll-token` + `agent enroll` generates an
Ed25519 keypair; hub stores only the public key; dial-home presents an **agent-signed bearer
assertion** (`v1.<machineID>.<ts>.<sig>`, on the `Authorization` header) — hub holds no signing
secret. Revocation is soft (`machines.revoked_at`); `hub machines` / `hub revoke` / `agent reset`
wired. Machine lifecycle is also REST + UI: `POST /api/machines/{id}/revoke|unrevoke` and `DELETE
/api/machines/{id}`, the last **refusing with 409** (`enroll.ErrNotRevoked`) unless already revoked,
then cascading sessions → projects → credentials → machine in one transaction (app-level, **not** an
FK `ON DELETE CASCADE`). Un-revoke re-arms the *existing* keypair — nothing is rotated. **Operator auth** — TOTP (`pquerna/otp`) + single-use recovery codes + WebAuthn passkeys
(`go-webauthn`); server-side sessions in `operator_sessions`; opaque cookie `constellate_session`
(HttpOnly, SameSite=Lax, Secure, 24 h). Rate limiting (per-IP + global) + TOTP single-use
anti-replay. **Auth middleware** gates all `/api/*` + `/ws/*`; explicit allowlist for unauthenticated
paths. **Audit log** wired via `AuditSink` port in `attach`, `sessions`, `enroll`, `auth` use cases.
**TLS** via Caddy (`deploy/caddy/Caddyfile` + `deploy/compose.yaml`) or optional in-app
(`tls.{cert,key}`). No dev token anywhere. Tables: `enroll_tokens`, `operator_sessions`,
`machine_credentials` (stores the Ed25519 public key).

**Progress dashboard.** A server-side aggregation use case (`app/dashboard`) composes the
machine/session/project/audit read ports + live-agent presence into one `View` — fleet totals,
per-machine + per-project **status rollups** (running/exited/lost/disconnected, with an "Ungrouped"
bucket), an **attention list** (`lost_session`, `disconnected_sessions` — an offline machine still
owning disconnected sessions — and `awaiting_input`), and the 20 most recent
audit events. Session-gated `GET /api/dashboard`; frontend has a third **Dashboard** view (summary
cards, rollups, attention banner, activity feed) polling only while active.

**AI-session awareness.** The agent derives per-session **activity** (active/idle/awaiting-input)
from output timing (~2 s window), **OSC 133** shell-integration prompt markers parsed by the vt
emulator, and a short-line screen-tail question heuristic; it ships in each `Heartbeat`
(`SessionStat.activity`). The hub persists it to `sessions.activity` (best-effort), surfaces it on
the session DTO, and the dashboard adds active/idle/awaiting-input totals + an `awaiting_input`
attention kind. Frontend shows an **activity badge** (active/idle/**needs input**,
colorblind-distinct, reduced-motion-safe) on sidebar rows, overview tiles, and the dashboard. Opt-in
OSC 133 shell hook documented in `docs/shell-integration.md`.

**Host metrics.** `Heartbeat` carries an optional `metrics` object (`cpuPercent`, `memUsedMB`,
`memTotalMB`) sampled via gopsutil; the hub stashes it in-memory on the live `Conn` and surfaces it
on the machine DTO; the sidebar shows `12% · 5.4/16 GB` under the machine name while online.
Additive; older peers ignore it.

**Mobile keypad.** On coarse-pointer devices the native virtual keyboard is suppressed
(`inputmode="none"` + `readOnly` on xterm's helper textarea, `features/terminal/inputMode.ts`) because
xterm.js IME composition replays already-sent characters when punctuation commits a word; input comes
instead from an in-app on-screen keypad (`Keypad.tsx` over the pure-data `keypadLayout.ts` — command
row + `letters`/`symbols`/`fn` layers, all exactly `LAYER_ROWS` tall so a layer switch never
ResizeObserver-resizes the PTY), with a ⌨ escape hatch back to the native keyboard.

**Resilient terminal sockets.** `/ws/term` carries a `{"type":"hb"}` keepalive; the hub splits
liveness into a **prober** (never fatal) and a **reaper** (sole teardown authority, fires only after
the peer has been silent past 4× the interval), and maps failures to real close codes instead of a
universal 1006. The browser reconnects **indefinitely** with capped jittered backoff (`api/reconnect.ts`
— pure, unit-tested: 300 ms ×2 → 15 s cap, ±20 % jitter), reaching `stopped` only on a terminal close
code. A 45 s staleness watchdog arms at socket open and drives the reconnect path directly rather
than trusting a zombie socket to deliver `onclose`. Because the agent replays scrollback on *every*
attach, the client calls `term.reset()` on the first binary frame of a re-attach (tracked at hook
level via `replayedRef`) so a replay never doubles. Keystrokes are **not** buffered while
disconnected. `useSnapshots` (overview) shares the same policy module.

**Mobile + PWA.** Installable PWA — `web/public/manifest.webmanifest`, icons, and a **network-first,
no-precache** service worker (`web/public/sw.js`) that never touches `/api/*` or `/ws/*`, so an online
client can never be served stale fleet state. `httpapi/server.go`'s `init()` hand-registers the
`.webmanifest` and `.ico` MIME types — browsers reject a sniffed manifest. Static paths are ungated
**by construction** (the middleware only gates `/api/` and `/ws/` prefixes). Responsive contract is
one module, `web/src/breakpoints.ts` (`PHONE_MAX=600`, `TABLET_MAX=900`, `useCoarsePointer`,
`confirmTimeoutMs()`); the px values are hand-mirrored in `styles.css` `@media` blocks — **no
build-time bridge, keep them in sync**. Tablet gets a drawer sidebar, phone a single-leaf pane view
plus a header overflow menu and safe-area insets.

## Conventions worth knowing
- Control stream: agent-opened/hub-accepted. **Data streams: hub-opened/agent-accepted**, first line is
  `transport.AttachHeader{sessionID}`, then raw PTY bytes. `/ws/term` uses **binary** frames for terminal
  I/O and **text** frames for control: `{"type":"resize",...}` browser→hub, `{"type":"hb",...}`
  hub→browser keepalive. Close codes 4404 (not found) and 4410 (session ended) are terminal for the
  client; 4503 (agent offline) and the rest are retried indefinitely with capped backoff.
- Frontend builds to `web/dist` (gitignored except `.gitkeep`) and embeds via `web/embed.go`
  (`//go:embed all:dist`). `make build` runs `make web` first; the hub Docker image has a node stage.
