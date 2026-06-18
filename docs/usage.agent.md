# Bringing a machine online — the agent guide

This is the **per-machine** guide: how to take one of your machines and make it show up in the hub
with **working, clickable shells**. It assumes someone has already stood up a hub and given you a
hub URL plus a one-time enrollment token. If you're setting up the hub itself, see
[`usage.binary.md`](usage.binary.md) (binaries) or [`usage.docker.md`](usage.docker.md)
(containers).

> **The one thing people miss:** enrolling a machine is *not* the same as connecting it.
> `enroll` registers the machine and exits. The machine then shows in the hub list but has **no
> shell button** — because nothing is dialed home yet. You must also run `connect` (and keep it
> running). This guide walks both steps and then makes `connect` permanent.

---

## How it works (30 seconds)

Your machine **dials out** to the hub over a single WebSocket — there are **no inbound ports** on
your machine. The hub never connects to you.

The agent runs as **two cooperating processes** from the same binary:

```
  your machine
  ┌─────────────────────────────────────────────────────────────┐
  │  constellate-agent session-host  (durable)                  │
  │    owns: instanceID · PTYs · scrollback · vt · snapshots    │
  │    listens on: unix socket (host.sock, 0600)                 │
  │         ▲  local protocol (UDS)                             │
  │  constellate-agent connect  (volatile, restartable)         │
  │    sources: instanceID from session-host                    │
  │    dials home to hub ──WSS──▶   hub + web UI  ◀── browser   │
  └─────────────────────────────────────────────────────────────┘
```

- **`session-host`** is durable: it owns all PTYs, scrollback buffers, and the stable `instanceID`.
  Killing it ends all sessions. It stays alive across `connect` restarts and `agent update` runs.
- **`connect`** is volatile: it dials home, relays hub commands to the session-host over a Unix domain
  socket, and exits cleanly on a signal. The service supervisor (`systemd Restart=always`) restarts it
  automatically — without losing any running sessions.

A machine is **online** (and gets a shell button) only while `connect` is running and connected. If
`connect` exits and is not restarted, the machine flips **offline** and the button disappears. **Running
sessions stay alive** as long as the session-host keeps running, even while connect is down. When
connect reconnects it picks up the same `instanceID` and the hub sees the machine come back online
without marking sessions lost.

> **Sessions are only lost when the session-host dies** (crash, OOM, or machine reboot). That's why
> the session-host unit uses `Restart=on-failure` while the connect unit uses `Restart=always`.

---

## What you need before you start

| You need | Example | Where it comes from |
|---|---|---|
| The `constellate-agent` binary | `./bin/constellate-agent` | `make build`, or copy a prebuilt binary onto the machine |
| The hub's base URL | `https://hub.example.com` or `https://1.2.3.4:443` | from whoever runs the hub |
| A one-time enrollment token | `ae0fab0226…` | hub operator runs `constellate-hub enroll-token` |
| (sometimes) the hub's CA/cert PEM | `hub-ca.pem` | only if the hub uses a self-signed / private cert |

Check the binary runs:

```bash
./bin/constellate-agent version
```

**Installing via script:** the quickest way to get the binary onto a machine is:

```bash
# System-wide install (to /usr/local/bin, requires sudo):
curl -fsSL https://raw.githubusercontent.com/rizquuula/Constellate/main/install.sh | sudo sh

# Rootless install (to ~/.local/bin, no sudo):
curl -fsSL https://raw.githubusercontent.com/rizquuula/Constellate/main/install.sh | sh -s -- --rootless
# or:  CONSTELLATE_ROOTLESS=1 sh install.sh
```

The `--rootless` flag (or `CONSTELLATE_ROOTLESS=1`) installs to `${XDG_BIN_HOME:-~/.local/bin}` without
requiring root. Make sure `~/.local/bin` is on your `$PATH` (`export PATH="$HOME/.local/bin:$PATH"`).

---

