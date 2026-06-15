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

`ddocker-up` already bootstrapped the operator and registered its TOTP secret, so in the dev stack
you don't run `operator add` yourself — `ddocker-totp` just derives the current 6-digit code from
that secret for you. For how the secret works and how to load it into a real authenticator app
(prod), see [Set up the TOTP authenticator secret](#set-up-the-totp-authenticator-secret) below.

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

> **Custom host ports.** Caddy publishes host `80`/`443` by default; override with `CADDY_HTTP` /
> `CADDY_HTTPS` (the *container* ports stay 80/443). The app is served over **HTTPS only** — the HTTP
> port does nothing but redirect. **Caveat:** Caddy's auto HTTP→HTTPS redirect targets the standard
> port `443` (it can't see your host remap), so when `CADDY_HTTPS` is non-standard the redirect lands
> on the wrong port. Just open the HTTPS URL with its port directly, e.g.
> `CADDY_HTTPS=44081 → https://$CONSTELLATE_DOMAIN:44081`.

Then bootstrap auth and enroll agents (the hub binary lives inside the `hub` container):

```bash
# 1. Create the first operator (prints TOTP secret + recovery codes)
docker compose -f deploy/compose.yaml exec hub constellate-hub operator add

# 2. Mint a one-time enrollment token per agent machine
docker compose -f deploy/compose.yaml exec hub constellate-hub enroll-token
```

#### Set up the TOTP authenticator secret

`operator add` prints a base32 **TOTP secret**, an `otpauth://` URI, and ten single-use **recovery
codes**:

```
TOTP secret: JBSWY3DPEHPK3PXP
Scan this URI in your authenticator app:
otpauth://totp/Constellate:operator?secret=JBSWY3DPEHPK3PXP&issuer=Constellate

Recovery codes (store these safely):
  a1b2c-3d4e5
  ...
```

The hub keeps this secret in its database (the `hub-data` volume) — there is no secret to put in
compose env or a `.env` file. Load it into any standard TOTP app (Google Authenticator, 1Password,
Aegis, Authy, …) either by **scanning a QR** rendered locally from the `otpauth://` URI
(`qrencode -t ANSIUTF8 '<uri>'`) or by **typing the secret** with the default parameters: time-based,
**SHA-1**, **6 digits**, **30-second** period. Keep the recovery codes safe — they are the only way
back in if you lose the authenticator. `operator add` bootstraps the first operator only; it fails
with `operator already exists` afterward.

Then open `https://$CONSTELLATE_DOMAIN`, log in with the current 6-digit code (or a recovery code),
and optionally register a WebAuthn passkey. The hub accepts the code for the current 30-second
window ±1, so keep the **host clock correct** (NTP); codes are single-use and can't be replayed.
For the full walkthrough and troubleshooting, see
[`usage.binary.md` §3](usage.binary.md#3-create-an-operator-one-time-bootstrap).

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
built-in CA with an IP SAN for `1.2.3.4`. No Caddyfile edit is needed — `{$CONSTELLATE_DOMAIN}`
expands to the IP, and the shipped Caddyfile sets `default_sni {$CONSTELLATE_DOMAIN}` so the cert is
served even when the client sends no SNI. The hub's `public_url` becomes `https://1.2.3.4`.

> **Why `default_sni` matters here.** Browsers and `curl` send no TLS SNI when you connect to a bare
> IP (SNI carries hostnames only). Without a default SNI, Caddy wouldn't know which cert to present
> and the handshake would fail with `tlsv1 alert internal error`. With it, reach the UI at
> **`https://1.2.3.4`** (the IP, *not* `localhost`) and click through the private-CA warning.

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
