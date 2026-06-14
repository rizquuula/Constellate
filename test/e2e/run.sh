#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

# ── Build ─────────────────────────────────────────────────────────────────────

make build

# ── Temp files ────────────────────────────────────────────────────────────────

TMP_DB=$(mktemp /tmp/constellate-e2e-XXXXXX.db)
TMP_ID=$(mktemp /tmp/constellate-e2e-id-XXXXXX)
HUB_LOG=$(mktemp /tmp/constellate-hub-XXXXXX.log)
AGENT_LOG=$(mktemp /tmp/constellate-agent-XXXXXX.log)
HUB_PID=""
AGENT_PID=""

# ── Cleanup ───────────────────────────────────────────────────────────────────

cleanup() {
  echo "==> Cleaning up..."
  if [ -n "$AGENT_PID" ] && kill -0 "$AGENT_PID" 2>/dev/null; then
    kill "$AGENT_PID" 2>/dev/null || true
  fi
  if [ -n "$HUB_PID" ] && kill -0 "$HUB_PID" 2>/dev/null; then
    kill "$HUB_PID" 2>/dev/null || true
  fi
  rm -f "$TMP_DB" "${TMP_DB}-shm" "${TMP_DB}-wal" "$TMP_ID" "$HUB_LOG" "$AGENT_LOG"
}
trap cleanup EXIT

# ── Start hub ─────────────────────────────────────────────────────────────────

echo "==> Starting hub..."
CONSTELLATE_DB_PATH="$TMP_DB" \
  CONSTELLATE_DEV_TOKEN=e2e \
  ./bin/constellate-hub serve >"$HUB_LOG" 2>&1 &
HUB_PID=$!

# ── Start agent ───────────────────────────────────────────────────────────────

echo "==> Starting agent..."
CONSTELLATE_HUB_URL=ws://127.0.0.1:8080/ws/agent \
  CONSTELLATE_DEV_TOKEN=e2e \
  CONSTELLATE_ID_FILE="$TMP_ID" \
  CONSTELLATE_NAME=e2e-box \
  ./bin/constellate-agent connect >"$AGENT_LOG" 2>&1 &
AGENT_PID=$!

# ── Wait for hub API + agent online ──────────────────────────────────────────

echo "==> Waiting for hub API and agent to come online (up to 30s)..."
WAIT_ELAPSED=0
until [ "$WAIT_ELAPSED" -ge 30 ]; do
  RESPONSE=$(curl -s http://127.0.0.1:8080/api/machines 2>/dev/null || echo "")
  if echo "$RESPONSE" | grep -q '"online":true'; then
    echo "  ok: agent is online"
    break
  fi
  sleep 1
  WAIT_ELAPSED=$((WAIT_ELAPSED + 1))
done

if [ "$WAIT_ELAPSED" -ge 30 ]; then
  echo "TIMEOUT: agent never came online within 30s"
  echo "--- hub log ---"
  cat "$HUB_LOG" || true
  echo "--- agent log ---"
  cat "$AGENT_LOG" || true
  exit 1
fi

# ── Run Playwright tests ──────────────────────────────────────────────────────

echo "==> Installing npm dependencies and running Playwright tests..."
cd test/e2e
npm ci
npx playwright install chromium >/dev/null 2>&1 || npx playwright install --with-deps chromium
npx playwright test