## Step 1 — Write the agent config

Copy the example and edit it. **This step is what people skip** — without `hub_url` in the config,
`connect` won't start.

```bash
mkdir -p ~/.constellate
cp configs/agent.example.yaml ~/.constellate/agent.yaml
```

Edit `~/.constellate/agent.yaml`:

```yaml
# Use the WebSocket dial-home path, NOT the plain hub URL.
# https://host:port   ->   wss://host:port/ws/agent
# http://host:port    ->   ws://host:port/ws/agent
hub_url: "wss://1.2.3.4:443/ws/agent"

name: ""                 # blank = use this machine's hostname
default_shell: "/bin/bash"

# Only needed if the hub uses a self-signed or private-CA certificate:
# hub_ca: "~/.constellate/hub-ca.pem"
```

> **`hub_url` is a WebSocket URL, not the enroll URL.** You enroll against the hub's *HTTP* base
> (`https://…:443`), but you connect against its *WebSocket* path (`wss://…:443/ws/agent`).
> Mixing these up is the #1 reason `connect` fails right after a successful enroll.

| Field | Purpose | Default |
|---|---|---|
| `hub_url` | Hub WebSocket dial-home URL (`ws://` or `wss://`, ends in `/ws/agent`) | `ws://127.0.0.1:8080/ws/agent` |
| `name` | Display name in the UI | hostname |
| `id_file` | Where the enrolled machine ID is stored | `~/.constellate/agent-id` |
| `cred_file` | Where the enrolled private key is stored | `~/.constellate/cred` |
| `hub_ca` | PEM CA/cert to verify the hub (empty = system trust store) | empty |
| `default_shell` | Shell spawned per session | `/bin/bash` |
| `scrollback_bytes` | Per-session replay buffer | `262144` (256 KiB) |
| `runtime_dir` | Directory for the session-host Unix socket (`host.sock`). Created with mode `0700`. | `$XDG_RUNTIME_DIR/constellate` if set, else `~/.constellate/run` |

Every field has an env override: `CONSTELLATE_HUB_URL`, `CONSTELLATE_NAME`, `CONSTELLATE_ID_FILE`,
`CONSTELLATE_CRED_FILE`, `CONSTELLATE_HUB_CA`, `CONSTELLATE_RUNTIME_DIR`.

> ### ⚠️ `~` follows the **invoking user** — run everything as the same user
>
> `id_file` and `cred_file` default under `~/.constellate`, and `~` expands from the invoking
> user's `$HOME` (via `os.UserHomeDir()`). Every agent command — `enroll`, `connect`, `status`,
> `update`, `reset` — resolves these paths *at the moment you run it*, so **who** runs the command
> decides which identity it reads or writes.
>
> The classic symptom is two commands disagreeing:
>
> ```console
> $ constellate-agent status          # runs as you → ~/.constellate = /home/you/.constellate
> machine id: 01KV5SH8NRYQYZ9ZFEYETJTP1Q
>
> $ sudo constellate-agent status     # runs as root → ~/.constellate = /root/.constellate
> machine id: 01KW9ABXNRYQYZ9ZFEYETJTP7H   # ← different file, different identity!
> ```
>
> This is **not a bug**. `sudo` resets `$HOME` to `/root` (unless `sudo -E` / a sudoers
> `env_keep += HOME`), so `~/.constellate` points at root's home — an entirely separate
> `agent-id` + `cred` pair. If you ran `enroll` once as your user and once under `sudo`, you
> performed **two enrollments**: the hub minted **two distinct machineIDs** (each enroll generates
> its own Ed25519 keypair), and your one physical box now shows up as **two machines** in the UI.
>
> **Rules of thumb:**
> - Pick one identity and run *every* command as that **same** user.
>   - Rootless / user systemd service (`--rootless`, `systemctl --user`) → **never** prefix `sudo`.
>   - System-wide service running as a named `User=` (or as root) → enroll and check `status` as
>     **that** user (e.g. `sudo -u constellate constellate-agent status …`, or just `sudo …` if it
>     runs as root). Don't mix in a bare-user command.
> - If you already created a stray identity, clean it up: `constellate-hub machines` to list,
>   `constellate-hub revoke <machine-id>` to revoke the unwanted one, and `constellate-agent reset`
>   (as the matching user) to delete its local `id_file` + `cred_file`.
>
> **Make it user-independent.** To stop `$HOME` from mattering, point the files at an absolute,
> shared path so the same identity is used no matter who invokes the agent:
>
> ```yaml
> # in agent.yaml
> id_file:   /etc/constellate/agent-id
> cred_file: /etc/constellate/cred
> ```
>
> ```bash
> # …or via env (overrides config), e.g. in the systemd unit's Environment=
> CONSTELLATE_ID_FILE=/etc/constellate/agent-id \
> CONSTELLATE_CRED_FILE=/etc/constellate/cred \
>   constellate-agent status
> ```
>
> Whoever owns that path must have read/write on it (the dir is created `0700`, the id file `0600`,
> the cred file `0600`), so keep it owned by the single user the agent runs as.

