#!/usr/bin/env bash
# deploy/dev-up.sh — one-command local dev stack for Constellate.
#
# Builds the images (picking up local changes), starts the hub + two agents,
# bootstraps the operator account (TOTP), enrolls both agents, and prints how
# to log in. Safe to re-run: an existing operator and already-enrolled agents
# are reused.
#
#   ./deploy/dev-up.sh
#   open http://localhost:8080   → log in → pick an agent → "New shell"
#
set -euo pipefail
cd "$(dirname "$0")/.."

COMPOSE="docker compose -f docker-compose.dev.yaml"
HUB_BIN="/usr/local/bin/constellate-hub"
BASE="http://localhost:8080"
DEV_DIR="deploy/.dev"
SECRET_FILE="$DEV_DIR/totp-secret"
mkdir -p "$DEV_DIR"

# ── Build + start the hub ──────────────────────────────────────────────────────
echo "==> Building and starting the hub (this rebuilds with your latest changes)..."
$COMPOSE up -d --build hub

echo "==> Waiting for the hub to be ready..."
elapsed=0
until [ "$elapsed" -ge 90 ]; do
  if curl -s -o /dev/null -w "%{http_code}" "$BASE/api/auth/status" 2>/dev/null | grep -q "^200$"; then
    echo "  ok: hub is up at $BASE"
    break
  fi
  sleep 1
  elapsed=$((elapsed + 1))
done
if [ "$elapsed" -ge 90 ]; then
  echo "TIMEOUT: hub never became ready" >&2
  $COMPOSE logs --tail=40 hub || true
  exit 1
fi

# ── Bootstrap the operator account (idempotent) ────────────────────────────────
STATUS=$(curl -s "$BASE/api/auth/status" || echo "")
if echo "$STATUS" | grep -q '"hasOperator":true'; then
  echo "==> Operator already configured — reusing it."
  if [ ! -f "$SECRET_FILE" ]; then
    echo "  NOTE: no saved TOTP secret at $SECRET_FILE. Use your authenticator app"
    echo "        or recovery codes from when the operator was first created, or run"
    echo "        '$COMPOSE down -v' to reset and re-bootstrap a fresh operator."
  fi
else
  echo "==> Creating the operator account..."
  OP_OUT=$($COMPOSE exec -T hub "$HUB_BIN" operator add 2>&1)
  echo "$OP_OUT"
  SECRET=$(echo "$OP_OUT" | grep '^TOTP secret:' | sed 's/TOTP secret: //' | tr -d '\r')
  if [ -z "$SECRET" ]; then
    echo "ERROR: could not parse TOTP secret from 'operator add' output" >&2
    exit 1
  fi
  printf '%s' "$SECRET" > "$SECRET_FILE"
  echo "  TOTP secret saved to $SECRET_FILE (gitignored) for ./deploy/dev-totp.sh"
fi

# ── Mint enrollment tokens + start the agents ──────────────────────────────────
# Tokens are single-use; mint one per agent against the hub's own DB. Agents that
# already hold credentials (persisted volume) ignore the token and just connect.
echo "==> Minting enrollment tokens and starting agents..."
AGENT_ALPHA_TOKEN=$($COMPOSE exec -T hub "$HUB_BIN" enroll-token --ttl=24h | tr -d '\r\n')
AGENT_BETA_TOKEN=$($COMPOSE exec -T hub "$HUB_BIN" enroll-token --ttl=24h | tr -d '\r\n')
export AGENT_ALPHA_TOKEN AGENT_BETA_TOKEN
$COMPOSE up -d --build agent-alpha agent-beta

echo "==> Waiting for agents to come online (up to 30s)..."
elapsed=0
until [ "$elapsed" -ge 30 ]; do
  online=$($COMPOSE logs --no-color hub 2>/dev/null | grep -c 'agent online' || true)
  if [ "${online:-0}" -ge 2 ]; then
    echo "  ok: $online agent connection(s) seen"
    break
  fi
  sleep 1
  elapsed=$((elapsed + 1))
done

# ── Summary ────────────────────────────────────────────────────────────────────
echo ""
echo "─────────────────────────────────────────────────────────────────────"
echo " Constellate dev stack is up:  $BASE"
echo ""
if [ -f "$SECRET_FILE" ]; then
  CODE=$(./deploy/dev-totp.sh 2>/dev/null || echo "??????")
  echo " Log in with this 6-digit code (valid ~30s):   $CODE"
  echo " Need a fresh code later:                       ./deploy/dev-totp.sh"
else
  echo " Log in using your authenticator app or recovery codes."
fi
echo ""
echo " Then: pick agent-alpha or agent-beta → \"New shell\" → type away."
echo ""
echo " Stop:        docker compose -f docker-compose.dev.yaml down"
echo " Reset all:   docker compose -f docker-compose.dev.yaml down -v"
echo "─────────────────────────────────────────────────────────────────────"
