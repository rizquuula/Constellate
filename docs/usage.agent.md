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

```
  your machine                          hub (VPS)
  ┌──────────────┐                      ┌──────────────┐
  │ constellate- │ ──WSS (dial-home)──▶ │  hub + web   │ ◀── your browser
  │ agent connect│   stays connected    │     UI       │
  │   owns PTYs  │ ◀──────────────────  │              │
  └──────────────┘   shells, snapshots  └──────────────┘
```

A machine is **online** (and gets a shell button) only while a `connect` process is running and
connected. Kill it and the machine flips **offline**; the button disappears. That's why `connect`
belongs under a service supervisor.

---

## What you need before you start

| You need | Example | Where it comes from |
|---|---|---|
| The `constellate-agent` binary | `./bin/constellate-agent` | `make build`, or copy a prebuilt binary onto the machine |
| The hub's base URL | `https://hub.example.com` or `https://1.2.3.4:44081` | from whoever runs the hub |
| A one-time enrollment token | `ae0fab0226…` | hub operator runs `constellate-hub enroll-token` |
| (sometimes) the hub's CA/cert PEM | `hub-ca.pem` | only if the hub uses a self-signed / private cert |

Check the binary runs:

```bash
./bin/constellate-agent version
```

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
hub_url: "wss://1.2.3.4:44081/ws/agent"

name: ""                 # blank = use this machine's hostname
default_shell: "/bin/bash"

# Only needed if the hub uses a self-signed or private-CA certificate:
# hub_ca: "~/.constellate/hub-ca.pem"
```

> **`hub_url` is a WebSocket URL, not the enroll URL.** You enroll against the hub's *HTTP* base
> (`https://…:44081`), but you connect against its *WebSocket* path (`wss://…:44081/ws/agent`).
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

Every field has an env override: `CONSTELLATE_HUB_URL`, `CONSTELLATE_NAME`, `CONSTELLATE_ID_FILE`,
`CONSTELLATE_CRED_FILE`, `CONSTELLATE_HUB_CA`.

---

## Step 2 — Enroll (one time, registers the machine)

Redeem the one-time token. This generates an **Ed25519 keypair**, registers the **public** key with
the hub, and writes your machine ID + private key locally. The hub never holds a signing secret.

```bash
./bin/constellate-agent enroll \
  --hub https://1.2.3.4:44081 \
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
hub:        wss://1.2.3.4:44081/ws/agent
(live connectivity requires a running agent daemon — not checked here)
```

> If the `hub:` line is **blank**, `hub_url` isn't set in your config — fix Step 1 before
> connecting, or `connect` will exit with `hub_url is required`.

---

## Step 3 — Connect (brings the machine online)

This is the step that makes the shell button appear. `connect` is a **long-running daemon**: it
dials home, heartbeats, and owns the PTYs until you stop it.

```bash
./bin/constellate-agent connect --config ~/.constellate/agent.yaml
```

Leave it running and refresh the hub in your browser — the machine flips **online**, a
CPU/memory line appears under its name, and the **New shell** button shows up. Click it to get a
live terminal.

The agent **reconnects automatically** with backoff if the network blips or the hub restarts. Add
`--log-level debug` to see the dial-home handshake if something's wrong.

> This is fine for a quick test, but the moment you close this terminal the agent stops and the
> machine goes offline. For anything real, run it as a service — next step.

---

## Step 4 — Keep it running (service supervisor)

For an always-on machine, supervise `connect` so it starts at boot and restarts on crash.

### Linux — systemd

Create `/etc/systemd/system/constellate-agent.service` (adjust the user and paths):

```ini
[Unit]
Description=Constellate agent
After=network-online.target
Wants=network-online.target

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
sudo systemctl enable --now constellate-agent
systemctl status constellate-agent          # should be "active (running)"
journalctl -u constellate-agent -f          # follow logs
```

### macOS — launchd

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
   repaints instantly. If the agent restarts, its orphaned sessions are marked **`lost`**.
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

## Troubleshooting

| Symptom | Cause & fix |
|---|---|
| **Machine shows in the list but there's no shell button** | It's enrolled but **offline** — you haven't run `connect`, or `connect` exited. Start it (Step 3) and keep it running (Step 4). |
| **`connect: hub_url is required`** | `hub_url` is missing from `agent.yaml`. The `--hub` flag used for `enroll` does **not** persist to config — set `hub_url` (Step 1). |
| **`not enrolled: run constellate-agent enroll …`** | No credential on this machine. Run [Step 2](#step-2--enroll-one-time-registers-the-machine) first. |
| **`enroll: server error 4xx`** | Token expired or already used (one-time, short-lived). Get a fresh token from the operator. |
| **`connect` can't verify the hub / x509 error** | Hub uses a self-signed or private cert. Set `hub_ca` to the hub's CA/cert PEM, or install it into the machine's system trust store. |
| **Connects then immediately drops** | Often a `ws://` vs `wss://` mismatch, or `hub_url` pointing at the HTTP base instead of `…/ws/agent`. Re-check Step 1. |
| **Machine still offline after `connect`** | Egress blocked. The agent only ever dials *out*; check the machine's firewall/proxy allows outbound to the hub's host:port. |

---

## CLI quick reference

`constellate-agent <subcommand>` (default `connect`). All accept `--config <path>`; `connect` also
accepts `--log-level`.

| Subcommand | What it does |
|---|---|
| `enroll` | Redeem a one-time token (`--hub`, `--token`) and store the credential. One-shot. |
| `connect` | Dial home and serve PTYs. Long-running — this is what makes the machine **online**. |
| `status` | Print local enrollment identity (no live connectivity check). |
| `reset` | Delete the local machine ID + credential. |
| `version` | Print version, commit, wire protocol version. |

---

## See also

- [`usage.binary.md`](usage.binary.md) — full setup including the **hub** side and operator login.
- [`usage.docker.md`](usage.docker.md) — running the agent (and hub) in containers.
- [`shell-integration.md`](shell-integration.md) — opt-in OSC 133 markers for accurate activity badges.
- [`DESIGN.md`](../DESIGN.md) — canonical architecture and wire protocol.