---

## Step 2 — Enroll (one time, registers the machine)

Redeem the one-time token. This generates an **Ed25519 keypair**, registers the **public** key with
the hub, and writes your machine ID + private key locally. The hub never holds a signing secret.

```bash
./bin/constellate-agent enroll \
  --hub https://1.2.3.4:443 \
  --token <one-time-token> \
  --config ~/.constellate/agent.yaml
```

Success looks like:

```
enrolled: machineID=01KV5SH8NRYQYZ9ZFEYETJTP1Q
```

- `--hub` is the hub's **HTTP base URL** (`https://…`), *not* the `wss://…/ws/agent` path. If you
  omit it, it's derived from `hub_url` in the config.
- Writes the private key to `cred_file` (default `~/.constellate/cred`, mode `0600`) and the machine
  ID to `id_file` (default `~/.constellate/agent-id`).
- The token is **single-use and short-lived**. If it expired or was already used, ask the operator
  to mint a fresh one (`constellate-hub enroll-token`).

At this point the machine appears in the hub's list but is **offline with no shell button**. That's
expected — keep going.

You can confirm the local identity any time (this does **not** check connectivity):

```bash
./bin/constellate-agent status --config ~/.constellate/agent.yaml
```

```
enrolled:   yes
machine id: 01KV5SH8NRYQYZ9ZFEYETJTP1Q
name:       debian
hub:        wss://1.2.3.4:443/ws/agent
(live connectivity requires a running agent daemon — not checked here)
```

> If the `hub:` line is **blank**, `hub_url` isn't set in your config — fix Step 1 before
> connecting, or `connect` will exit with `hub_url is required`.

---

## Step 3 — Connect (brings the machine online)

This is the step that makes the shell button appear. Run `connect` — it starts the session-host
automatically if it is not already running, then dials home.

```bash
./bin/constellate-agent connect --config ~/.constellate/agent.yaml
```

On the first run, connect **auto-spawns the session-host** as a detached background process (its
own process group, via `setsid`) under the runtime directory (default: `~/.constellate/run/`). The
socket is at `<runtime_dir>/host.sock`; connect polls it for up to 10 s then proceeds.

Leave it running and refresh the hub in your browser — the machine flips **online**, a
CPU/memory line appears under its name, and the **New shell** button shows up. Click it to get a
live terminal.

The agent **reconnects automatically** with backoff if the network blips or the hub restarts. Add
`--log-level debug` to see the dial-home handshake if something's wrong.

> This is fine for a quick test, but the moment you close this terminal `connect` stops. **The
> session-host process keeps running** (it is detached), so running sessions stay alive — but
> the machine will show offline in the hub until connect restarts. For anything real, run both
> processes as a service — next step.

---

## Step 4 — Keep it running (service supervisor)

