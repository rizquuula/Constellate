# 10 · Operations

This page is the reference for running Constellate: configuration, the two Docker stacks, the
binary/systemd path, install/update, releases, and a troubleshooting table. For step-by-step
walkthroughs see the task guides: [`usage.binary.md`](usage.binary.md),
[`usage.docker.md`](usage.docker.md), [`usage.agent.md`](usage.agent.md).

---

## Configuration

YAML file via `--config`, with per-field `CONSTELLATE_*` env overrides for containers. Samples in
`configs/`. Defaults below are from `internal/platform/config/{hub,agent}.go`.

### Hub (`internal/platform/config/hub.go`)

| YAML key | Default | Env override |
|----------|---------|--------------|
| `addr` | `127.0.0.1:8080` | `CONSTELLATE_ADDR` |
| `public_url` | `""` | `CONSTELLATE_PUBLIC_URL` |
| `db_path` | `./constellate.db` | `CONSTELLATE_DB_PATH` |
| `enroll_token_ttl` | `15m` | `CONSTELLATE_ENROLL_TOKEN_TTL` |
| `session_ttl` | `24h` | `CONSTELLATE_SESSION_TTL` |
| `tls.cert` / `tls.key` | `""` / `""` | — |
| `webauthn.rp_id` / `webauthn.origins` | derived from `public_url` | — |
| `log.level` / `log.format` | `info` / `text` | — |

`public_url` is load-bearing: it drives the session-cookie `Secure` flag (`https…` ⇒ Secure) and the
WebAuthn RP-ID/origins.

`session_ttl` is a **sliding** window, not a fixed one: each authenticated request pushes the
operator session's expiry out to `now + session_ttl`, so it bounds *idle* time, not total time. There
is no absolute cap — logging out (or `DELETE`ing the row) is what ends a session that stays in use.

### Agent (`internal/platform/config/agent.go`)

| YAML key | Default | Env override |
|----------|---------|--------------|
| `hub_url` | `""` (sample: `ws://127.0.0.1:8080/ws/agent`) | `CONSTELLATE_HUB_URL` |
| `name` | `os.Hostname()` | `CONSTELLATE_NAME` |
| `id_file` | `~/.constellate/agent-id` | `CONSTELLATE_ID_FILE` |
| `cred_file` | `~/.constellate/cred` | `CONSTELLATE_CRED_FILE` |
| `hub_ca` | `""` (system roots) | `CONSTELLATE_HUB_CA` |
| `default_shell` | `""` (falls back to `/bin/bash`) | — |
| `scrollback_bytes` | `262144` (256 KiB) | — |
| `persist_scrollback` | `true` | — |
| `scrollback_dir` | `$XDG_DATA_HOME/constellate/scrollback` else `~/.constellate/data/scrollback` | — |
| `scrollback_disk_cap_bytes` | `67108864` (64 MiB) | — |
| `runtime_dir` | `$XDG_RUNTIME_DIR/constellate` else `~/.constellate/run` (holds `host.sock`) | `CONSTELLATE_RUNTIME_DIR` |
| `log.level` / `log.format` | `info` / `text` | — |

> `hub_url` is a **WebSocket** URL ending in `/ws/agent` (`wss://…`), not the HTTP enroll base. Mixing
> them up is the top reason `connect` fails right after a successful `enroll`. See
> [`usage.agent.md`](usage.agent.md).

---

## Two Docker stacks

| | Dev (`deploy/compose.dev.yaml`) | Prod (`deploy/compose.yaml`) |
|---|---|---|
| Make targets | `ddocker-up/down/totp/logs/reset` | `docker-up/down/logs` |
| TLS | none — plain `http://localhost:8080` | Caddy auto-HTTPS |
| Services | `hub` + `agent-alpha` + `agent-beta` (agents in containers) | `hub` (internal only) + `caddy` (`:80`/`:443`, `+443/udp` for HTTP/3) |
| Hub bind | `127.0.0.1:8080` published | `0.0.0.0:8080`, **not** published — only via Caddy |
| Public URL | unset (cookie not `Secure`) | `https://${CONSTELLATE_DOMAIN}` |
| Volumes | `agent-*-id` | `hub-data`, `caddy-data`, `caddy-config` |

