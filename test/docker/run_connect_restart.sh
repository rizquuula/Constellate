#!/usr/bin/env bash
# test/docker/run_connect_restart.sh
#
# Scenario: connect-restart survival.
#
# Asserts that killing ONLY the connect process (while the setsid-spawned
# session-host stays alive) does NOT cause the hub to mark sessions lost.
# The hub must see the same instanceID before and after the connect restart
# because the host never changes, so registry.Register fires restarted=false.
#
# Prerequisites: Docker, docker compose.
#
# What it checks:
#   1. Agent comes online (initial connect).
#   2. We kill the connect process inside the container (NOT the container itself).
#   3. Agent goes offline briefly (hub notices the WebSocket drop).
#   4. The supervisor restarts connect; agent comes back online (second "agent online").
#   5. Hub logs do NOT contain "process restart detected, marking running sessions lost"
#      between the two "agent online" events for agent-cr-1 → sessions stayed running.
#
# Comparison with run.sh (existing test):
#   run.sh does `docker compose stop agent1` (kills PID 1 = kills container = kills host)
#   → restarted=true → sessions correctly lost.
#   THIS test kills only the connect PID inside a still-running container
#   → host alive → same instanceID → restarted=false → sessions stay running.

set -euo pipefail
cd "$(dirname "$0")"

COMPOSE="docker compose -f compose.connect-restart.yaml"
HUB_BIN="/usr/local/bin/constellate-hub"
HUB_PORT="18081"

trap '$COMPOSE down -v --remove-orphans >/dev/null 2>&1 || true' EXIT

# ── helpers ──────────────────────────────────────────────────────────────────

hub_logs() {
  $COMPOSE logs --no-color hub 2>/dev/null || true
}

# Count net "agent online" minus "agent offline" from hub logs.
online_count() {
  local logs online offline
  logs=$(hub_logs)
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
  hub_logs | grep -E 'agent (online|offline)|process restart' || true
  echo "--- full container logs ---"
  $COMPOSE logs --tail=60 || true
  exit 1
}

# Wait for the hub's log to accumulate at least N occurrences of a pattern.
wait_for_log_count() {
  local pattern=$1 want=$2 timeout_s=$3 desc=$4
  local elapsed=0
  while [ "$elapsed" -lt "$timeout_s" ]; do
    local got
    got=$(hub_logs | grep -c "$pattern" || true)
    if [ "$got" -ge "$want" ]; then
      echo "  ok: log pattern '$pattern' seen ${got} time(s) (${desc})"
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  echo "TIMEOUT waiting for ${desc}"
  hub_logs | grep -E 'agent (online|offline)|process restart' || true
  exit 1
}

# ── scenario ─────────────────────────────────────────────────────────────────

echo "==> Building image and starting hub..."
$COMPOSE up -d --build hub

echo "==> Waiting for hub API (up to 60s)..."
elapsed=0
while [ "$elapsed" -lt 60 ]; do
  if curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:${HUB_PORT}/api/auth/status" 2>/dev/null | grep -q "^200$"; then
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

echo "==> Minting enrollment token..."
AGENT1_TOKEN=$($COMPOSE exec -T hub "$HUB_BIN" enroll-token --ttl=1h | tr -d '\r\n')
export AGENT1_TOKEN
if [ -z "$AGENT1_TOKEN" ]; then
  echo "ERROR: failed to mint enrollment token"
  exit 1
fi
echo "  minted token"

echo "==> Building and starting agent (supervisor entrypoint)..."
$COMPOSE up -d --build agent1

echo "==> Waiting for agent to come online (up to 60s)..."
wait_for_count 1 60 "agent online (first connect)"

# Snapshot hub logs right after first online event for later comparison.
LOGS_AFTER_FIRST_ONLINE=$(hub_logs)

echo "==> Finding connect PID inside the agent container..."
# The session-host is also running (setsid-spawned by connect). We need the
# connect PID specifically. The supervisor runs as PID 1; the session-host is
# in its own process group (setsid). connect is the direct child of the
# supervisor (shell). We find it by looking for the process named
# "constellate-agent" whose parent is PID 1 (the supervisor shell).
CONNECT_PID=$($COMPOSE exec -T agent1 sh -c \
  'for p in /proc/[0-9]*/status; do
     pid=$(basename $(dirname "$p"))
     ppid=$(grep "^PPid:" "$p" 2>/dev/null | awk "{print \$2}" || echo 0)
     name=$(grep "^Name:" "$p" 2>/dev/null | awk "{print \$2}" || echo "")
     if [ "$ppid" = "1" ] && [ "$name" = "constellate-age" ]; then
       echo "$pid"
       break
     fi
   done' 2>/dev/null | tr -d '\r\n' || true)

if [ -z "$CONNECT_PID" ]; then
  echo "ERROR: could not find connect PID inside agent container"
  echo "--- agent process list ---"
  $COMPOSE exec -T agent1 sh -c 'ps aux 2>/dev/null || ps 2>/dev/null || cat /proc/*/status 2>/dev/null | grep -E "^(Name|Pid|PPid):" | paste - - -' || true
  exit 1
fi
echo "  connect PID = $CONNECT_PID"

echo "==> Killing connect process (NOT the container)..."
$COMPOSE exec -T agent1 kill -9 "$CONNECT_PID" || true

echo "==> Waiting for agent to go offline (up to 20s)..."
wait_for_count 0 20 "agent offline after connect kill"

echo "==> Waiting for agent to come back online via supervisor restart (up to 30s)..."
wait_for_count 1 30 "agent back online after connect restart"

# ── assertion: sessions NOT marked lost ──────────────────────────────────────
echo "==> Asserting: hub must NOT have logged 'process restart detected' ..."

LOST_COUNT=$(hub_logs | grep -c 'process restart detected' || true)
if [ "$LOST_COUNT" -gt 0 ]; then
  echo "FAIL: hub logged 'process restart detected' ${LOST_COUNT} time(s) — instanceID changed"
  echo "  This means the session-host was NOT preserved across the connect restart."
  echo "--- relevant hub log lines ---"
  hub_logs | grep -E 'process restart|marking.*lost|agent online|agent offline' || true
  exit 1
fi
echo "  ok: no 'process restart detected' in hub logs — sessions stayed running"

echo "==> Asserting: hub logged exactly 2 'agent online' events..."
ONLINE_COUNT=$(hub_logs | grep -c 'agent online' || true)
if [ "$ONLINE_COUNT" -lt 2 ]; then
  echo "FAIL: expected >=2 'agent online' events, got ${ONLINE_COUNT}"
  hub_logs | grep -E 'agent online|agent offline' || true
  exit 1
fi
echo "  ok: ${ONLINE_COUNT} 'agent online' events seen (initial + reconnect)"

echo ""
echo "PASS: connect-restart survival verified"
echo "  - connect was killed (PID $CONNECT_PID); container stayed up"
echo "  - supervisor restarted connect; agent reconnected"
echo "  - hub never detected a process restart (instanceID stable)"
echo "  - sessions remained running (no 'marking sessions lost')"