For an always-on machine, supervise `connect` so it starts at boot and restarts on crash.

### Linux — systemd (one command)

After enrolling, let the agent install and start its own services:

```bash
sudo constellate-agent install --config /home/rizquuula/.constellate/agent.yaml
```

This requires the machine to be **enrolled** already (it refuses otherwise). It writes **two units**
and starts them in order:

1. `constellate-session-host.service` — durable PTY owner (`Restart=on-failure`). Started first.
2. `constellate-agent.service` — connect relay (`Restart=always`, `Requires=constellate-session-host.service`). Started after.

It runs both services as the user behind `sudo` (`$SUDO_USER`); override with `--user <name>`, or
pass `--no-start` to write the units without starting them. Then:

```bash
systemctl status constellate-session-host   # should be "active (running)"
systemctl status constellate-agent          # should be "active (running)"
journalctl -u constellate-session-host -f   # session-host logs
journalctl -u constellate-agent -f          # connect logs
```

### Linux — systemd (manual)

If you prefer to write the units yourself (this is what `install` generates), create two files.
First, the durable session-host unit at `/etc/systemd/system/constellate-session-host.service`:

```ini
[Unit]
Description=Constellate session host (durable PTY owner)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=rizquuula
ExecStart=/usr/local/bin/constellate-agent session-host --config /home/rizquuula/.constellate/agent.yaml
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
```

Then the connect relay unit at `/etc/systemd/system/constellate-agent.service`:

```ini
[Unit]
Description=Constellate agent (connect relay)
After=network-online.target constellate-session-host.service
Wants=network-online.target
Requires=constellate-session-host.service

[Service]
Type=simple
User=rizquuula
ExecStart=/usr/local/bin/constellate-agent connect --config /home/rizquuula/.constellate/agent.yaml
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now constellate-session-host
sudo systemctl enable --now constellate-agent
systemctl status constellate-session-host   # should be "active (running)"
systemctl status constellate-agent          # should be "active (running)"
```

### Linux — systemd (rootless / user service)

If you don't have root on the machine, install **user-level** systemd services — no sudo anywhere:

```bash
constellate-agent install --rootless --config ~/.constellate/agent.yaml
```

This writes two units under `~/.config/systemd/user/` (`constellate-session-host.service` and
`constellate-agent.service`), runs `systemctl --user daemon-reload`, then enables and starts them in
order. The `--user <name>` flag is ignored in rootless mode.

```bash
systemctl --user status constellate-session-host  # should be "active (running)"
systemctl --user status constellate-agent         # should be "active (running)"
journalctl --user -u constellate-session-host -f  # session-host logs
journalctl --user -u constellate-agent -f         # connect logs
systemctl --user restart constellate-agent        # restart connect only (sessions stay alive)
```

