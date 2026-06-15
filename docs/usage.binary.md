# Using Constellate — from binaries

This is the end-user guide for running Constellate **from the prebuilt binaries**: standing up the
**hub**, enrolling **agents** on your machines, and driving everything from the **web UI**. If you'd
rather run everything in containers, see the Docker guide,
[`usage.docker.md`](usage.docker.md). For the architecture and design rationale, see
[`DESIGN.md`](../DESIGN.md).

---

## What you get

Constellate is a self-hosted control plane for a fleet of developer machines:

- One **hub** (a single binary, typically on a public VPS) serves the web UI and brokers traffic.
- One **agent** per machine **dials home** to the hub (outbound only — no inbound ports on your
  machines) and owns the real PTYs.
- A browser gives you **live, persistent terminals** on every machine, grouped by project, with a
  mission-control **overview** of every running shell at a glance.

```
  browser ──HTTPS/WSS──▶  hub (VPS)  ◀──WSS (dial-home)──  agent ──▶ PTYs
                          web UI + SQLite                  (your machine)
```

The hub never connects *to* your machines; agents connect *out* to the hub. You only expose the
hub.

---

## 1. Build the binaries

Requires **Go 1.25+** (set `GOTOOLCHAIN=auto` if your local Go is older) and **Node 18+ / npm**
(to build the embedded web app).

```bash
make build          # builds the web app, then ./bin/constellate-hub and ./bin/constellate-agent
```

Both binaries are static (`CGO_ENABLED=0`) and run on distroless/scratch. Check a build with:

```bash
./bin/constellate-hub version
./bin/constellate-agent version
```

---

## 2. Run the hub

The hub auto-migrates its SQLite database on startup. The simplest local run:

```bash
./bin/constellate-hub serve --config configs/hub.yaml
```

Copy [`configs/hub.example.yaml`](../configs/hub.example.yaml) to `hub.yaml` first and edit it.
Key fields:

| Field | Purpose | Default |
|---|---|---|
| `addr` | Listen address | `127.0.0.1:8080` |
| `public_url` | Externally reachable URL (drives cookie `Secure` flag + WebAuthn) | `http://localhost:8080` |
| `db_path` | SQLite file | `./constellate.db` |
| `enroll_token_ttl` | Lifetime of enrollment tokens (Go duration, e.g. `15m`) | `15m` |
| `tls.cert` / `tls.key` | PEM cert + key for in-app HTTPS (optional) | empty |
| `webauthn.rp_id` / `webauthn.origins` | Passkey settings (derived from `public_url` if unset) | derived |

Any field can be overridden by environment variable: `CONSTELLATE_ADDR`, `CONSTELLATE_DB_PATH`,
`CONSTELLATE_PUBLIC_URL`, `CONSTELLATE_ENROLL_TOKEN_TTL`.

> **TLS:** either set `tls.cert` / `tls.key` for the hub to terminate HTTPS itself, **or** leave
> them empty and run behind a TLS-terminating reverse proxy. A ready-made
> [`deploy/caddy/Caddyfile`](../deploy/caddy/Caddyfile) + [`deploy/compose.yaml`](../deploy/compose.yaml)
> are provided for the Caddy path. The hub must be reached over HTTPS in production — the session
> cookie is `Secure` whenever `public_url` starts with `https`.

---

## 3. Create an operator (one-time bootstrap)

All `/api/*` and `/ws/*` routes are gated behind operator login. Bootstrap the first operator with
TOTP:

```bash
./bin/constellate-hub operator add --config configs/hub.yaml
```

This prints a **TOTP secret** (base32), an **`otpauth://` URI**, and ten single-use **recovery
codes**:

```
TOTP secret: JBSWY3DPEHPK3PXP
Scan this URI in your authenticator app:
otpauth://totp/Constellate:operator?secret=JBSWY3DPEHPK3PXP&issuer=Constellate

Recovery codes (store these safely):
  a1b2c-3d4e5
  ...
```

The hub stores this secret in its database; there is no separate secret to put in config or an env
var. It is the **shared seed** your authenticator and the hub both use to derive the rotating
6-digit code.

#### Load the secret into an authenticator app

Use any standard TOTP app — Google Authenticator, 1Password, Aegis, Authy, Bitwarden, etc. You have
two ways to add it:

