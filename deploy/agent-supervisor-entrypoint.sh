#!/bin/sh
# deploy/agent-supervisor-entrypoint.sh — supervisor entrypoint for agent containers.
#
# Unlike agent-entrypoint.sh which uses "exec" to make connect PID 1, this
# supervisor keeps the shell as PID 1 so that killing the connect process does
# NOT terminate the container. This enables the connect-restart survival test:
# the session-host (setsid-spawned by connect) stays alive, connect is restarted
# by this supervisor, and the hub sees the same instanceID → sessions stay running.
#
# On SIGTERM (container stop), the supervisor sends SIGTERM to connect and waits.
#
# Environment variables used:
#   CONSTELLATE_HUB_BASE      — HTTP base URL of the hub (e.g. http://hub:8080)
#   CONSTELLATE_HUB_URL       — WebSocket URL for connect (e.g. ws://hub:8080/ws/agent)
#   CONSTELLATE_NAME          — agent display name
#   CONSTELLATE_CRED_FILE     — path to credential file (defaults to ~/.constellate/cred)
#   CONSTELLATE_ENROLL_TOKEN  — enrollment token; if set, used directly (preferred)

set -eu

CRED_FILE="${CONSTELLATE_CRED_FILE:-/root/.constellate/cred}"
HUB_BASE="${CONSTELLATE_HUB_BASE:-http://hub:8080}"

# ── enrollment (only on first start) ────────────────────────────────────────

if [ ! -f "$CRED_FILE" ]; then
    if [ -n "${CONSTELLATE_ENROLL_TOKEN:-}" ]; then
        TOKEN="$CONSTELLATE_ENROLL_TOKEN"
        echo "[supervisor] Using enrollment token from CONSTELLATE_ENROLL_TOKEN."
    else
        echo "[supervisor] ERROR: no credential and no CONSTELLATE_ENROLL_TOKEN" >&2
        exit 1
    fi

    echo "[supervisor] Enrolling with hub at $HUB_BASE ..."
    constellate-agent enroll --hub "$HUB_BASE" --token "$TOKEN"
    echo "[supervisor] Enrollment complete."
else
    echo "[supervisor] Credential found — skipping enrollment."
fi

# ── supervisor loop ──────────────────────────────────────────────────────────
#
# Run constellate-agent connect as a background child (NOT exec). The shell
# stays as PID 1. On SIGTERM we forward it to connect and exit cleanly.

CONNECT_PID=""

term_handler() {
    echo "[supervisor] SIGTERM received, stopping connect..."
    if [ -n "$CONNECT_PID" ]; then
        kill -TERM "$CONNECT_PID" 2>/dev/null || true
        wait "$CONNECT_PID" 2>/dev/null || true
    fi
    exit 0
}

trap term_handler TERM INT

echo "[supervisor] Starting connect supervisor loop..."
while true; do
    constellate-agent connect &
    CONNECT_PID=$!
    echo "[supervisor] connect started (pid=$CONNECT_PID)"

    # Wait for connect to exit. wait is interruptible by the trap above.
    wait "$CONNECT_PID" || true
    EXIT_CODE=$?
    CONNECT_PID=""

    echo "[supervisor] connect exited (code=$EXIT_CODE); restarting in 1s..."
    sleep 1
done
