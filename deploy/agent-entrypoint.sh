#!/bin/sh
# deploy/agent-entrypoint.sh — enroll-then-connect entrypoint for agent containers.
#
# On first start: waits for the enrollment token file, enrolls the agent, then
# runs constellate-agent connect.
# On subsequent starts: cred file already exists → skips enrollment, connects directly.
#
# Environment variables used:
#   CONSTELLATE_HUB_BASE   — HTTP base URL of the hub (e.g. http://hub:8080)
#   CONSTELLATE_HUB_URL    — WebSocket URL for connect (e.g. ws://hub:8080/ws/agent)
#   CONSTELLATE_NAME       — agent display name
#   CONSTELLATE_CRED_FILE  — path to credential file (defaults to ~/.constellate/cred)
#
# The enrollment token is read from /run/enroll/token (shared volume written by hub-init).

set -eu

CRED_FILE="${CONSTELLATE_CRED_FILE:-/root/.constellate/cred}"
TOKEN_FILE="/run/enroll/token"
HUB_BASE="${CONSTELLATE_HUB_BASE:-http://hub:8080}"

if [ ! -f "$CRED_FILE" ]; then
    echo "[entrypoint] No credential found — waiting for enrollment token..."

    # Wait up to 60s for the hub-init container to write the token.
    waited=0
    while [ ! -f "$TOKEN_FILE" ]; do
        if [ "$waited" -ge 60 ]; then
            echo "[entrypoint] ERROR: enrollment token never appeared at $TOKEN_FILE" >&2
            exit 1
        fi
        sleep 1
        waited=$((waited + 1))
    done

    TOKEN=$(cat "$TOKEN_FILE")
    echo "[entrypoint] Enrolling with hub at $HUB_BASE ..."
    constellate-agent enroll --hub "$HUB_BASE" --token "$TOKEN"
    echo "[entrypoint] Enrollment complete."
else
    echo "[entrypoint] Credential found — skipping enrollment."
fi

echo "[entrypoint] Starting constellate-agent connect..."
exec constellate-agent connect