1. **Scan a QR code (easiest).** The CLI prints the `otpauth://` URI as *text*, not as an image.
   Turn it into a scannable QR with any local tool, e.g.:

   ```bash
   # paste the printed otpauth:// URI as the argument
   qrencode -t ANSIUTF8 'otpauth://totp/Constellate:operator?secret=...&issuer=Constellate'
   ```

   Then scan it from your phone. (Generate the QR locally — never paste a live TOTP secret into an
   online QR website.)

2. **Type the secret by hand.** In the app choose *add account → enter setup key* and paste the
   **TOTP secret** string. If the app asks for parameters, Constellate uses the standard TOTP
   defaults, so leave them as-is: **time-based**, **SHA-1**, **6 digits**, **30-second** period.

**Store the recovery codes somewhere safe** — each works once and they are the only way back in if
you lose the authenticator.

Optional flags: `--issuer` (default `Constellate`) and `--account` (default `operator`) customize
how the entry is labelled in your authenticator (it shows as `issuer:account`).

> **One operator secret.** `operator add` bootstraps the *first* operator only; re-running it once
> an operator exists fails with `operator already exists`. To start over you must reset the hub's
> auth state (wipe the operator credentials in the DB). Treat the secret + recovery codes as your
> root credentials.

### Log in

Open the hub URL in a browser and log in with the current **6-digit code** from your authenticator
(or a recovery code if you've lost it). Notes on verification:

- The hub accepts the code for the current 30-second window plus one window on either side, so minor
  clock skew is fine — but the **hub's clock must be roughly correct** (run NTP on the VPS) or codes
  will never match.
- Codes are **single-use**: once a code is accepted it can't be replayed, even within its 30-second
  window. If login says the code was already used, wait for the next one.

On success the hub sets the `constellate_session` cookie (HttpOnly, 24 h). Once logged in you can
register a **WebAuthn passkey** for faster, phishing-resistant logins afterward.

---

## 4. Enroll an agent

Agents authenticate with an **Ed25519 keypair**, not a shared secret. Enrollment is a two-step
handshake: mint a short-lived token on the hub, redeem it once on the machine.

**On the hub** — mint a one-time enrollment token (valid for `enroll_token_ttl`):

```bash
./bin/constellate-hub enroll-token --config configs/hub.yaml
# prints a single token string — copy it
```

Add `--ttl 1h` to override the lifetime for this token.

**On the target machine** — redeem the token. This generates a keypair, registers the public key
with the hub, and writes the machine ID + private key locally:

```bash
./bin/constellate-agent enroll \
  --hub https://hub.example.com \
  --token <token-from-hub>
# prints: enrolled: machineID=...
```

- `--hub` is the hub's **HTTP base URL** (not the `ws://…/ws/agent` path). If omitted, it is
  derived from `hub_url` in the agent config.
- The hub stores **only the public key** — it never holds a signing secret.
- The private key is written to `cred_file` (default `~/.constellate/cred`, mode `0600`) and the
  machine ID to `id_file` (default `~/.constellate/agent-id`).

---

## 5. Run the agent

Copy [`configs/agent.example.yaml`](../configs/agent.example.yaml) to `agent.yaml`, then:

```bash
./bin/constellate-agent connect --config configs/agent.yaml
```

Agent config fields:

| Field | Purpose | Default |
|---|---|---|
| `hub_url` | Hub WebSocket dial-home URL | `ws://127.0.0.1:8080/ws/agent` |
| `name` | Display name in the UI | hostname |
| `id_file` | Where the enrolled machine ID lives | `~/.constellate/agent-id` |
| `cred_file` | Where the enrolled private key lives | `~/.constellate/cred` |
| `hub_ca` | PEM CA/cert to verify the hub (empty = system roots) | empty |
| `default_shell` | Shell to spawn per session | `/bin/bash` |
| `scrollback_bytes` | Per-session replay buffer | `262144` (256 KiB) |

Env overrides: `CONSTELLATE_HUB_URL`, `CONSTELLATE_NAME`, `CONSTELLATE_ID_FILE`,
`CONSTELLATE_CRED_FILE`, `CONSTELLATE_HUB_CA`.

> For a production hub over HTTPS, use a `wss://` `hub_url` and point `hub_ca` at the hub's CA if
> it isn't in your system trust store.

The agent reconnects automatically with backoff. To check what an agent thinks its identity is
(without starting it):

```bash
./bin/constellate-agent status --config configs/agent.yaml
```

### Run it as a service

For an always-on machine, run `connect` under a supervisor (systemd, launchd, a container).
A reference container is provided in [`deploy/agent.Dockerfile`](../deploy/agent.Dockerfile) with
[`deploy/agent-entrypoint.sh`](../deploy/agent-entrypoint.sh).

---

## 6. Drive it from the browser

Once the hub is up and at least one agent is enrolled and connected:

1. **Sidebar** — online machines appear grouped by project (sessions without a project land in an
   *Ungrouped* bucket). An **activity badge** shows each session as *active*, *idle*, or
   *needs input* (improve this signal with [shell integration](shell-integration.md)).
2. **New shell** — opens a live terminal on the selected machine. Type, run commands, resize — it's
   a real PTY.
3. **Persistence** — close the tab or switch sessions and come back; the shell is still running and
   its **scrollback repaints instantly** before continuing live. If an agent process restarts, its
   orphaned sessions are marked **`lost`** so the list stays honest.
4. **Split-pane workspace** — split panes horizontally/vertically and bind one session per leaf;
   drag shells between panes to rearrange.
5. **Overview** — toggle to a mission-control grid of color thumbnails of every running terminal;
   **click any tile to dive** into that session.
6. **Dashboard** — fleet totals, per-machine and per-project status rollups (running / exited /
   lost), an **attention list** (lost sessions, offline machines with running sessions), and a
   recent **audit feed**.
7. **Projects** — create projects and rename/assign sessions to organize the fleet.

---

## 7. Managing the fleet

List enrolled machines (ID, name, OS, enrolled/last-seen, revoked status):

```bash
./bin/constellate-hub machines --config configs/hub.yaml
```

Revoke a machine's credential (soft revocation — the agent can no longer dial home):

```bash
./bin/constellate-hub revoke <machine-id> --config configs/hub.yaml
```

Wipe an agent's local identity (forces a fresh enrollment next time):

```bash
./bin/constellate-agent reset --config configs/agent.yaml
```

---

## CLI reference

**Hub** — `constellate-hub <subcommand>` (default `serve`):

| Subcommand | What it does |
|---|---|
| `serve` | Run the hub (auto-migrates, serves the UI + APIs) |
| `migrate` | Apply DB migrations and exit |
| `enroll-token` | Mint a one-time agent enrollment token (`--ttl` to override) |
| `machines` | List enrolled machines |
| `revoke <id>` | Soft-revoke a machine credential |
| `operator add` | Bootstrap an operator (TOTP secret + recovery codes; `--issuer`, `--account`) |
| `version` | Print version, commit, wire protocol version |

**Agent** — `constellate-agent <subcommand>` (default `connect`):

| Subcommand | What it does |
|---|---|
| `connect` | Dial home and serve PTYs (requires prior enrollment) |
| `enroll` | Redeem an enrollment token (`--hub`, `--token`) and store the credential |
| `status` | Print local enrollment identity (no live check) |
| `reset` | Delete the local machine ID + credential |
| `version` | Print version, commit, wire protocol version |

All subcommands accept `--config <path>`; `serve`/`connect` also accept `--log-level`.

---

## Troubleshooting

- **`not enrolled: run constellate-agent enroll …`** — the agent has no credential. Run the
  [enroll step](#4-enroll-an-agent) first.
- **`enroll: server error 4xx`** — the token expired or was already used (tokens are one-time and
  short-lived). Mint a fresh one with `hub enroll-token`.
- **Agent can't verify the hub over HTTPS** — set `hub_ca` to the hub's CA/cert PEM, or install it
  into the machine's system trust store.
- **Browser logs in but routes 401** — confirm the `constellate_session` cookie is set; over HTTPS
  it is `Secure`, so the hub must be reached via `https://` (check `public_url`).
- **WebAuthn passkey unavailable** — `rp_id`/`origins` must match the URL in the browser. They are
  derived from `public_url`; set `webauthn.rp_id` / `webauthn.origins` explicitly if you use a
  non-standard host or multiple origins.
- **Machine shows offline** — the agent process isn't running or can't reach the hub; the hub only
  ever accepts inbound dial-home, so check egress/firewall from the machine to the hub.

---

## See also

- [`shell-integration.md`](shell-integration.md) — opt-in OSC 133 prompt markers for accurate
  activity badges.
- [`DESIGN.md`](../DESIGN.md) — canonical architecture, wire protocol, and milestone history.
- [`configs/`](../configs/) — example hub and agent config files.
- [`deploy/`](../deploy/) — Caddy + Compose topology and reference Dockerfiles.
