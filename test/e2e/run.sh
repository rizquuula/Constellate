#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

# ── Build ─────────────────────────────────────────────────────────────────────

make build

# ── Temp files ────────────────────────────────────────────────────────────────

TMP_DB=$(mktemp /tmp/constellate-e2e-XXXXXX.db)
TMP_ID=$(mktemp /tmp/constellate-e2e-id-XXXXXX)
TMP_CRED=$(mktemp /tmp/constellate-e2e-cred-XXXXXX)
HUB_LOG=$(mktemp /tmp/constellate-hub-XXXXXX.log)
AGENT_LOG=$(mktemp /tmp/constellate-agent-XXXXXX.log)
HUB_PID=""
AGENT_PID=""

# Pick a port that is not already in use (avoid conflicts with Docker services).
HUB_PORT=18080
HUB_HOST=127.0.0.1
HUB_BASE="http://${HUB_HOST}:${HUB_PORT}"

# ── Cleanup ───────────────────────────────────────────────────────────────────

cleanup() {
  echo "==> Cleaning up..."
  if [ -n "$AGENT_PID" ] && kill -0 "$AGENT_PID" 2>/dev/null; then
    kill "$AGENT_PID" 2>/dev/null || true
  fi
  if [ -n "$HUB_PID" ] && kill -0 "$HUB_PID" 2>/dev/null; then
    kill "$HUB_PID" 2>/dev/null || true
  fi
  rm -f "$TMP_DB" "${TMP_DB}-shm" "${TMP_DB}-wal" "$TMP_ID" "$TMP_CRED" "$HUB_LOG" "$AGENT_LOG"
}
trap cleanup EXIT

# ── Start hub ─────────────────────────────────────────────────────────────────

echo "==> Starting hub on ${HUB_BASE}..."
CONSTELLATE_DB_PATH="$TMP_DB" \
  CONSTELLATE_ADDR="${HUB_HOST}:${HUB_PORT}" \
  ./bin/constellate-hub serve >"$HUB_LOG" 2>&1 &
HUB_PID=$!

# Wait for hub to be ready.
echo "==> Waiting for hub to be ready..."
WAIT_ELAPSED=0
until [ "$WAIT_ELAPSED" -ge 30 ]; do
  if wget -q -O- "${HUB_BASE}/api/auth/status" >/dev/null 2>&1; then
    echo "  ok: hub is ready"
    break
  fi
  sleep 1
  WAIT_ELAPSED=$((WAIT_ELAPSED + 1))
done
if [ "$WAIT_ELAPSED" -ge 30 ]; then
  echo "TIMEOUT: hub never became ready"
  cat "$HUB_LOG" || true
  exit 1
fi

# ── Bootstrap operator account ────────────────────────────────────────────────
# Mint a TOTP secret for the operator. The secret is printed to stdout by
# `constellate-hub operator add`; we parse it for use in Playwright tests.
# TODO (Playwright test authors): use TOTP_SECRET to compute a login code via
# a library (e.g. @otplib/preset-default) in the test setup. The secret is
# exported as E2E_TOTP_SECRET below so specs can read it from process.env.

echo "==> Adding operator account..."
OPERATOR_OUTPUT=$(CONSTELLATE_DB_PATH="$TMP_DB" ./bin/constellate-hub operator add 2>&1 || true)
echo "$OPERATOR_OUTPUT"
TOTP_SECRET=$(echo "$OPERATOR_OUTPUT" | grep "^TOTP secret:" | sed 's/TOTP secret: //')
export E2E_TOTP_SECRET="$TOTP_SECRET"
echo "  TOTP secret exported as E2E_TOTP_SECRET (use in Playwright for login step)"

# ── Enroll and start agent ────────────────────────────────────────────────────

echo "==> Minting enrollment token..."
ENROLL_TOKEN=$(CONSTELLATE_DB_PATH="$TMP_DB" ./bin/constellate-hub enroll-token --ttl=5m)
echo "  token: $ENROLL_TOKEN"

echo "==> Enrolling agent..."
CONSTELLATE_ID_FILE="$TMP_ID" \
  CONSTELLATE_CRED_FILE="$TMP_CRED" \
  ./bin/constellate-agent enroll \
    --hub "${HUB_BASE}" \
    --token "$ENROLL_TOKEN"
echo "  enrollment complete"

echo "==> Starting agent..."
CONSTELLATE_HUB_URL="ws://${HUB_HOST}:${HUB_PORT}/ws/agent" \
  CONSTELLATE_ID_FILE="$TMP_ID" \
  CONSTELLATE_CRED_FILE="$TMP_CRED" \
  CONSTELLATE_NAME=e2e-box \
  ./bin/constellate-agent connect >"$AGENT_LOG" 2>&1 &
AGENT_PID=$!

# ── Wait for agent online ─────────────────────────────────────────────────────

echo "==> Waiting for agent to come online (up to 30s)..."
WAIT_ELAPSED=0
until [ "$WAIT_ELAPSED" -ge 30 ]; do
  RESPONSE=$(wget -q -O- "${HUB_BASE}/api/machines" 2>/dev/null || echo "")
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
# E2E_TOTP_SECRET is available to Playwright specs via process.env.E2E_TOTP_SECRET.
# TODO (Playwright test authors): add a login step in spec setup using the TOTP
# secret to authenticate before testing operator-gated routes.

echo "==> Installing npm dependencies and running Playwright tests..."
cd test/e2e
npm ci
npx playwright install chromium >/dev/null 2>&1 || npx playwright install --with-deps chromium
BASE_URL="${HUB_BASE}" \
  E2E_TOTP_SECRET="${E2E_TOTP_SECRET}" \
  npx playwright test
