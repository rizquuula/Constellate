#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

COMPOSE="docker compose -f compose.test.yaml"
HUB_BIN="/usr/local/bin/constellate-hub"

trap '$COMPOSE down -v --remove-orphans >/dev/null 2>&1 || true' EXIT

# ── helpers ──────────────────────────────────────────────────────────────────
#
# M5 gated /api/* behind the operator session cookie, so we can no longer poll
# /api/machines anonymously. Instead we count the hub's structured connection
# logs: each agent attach logs `agent online`, each drop logs `agent offline`.
# Net (online − offline) is the number of agents currently connected.

online_count() {
  local logs online offline
  logs=$($COMPOSE logs --no-color hub 2>/dev/null || true)
  online=$(printf '%s\n' "$logs" | grep -c 'agent online' || true)
  offline=$(printf '%s\n' "$logs" | grep -c 'agent offline' || true)
  echo $(( online - offline ))
}

wait_for_count() {
  local target=$1 timeout_s=$2 desc=$3
  local elapsed=0
  while [ "$elapsed" -lt "$timeout_s" ]; do
    local count
    count=$(online_count 2>/dev/null || echo 0)
    if [ "$count" = "$target" ]; then
      echo "  ok: online_count=${count} (${desc})"
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  echo "TIMEOUT waiting for ${desc} (wanted ${target}, got $(online_count 2>/dev/null || echo '?'))"
  echo "--- hub connection logs ---"
  $COMPOSE logs --no-color hub 2>/dev/null | grep -E 'agent (online|offline)' || true
  echo "--- container logs ---"
  $COMPOSE logs --tail=40 || true
  exit 1
}

# ── scenario ─────────────────────────────────────────────────────────────────

echo "==> Building image and starting the hub..."
$COMPOSE up -d --build hub

echo "==> Waiting for hub API to respond (up to 60s)..."
elapsed=0
while [ "$elapsed" -lt 60 ]; do
  # /api/auth/status is on the unauthenticated allowlist and always returns 200.
  if curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:18080/api/auth/status 2>/dev/null | grep -q "^200$"; then
    echo "  hub API is up"
    break
  fi
  sleep 1
  elapsed=$((elapsed + 1))
done
if [ "$elapsed" -ge 60 ]; then
  echo "TIMEOUT: hub API never responded"
  $COMPOSE logs --tail=40 || true
  exit 1
fi

# Mint one single-use enrollment token per agent, against the hub's own DB
# (exec runs inside the hub container, so it shares the serve process's DB).
echo "==> Minting per-agent enrollment tokens..."
AGENT1_TOKEN=$($COMPOSE exec -T hub "$HUB_BIN" enroll-token --ttl=1h | tr -d '\r\n')
AGENT2_TOKEN=$($COMPOSE exec -T hub "$HUB_BIN" enroll-token --ttl=1h | tr -d '\r\n')
export AGENT1_TOKEN AGENT2_TOKEN
if [ -z "$AGENT1_TOKEN" ] || [ -z "$AGENT2_TOKEN" ]; then
  echo "ERROR: failed to mint enrollment tokens"
  exit 1
fi
echo "  minted 2 tokens"

echo "==> Building and starting both agents..."
$COMPOSE up -d --build agent1 agent2

echo "==> Waiting for both agents to come online (up to 60s)..."
wait_for_count 2 60 "both agents online"

echo "==> Stopping agent1..."
$COMPOSE stop agent1

echo "==> Waiting for agent1 to go offline (up to 30s)..."
wait_for_count 1 30 "agent1 offline after container stop"

echo "==> Restarting agent1..."
$COMPOSE start agent1

echo "==> Waiting for agent1 to reconnect (up to 30s)..."
wait_for_count 2 30 "agent1 back online after restart"

echo ""
echo "PASS: dial-home topology verified across containers"
