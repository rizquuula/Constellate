#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

COMPOSE="docker compose -f compose.test.yaml"

trap '$COMPOSE down -v --remove-orphans >/dev/null 2>&1 || true' EXIT

# ── helpers ──────────────────────────────────────────────────────────────────

online_count() {
  curl -s http://127.0.0.1:18080/api/machines | grep -o '"online":true' | wc -l | tr -d ' '
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
  echo "--- last /api/machines response ---"
  curl -s http://127.0.0.1:18080/api/machines || true
  echo ""
  echo "--- container logs ---"
  $COMPOSE logs --tail=40 || true
  exit 1
}

# ── scenario ─────────────────────────────────────────────────────────────────

echo "==> Building images and starting containers..."
$COMPOSE up -d --build

echo "==> Waiting for hub API to respond (up to 60s)..."
elapsed=0
while [ "$elapsed" -lt 60 ]; do
  if curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:18080/api/machines 2>/dev/null | grep -q "^200$"; then
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