```mermaid
graph LR
    subgraph prod["prod: deploy/compose.yaml"]
        C["caddy:2<br/>:80 / :443 (+udp)"]
        HB["hub<br/>ghcr.io/rizquuula/constellate-hub<br/>0.0.0.0:8080 (internal net only)"]
        C -->|reverse_proxy hub:8080| HB
        HB --> V[("hub-data volume")]
    end
    BR["browser + agents"] -->|WSS/HTTPS| C

    style HB fill:#336791,color:#fff
    style C fill:#f59e0b,color:#000
```

The Caddyfile sets `default_sni {$CONSTELLATE_DOMAIN}` so a **bare-IP** deployment still presents a
cert when clients send no SNI; Caddy skips Let's Encrypt for an IP and issues from its internal CA
(export that CA to agents as `hub_ca`). Host ports are overridable via `CADDY_HTTP`/`CADDY_HTTPS`.
Full walkthrough incl. the bare-IP path: [`usage.docker.md`](usage.docker.md).

**Images** — `deploy/hub.Dockerfile` is a 3-stage build (`node:22` web → `golang:1.25`
`CGO_ENABLED=0` cross-compile → `gcr.io/distroless/static-debian12`). The agent runs on the **host**
in production (a containerized agent would only reach the container's own shell); its Dockerfiles exist
only for the topology tests. All three Dockerfiles pin `GOPROXY=https://goproxy.cn,direct` — a
China-region proxy; override the build arg if that host is slow/unreachable for you.

---

## Binary + systemd path

`make build` → `bin/constellate-hub` and `bin/constellate-agent` (static, version-stamped). On a
machine, `constellate-agent install` writes and starts **two** units in order:

```mermaid
graph TD
    SH["constellate-session-host.service<br/>ExecStart: … session-host<br/>Restart=on-failure"] --> CX["constellate-agent.service<br/>ExecStart: … connect<br/>Restart=always<br/>Requires= + After= the host unit"]
    style SH fill:#2d7d46,color:#fff
    style CX fill:#f59e0b,color:#000
```

`install --rootless` writes user units under `~/.config/systemd/user/` (no sudo; enable lingering to
survive logout). `uninstall` reverses it (connect first); `--purge` also drops the local enrollment.
Rationale for the two-unit split — session survival across restarts — is
[03 · Agent & sessions](03-agent-and-sessions.md).

---

## Install & update scripts

- **`install.sh`** — `curl -fsSL …/install.sh | sh`. Downloads the agent for your OS/arch from the
  latest release, verifies its **SHA-256** against `SHA256SUMS`, installs to `/usr/local/bin`
  (or `~/.local/bin` with `--rootless` / `CONSTELLATE_ROOTLESS=1`; `BIN_DIR` overrides). If both
  `CONSTELLATE_HUB` and `CONSTELLATE_TOKEN` are set, it enrolls in the same run. `CONSTELLATE_VERSION`
  pins a release.
- **`update.sh`** / `constellate-agent update` — checksum-verified **atomic swap** (`.bak` rollback on
  failure), then restarts **connect only** (`constellate-agent.service`) so sessions survive.
  `--restart-host` also restarts the session-host (**ends sessions**). `--check`, `--force`,
  `--no-restart`, `--rootless` supported; env: `CONSTELLATE_BIN`, `CONSTELLATE_NO_RESTART`, etc.

---

## Versioning & releases

Two axes, deliberately separate:

- **Per-binary semver** — `cmd/hub/VERSION` (`0.1.19` at the time of writing) and `cmd/agent/VERSION`
  (`0.1.5`) bump
  independently; the Makefile bakes each into its binary via `-ldflags -X …/version.Version=…`.
- **Wire protocol** — `transport.ProtocolVersion` (`6`) is the real compatibility gate, negotiated in
  `Hello` ([04 · Wire protocol](04-wire-protocol.md)). Interop depends only on the protocol window,
  never on release labels — which is what makes independent binary versions safe.

A **datetime "release-train" tag** `v<YYYYMMDD>-<HHMM>` (e.g. `v20260615-0830`) pushed to the repo
triggers `.github/workflows/release.yaml`. The tag is a neutral umbrella; the real per-binary versions
come from the two `VERSION` files at build time. Release builds: cross-compiled binaries
(`linux/darwin × amd64/arm64`, `CGO_ENABLED=0`) + `SHA256SUMS` + `update.sh` as GitHub Release assets,
plus multi-arch GHCR images (`ghcr.io/rizquuula/constellate-hub`, `…-agent`). **Run `make lint`
(golangci-lint v2) before any push** (`CLAUDE.md`).

---

## The service worker & what a deploy does to installed clients

The web app is an installable PWA (`web/public/manifest.webmanifest` + icons), which means some
operators are running it from a home screen with a **service worker** in front of every request.
That is normally where "why am I seeing the old build?" comes from — but not here, by design.

```mermaid
flowchart TD
    REQ["fetch event"] --> C{"GET · same-origin ·<br/>not /api/ · not /ws/ ?"}
    C -->|no| PASS["not intercepted at all<br/>REST + terminal I/O untouched"]
    C -->|yes| NET["try the network FIRST"]
    NET -->|"2xx"| OK["serve it · copy into cache"]
    NET -->|"non-ok"| OK2["serve it · do NOT cache"]
    NET -->|"throws · offline"| FB{"in cache?"}
    FB -->|yes| STALE["serve stale copy"]
    FB -->|no| ERR["propagate the error"]

    style PASS fill:#2d7d46,color:#fff
    style OK fill:#2d7d46,color:#fff
    style STALE fill:#f59e0b,color:#000
    style ERR fill:#dc2626,color:#fff
```

`web/public/sw.js` is **network-first with no precache**, and its header comment says why: *"a stale
cached shell could silently hide real fleet state."* Practical consequences for operations:

| Property | Consequence |
|---|---|
| Network-first for static assets | **An online client always gets the newly deployed build.** There is no stale-shell window after a hub upgrade |
| `/api/*` and `/ws/*` are never intercepted (`isCacheable`) | REST responses and terminal I/O can never be served from cache — no risk of a cached fleet state |
| Only `response.ok` is cached | A 404/500 during a rolling deploy is never frozen into the offline fallback |
| `skipWaiting()` + `clients.claim()` | A new worker takes over on the next load instead of waiting for every tab to close |
| `activate` deletes every cache but `CACHE_NAME` | Bumping `CACHE_NAME` (currently `constellate-v1`) force-evicts everything on the next load — the escape hatch if a client is ever genuinely wedged |

The cache exists for exactly one case: the device is **offline**, in which case the shell still
paints (and then fails to reach the hub, visibly).

---

## Troubleshooting (symptom → cause → fix)

| Symptom | Cause | Fix |
|---------|-------|-----|
| Machine in the list but **no shell button** | enrolled but offline — `connect` not running | start `connect` and supervise it ([`usage.agent.md`](usage.agent.md)) |
| `connect: hub_url is required` | `hub_url` missing from `agent.yaml` (the `--hub` enroll flag doesn't persist) | set `hub_url` in config |
| `not enrolled: run constellate-agent enroll …` | no local credential | run `enroll` first |
| `enroll: server error 4xx` | token expired or already used (one-time, short-lived) | mint a fresh `hub enroll-token` |
| Connects then immediately drops | `ws://` vs `wss://` mismatch, or `hub_url` points at the HTTP base not `…/ws/agent` | fix `hub_url` |
| `connect` can't verify the hub / x509 error | self-signed / private-CA hub cert | set `hub_ca` to the hub's CA PEM, or trust it system-wide |
| Machine still offline after `connect` | egress blocked | allow outbound to the hub's host:port (agents only dial out) |
| Sessions marked `lost` after `agent update` | the **session-host** unit was restarted (not just connect) | only restart `constellate-agent.service`; `--restart-host` intentionally ends sessions |
| Browser logs in but routes **401** | session cookie not set / not `Secure` over HTTPS | check `public_url` starts with `https` and you reach the hub via HTTPS |
| Passkey registration fails on a bare IP | WebAuthn needs a registrable domain | use TOTP + recovery, or front the hub with a hostname |
| Login says "code already used" | TOTP single-use anti-replay (matched step recorded) | wait for the next 30 s code; check the hub's clock (NTP) |
| Docker build stalls fetching modules | `GOPROXY=goproxy.cn` unreachable from your region | override the `GOPROXY` build arg |
| No "Install app" / "Add to home screen" prompt | manifest not served as `application/manifest+json`, or the hub isn't on HTTPS | both are prerequisites — check the MIME registration in `httpapi/server.go`'s `init()` and your TLS front end |
| Installed PWA shows an old build | Should not happen — the worker is network-first. Suspect a stuck worker, not the cache policy | unregister the worker in devtools, or bump `CACHE_NAME` in `web/public/sw.js` and redeploy |

---

## Where to go next

- Why the two-unit split exists: [03 · Agent & sessions](03-agent-and-sessions.md)
- Config field meanings in depth: [`usage.binary.md`](usage.binary.md)
- The test tiers behind CI: [11 · Testing](11-testing.md)
