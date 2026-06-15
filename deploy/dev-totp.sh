#!/usr/bin/env bash
# deploy/dev-totp.sh — print a current TOTP login code for the dev operator.
#
# Reads the operator's TOTP secret saved by deploy/dev-up.sh and prints the
# 6-digit code valid for the current 30-second window. Use it to log in to the
# dev UI without an authenticator app.
#
#   ./deploy/dev-totp.sh          # prints e.g. 482913
#
set -euo pipefail
cd "$(dirname "$0")/.."

SECRET_FILE="deploy/.dev/totp-secret"

if [ ! -f "$SECRET_FILE" ]; then
  echo "No saved TOTP secret at $SECRET_FILE." >&2
  echo "Run ./deploy/dev-up.sh first (it bootstraps the operator and saves the secret)." >&2
  exit 1
fi

SECRET=$(cat "$SECRET_FILE")

# Pure-stdlib RFC 6238 TOTP (SHA1 / 6 digits / 30s / base32 secret) — matches
# the hub's pquerna/otp defaults. No pip packages required.
python3 - "$SECRET" <<'PY'
import base64, hmac, hashlib, struct, sys, time

secret = sys.argv[1].strip().replace(" ", "").upper()
pad = "=" * ((8 - len(secret) % 8) % 8)
key = base64.b32decode(secret + pad)
counter = int(time.time()) // 30
digest = hmac.new(key, struct.pack(">Q", counter), hashlib.sha1).digest()
offset = digest[-1] & 0x0F
code = (struct.unpack(">I", digest[offset:offset + 4])[0] & 0x7FFFFFFF) % 1_000_000
print(f"{code:06d}")
PY