> **Logout caveat:** a `systemd --user` service stops when your login session ends. To keep the agent
> running after you log out, enable [lingering](https://www.freedesktop.org/software/systemd/man/loginctl.html):
>
> ```bash
> loginctl enable-linger <your-username>
> ```
>
> This may require `sudo` or polkit authorisation depending on your system policy. Once lingering is
> enabled, the user service manager starts at boot even without an active login session.

### macOS — launchd

On macOS the session-host is not a separate launchd job; `connect` auto-spawns it in the same
process group when the socket is absent. A full two-plist setup (mirroring the Linux two-unit
model) is future work; for now, supervise `connect` and the session-host follows.

Create `~/Library/LaunchAgents/com.constellate.agent.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>            <string>com.constellate.agent</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/constellate-agent</string>
    <string>connect</string>
    <string>--config</string>
    <string>/Users/you/.constellate/agent.yaml</string>
  </array>
  <key>RunAtLoad</key>        <true/>
  <key>KeepAlive</key>        <true/>
</dict>
</plist>
```

```bash
launchctl load ~/Library/LaunchAgents/com.constellate.agent.plist
```

### Container

A reference image is provided in [`deploy/agent.Dockerfile`](../deploy/agent.Dockerfile) with
[`deploy/agent-entrypoint.sh`](../deploy/agent-entrypoint.sh). See
[`usage.docker.md`](usage.docker.md) for the full container topology.

---

## Step 5 — Use it from the browser

Once the agent is connected:

1. The machine appears in the **sidebar**, online, with a **New shell** button.
2. **New shell** opens a real PTY — type, run commands, resize.
3. **Persistence** — close the tab and come back; the shell is still running and its scrollback
   repaints instantly. The session-host keeps all PTYs alive across browser disconnects and connect
   restarts. Sessions are only marked **`lost`** when the session-host process itself dies (crash or
   machine reboot).
4. Group shells into **projects**, split the **workspace** into panes, watch everything at once in
   the **overview** grid, and see fleet health in the **dashboard**.

For accurate *active / idle / needs-input* badges, enable the opt-in shell hook in
[`shell-integration.md`](shell-integration.md).

---

## Maintenance

Check local identity (no network):

```bash
./bin/constellate-agent status --config ~/.constellate/agent.yaml
```

Wipe this machine's identity to force a fresh enrollment (e.g. after the operator revoked it):

```bash
./bin/constellate-agent reset --config ~/.constellate/agent.yaml
```

If the operator runs `constellate-hub revoke <machine-id>`, this agent can no longer dial home;
re-enroll with a new token to come back.

---

## Updating

`constellate-agent update` downloads the latest release, verifies it, atomically replaces the
binary, and restarts the **connect** systemd unit only (`constellate-agent.service`).

```bash
sudo constellate-agent update
```

The command requires root (or sudo) when the binary lives in a root-owned directory such as
`/usr/local/bin`. After the binary is replaced, `systemctl restart constellate-agent` is called
automatically; pass `--no-restart` to skip this.

**Sessions survive a normal `agent update`** — the session-host unit (`constellate-session-host.service`)
is **not** restarted. Connect exits, the updated binary starts as a new connect process, re-dials the
still-running session-host over the UDS, picks up the same `instanceID`, and reconnects to the hub.
The hub sees the same instanceID and leaves all sessions as `running`.

If you need to update the session-host binary itself (i.e. the host process must be replaced), you
must restart `constellate-session-host.service` explicitly:

```bash
sudo systemctl restart constellate-session-host
```

This **will end all running sessions** — the old host dies (PTYs die), the new host starts with a
fresh `instanceID`, and the hub marks the old sessions `lost`. There is no automatic drain-and-hand-off
for session-host restarts (future work).

For a **rootless install** (binary in `~/.local/bin`, user service in `~/.config/systemd/user/`), pass
`--rootless` — no sudo needed:

```bash
constellate-agent update --rootless
```

After the binary is replaced, the user connect service is restarted via `systemctl --user restart
constellate-agent`. The session-host user service is left running. The direct shell one-liner form
also accepts the flag:

```bash
curl -fsSL https://github.com/rizquuula/Constellate/releases/latest/download/update.sh | sh -s -- --rootless
# or via env:
curl -fsSL https://github.com/rizquuula/Constellate/releases/latest/download/update.sh | CONSTELLATE_ROOTLESS=1 sh
```

### Flags

| Flag | Description |
|---|---|
| `--version <tag>` | Pin a specific release tag (e.g. `v20260615-0830`). Default: latest. |
| `--check` | Report current vs available version and exit without downloading. |
| `--force` | Reinstall even if already at the latest version. |
| `--no-restart` | Skip the systemd service restart after the binary is replaced. |
| `--rootless` | Update a rootless user install; restart via `systemctl --user` (no sudo). |
| `--bin <path>` | Override the target binary path (default: the running binary). |

### How it works

The Go bootstrapper (`cmdUpdate`) fetches `SHA256SUMS` from the release, locates the `update.sh`
entry, downloads `update.sh`, and verifies its checksum before executing anything. Only a
checksum-verified script is ever run — network interception cannot substitute an unverified payload.
The script itself performs the atomic swap: it downloads the tarball, verifies its hash, installs
the new binary via a same-filesystem `mv` (with `.bak` rollback on failure), and optionally
restarts the service.

### Standalone one-liner

You can also run the updater directly without the Go bootstrapper:

```bash
curl -fsSL https://github.com/rizquuula/Constellate/releases/latest/download/update.sh | sudo sh
```

To pin a version:

```bash
curl -fsSL https://github.com/rizquuula/Constellate/releases/download/v20260615-0830/update.sh | sudo sh
```

---

## Troubleshooting

| Symptom | Cause & fix |
|---|---|
| **Machine shows in the list but there's no shell button** | It's enrolled but **offline** — `connect` hasn't started or exited. Start it (Step 3) and keep it running (Step 4). |
| **`connect: hub_url is required`** | `hub_url` is missing from `agent.yaml`. The `--hub` flag used for `enroll` does **not** persist to config — set `hub_url` (Step 1). |
| **`not enrolled: run constellate-agent enroll …`** | No credential on this machine. Run [Step 2](#step-2--enroll-one-time-registers-the-machine) first. |
| **`enroll: server error 4xx`** | Token expired or already used (one-time, short-lived). Get a fresh token from the operator. |
| **`connect: could not start session-host`** | Auto-spawn failed (see session-host log at `<runtime_dir>/host.log`). Check that the runtime dir is writable and the binary is executable. |
| **`connect: dial session-host … connection refused`** | Session-host socket is not ready. Under systemd, check `systemctl status constellate-session-host`. In dev, check the host log. |
| **Sessions marked `lost` after `agent update`** | The session-host unit was restarted (not just connect). Check that only `constellate-agent.service` was restarted. Restarting `constellate-session-host.service` intentionally ends sessions. |
| **`connect` can't verify the hub / x509 error** | Hub uses a self-signed or private cert. Set `hub_ca` to the hub's CA/cert PEM, or install it into the machine's system trust store. |
| **Connects then immediately drops** | Often a `ws://` vs `wss://` mismatch, or `hub_url` pointing at the HTTP base instead of `…/ws/agent`. Re-check Step 1. |
| **Machine still offline after `connect`** | Egress blocked. The agent only ever dials *out*; check the machine's firewall/proxy allows outbound to the hub's host:port. |

---

## CLI quick reference

`constellate-agent <subcommand>` (default `connect`). All accept `--config <path>` / `-c`; `connect`
and `session-host` also accept `--log-level` / `-l`.

| Subcommand | What it does |
|---|---|
| `enroll` | Redeem a one-time token (`--hub` / `-H`, `--token` / `-t`) and store the credential. One-shot. |
| `connect` | Volatile relay: auto-spawns the session-host if needed, then dials home. Long-running. Makes the machine **online**. |
| `session-host` | Durable host: owns PTYs + scrollback + instanceID, listens on UDS. Normally started automatically by `connect` or systemd; rarely run manually. |
| `install` | Write and start both systemd units (`constellate-session-host.service` + `constellate-agent.service`) with correct ordering. Linux only; requires enrollment. |
| `update` | Download + verify latest release, replace binary, restart the **connect** unit only (sessions survive). |
| `status` | Print local enrollment identity (no live connectivity check). |
| `reset` | Delete the local machine ID + credential. |
| `version` | Print version, commit, wire protocol version. |

---

## See also

- [`usage.binary.md`](usage.binary.md) — full setup including the **hub** side and operator login.
- [`usage.docker.md`](usage.docker.md) — running the agent (and hub) in containers.
- [`shell-integration.md`](shell-integration.md) — opt-in OSC 133 markers for accurate activity badges.
- [`DESIGN.md`](../DESIGN.md) — canonical architecture and wire protocol.
