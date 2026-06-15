# Using Constellate — with Docker

This is the guide for running Constellate **in containers**. Two compose stacks ship with the repo:

| Stack | File | Make targets | TLS | For |
|---|---|---|---|---|
| **Dev** | [`docker-compose.dev.yaml`](../docker-compose.dev.yaml) | `ddocker-*` | none (plain `http://localhost`) | trying it locally / demo |
| **Prod** | [`deploy/compose.yaml`](../deploy/compose.yaml) | `docker-*` | Caddy auto-HTTPS | a public VPS |

Run `make` (or `make help`) to see every target. For running from the binaries instead of
containers, see [`usage.binary.md`](usage.binary.md); for architecture, see
[`DESIGN.md`](../DESIGN.md).

> **What runs where.** In the dev stack the two agents run *inside containers*, so the shells you
> open are shells in those Debian containers — **not your host**. To get a terminal on your actual
> machine, run the agent as a host binary ([`usage.binary.md`](usage.binary.md)) and point it at the
> containerized hub.

---

## Dev stack — try it locally

One command builds the images, starts the hub + two agent "machines", bootstraps an operator
account, enrolls both agents, and prints a ready-to-use login code:

```bash
make ddocker-up            # wraps ./deploy/dev-up.sh
```

Then:

```bash
open http://localhost:8080         # log in → pick an agent → "New shell"
make ddocker-totp                  # print a fresh 6-digit TOTP code anytime
```

The hub binds to `127.0.0.1:8080` with plain `http://` (no `CONSTELLATE_PUBLIC_URL`, so the session
cookie is not marked `Secure` and login works without TLS). It is **not** for the public internet.

Lifecycle:

```bash
make ddocker-logs          # follow the hub logs
make ddocker-down          # stop, keep volumes (operator + machine IDs survive)
make ddocker-reset         # stop and wipe volumes (fresh operator on next up)
```

`ddocker-up` is safe to re-run — an existing operator and already-enrolled agents are reused.

---

## Prod stack — public VPS with a domain

The prod stack puts **Caddy** in front of the hub: Caddy owns ports 80/443, terminates TLS with
certificates it obtains and renews automatically, and reverse-proxies plain HTTP to the hub on the
internal Docker network (the hub is never published directly). Topology:

```
browser ──HTTPS/WSS──▶ Caddy :443 ──HTTP──▶ hub:8080 (internal only)
agents  ──WSS────────▶ wss://$CONSTELLATE_DOMAIN/ws/agent  (through Caddy)
```

**Prerequisites:**

1. `CONSTELLATE_DOMAIN` is exported. Compose substitutes it into the hub's `public_url`
   (`https://$CONSTELLATE_DOMAIN`) and into Caddy's site address.
2. **DNS for that domain already points at this host.** Caddy gets its cert from Let's Encrypt via
   an ACME challenge on ports 80/443, which only succeeds if public DNS resolves to this server
   *before* you start the stack.

```bash
export CONSTELLATE_DOMAIN=example.com
make docker-up             # docker compose -f deploy/compose.yaml up -d
```

Then bootstrap auth and enroll agents (the hub binary lives inside the `hub` container):

```bash
# 1. Create the first operator (prints TOTP secret + recovery codes)
docker compose -f deploy/compose.yaml exec hub constellate-hub operator add

# 2. Mint a one-time enrollment token per agent machine
docker compose -f deploy/compose.yaml exec hub constellate-hub enroll-token
```

Copy each token to the target machine and enroll the agent there (host binary or the
[`deploy/agent.Dockerfile`](../deploy/agent.Dockerfile) container):

```bash
constellate-agent enroll --hub https://example.com --token <token>
constellate-agent connect
```

Lifecycle:

```bash
make docker-logs           # follow hub + caddy logs
make docker-down           # stop the prod stack
```

---

## Prod stack on a bare IP (no domain)

You can run the prod stack reachable only by IP — say `1.2.3.4` — but TLS works differently because
Let's Encrypt won't issue a publicly-trusted cert for a bare IP, and the agent verifies the hub's
cert strictly (it pins a CA via `hub_ca`, with **no** "insecure / skip-verify" escape hatch). So the
cert must carry an **IP SAN** for `1.2.3.4`, and the agent must dial `wss://1.2.3.4`. The clean way
is to switch Caddy from public certs to its **internal CA** and trust that CA on the agents.

### 1. Point the stack at the IP

```bash
export CONSTELLATE_DOMAIN=1.2.3.4
make docker-up
```

Caddy detects that the site address is an IP, **skips Let's Encrypt**, and issues a cert from its
built-in CA with an IP SAN for `1.2.3.4`. No Caddyfile edit is needed — `{$CONSTELLATE_DOMAIN}` just
expands to the IP. The hub's `public_url` becomes `https://1.2.3.4`.

### 2. Export Caddy's internal root and trust it on each agent

```bash
docker compose -f deploy/compose.yaml exec caddy \
  cat /data/caddy/pki/authorities/local/root.crt > hub-root.crt
```

Copy `hub-root.crt` to each agent machine.

### 3. Enroll the agent against the IP, pinning that root

```bash
constellate-agent enroll --hub https://1.2.3.4 --token <token>
```

and in the agent config:

```yaml
hub_url: wss://1.2.3.4
hub_ca:  /path/to/hub-root.crt
```

Verification now passes: the presented cert has IP SAN `1.2.3.4` and `hub_ca` trusts its issuer.

### Caveats with a bare IP

- **Browser trust.** The cert is from a private CA, so browsers warn. Import `hub-root.crt` into your
  OS/browser trust store, or click through the warning.
- **Passkeys won't work.** WebAuthn requires the RP ID to be a registrable domain (or `localhost`) —
  a bare IP is rejected by browsers, so passkey *registration* fails over `1.2.3.4`. You can still
  log in with **TOTP + recovery codes**; only the WebAuthn factor is unavailable until a real
  hostname fronts the hub.

> **Alternative — skip Caddy entirely.** The hub can terminate TLS itself via `tls.cert` / `tls.key`.
> Generate a self-signed cert with an IP SAN, mount it into the hub, publish the hub on `:443`, and
> hand the same cert to agents as `hub_ca`. More manual cert handling, but no reverse proxy.

---

## Building the hub image on its own

To build just the hub image (e.g. to push to a registry) without bringing up a stack:

```bash
make image-hub             # docker build -f deploy/hub.Dockerfile -t constellate-hub:<version> .
```

---

## See also

- [`usage.binary.md`](usage.binary.md) — running the hub and agents from the prebuilt binaries,
  full config reference, fleet management, and troubleshooting.
- [`DESIGN.md`](../DESIGN.md) — canonical architecture, wire protocol, and milestone history.
- [`deploy/`](../deploy/) — the Caddyfile, Compose stack, entrypoints, and reference Dockerfiles.
