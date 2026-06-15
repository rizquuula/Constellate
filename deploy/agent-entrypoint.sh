#!/bin/sh
# deploy/agent-entrypoint.sh — enroll-then-connect entrypoint for agent containers.
#
# On first start: obtains an enrollment token, enrolls the agent, then runs
# constellate-agent connect.
# On subsequent starts: cred file already exists → skips enrollment, connects directly.
#
# Environment variables used:
#   CONSTELLATE_HUB_BASE      — HTTP base URL of the hub (e.g. http://hub:8080)
#   CONSTELLATE_HUB_URL       — WebSocket URL for connect (e.g. ws://hub:8080/ws/agent)
#   CONSTELLATE_NAME          — agent display name
#   CONSTELLATE_CRED_FILE     — path to credential file (defaults to ~/.constellate/cred)
#   CONSTELLATE_ENROLL_TOKEN  — enrollment token; if set, used directly (preferred)
#
# Enrollment token resolution (only when no credential exists yet):
#   1. CONSTELLATE_ENROLL_TOKEN env var, if non-empty (preferred); else
#   2. the token file at /run/enroll/token (legacy shared-volume mode), waited for.

set -eu

CRED_FILE="${CONSTELLATE_CRED_FILE:-/root/.constellate/cred}"
TOKEN_FILE="/run/enroll/token"
HUB_BASE="${CONSTELLATE_HUB_BASE:-http://hub:8080}"

if [ ! -f "$CRED_FILE" ]; then
    if [ -n "${CONSTELLATE_ENROLL_TOKEN:-}" ]; then
        TOKEN="$CONSTELLATE_ENROLL_TOKEN"
        echo "[entrypoint] Using enrollment token from CONSTELLATE_ENROLL_TOKEN."
    else
        echo "[entrypoint] No credential found — waiting for enrollment token file..."

        # Wait up to 60s for the token file (legacy shared-volume mode).
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
    fi

    echo "[entrypoint] Enrolling with hub at $HUB_BASE ..."
    constellate-agent enroll --hub "$HUB_BASE" --token "$TOKEN"
    echo "[entrypoint] Enrollment complete."
else
    echo "[entrypoint] Credential found — skipping enrollment."
fi

echo "[entrypoint] Starting constellate-agent connect..."
exec constellate-agent connect
