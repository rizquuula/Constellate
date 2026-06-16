#!/bin/sh
# Constellate agent updater.
#
#   curl -fsSL https://github.com/rizquuula/Constellate/releases/latest/download/update.sh | sudo sh
#
# Downloads and verifies the latest (or pinned) constellate-agent release,
# atomically replaces the running binary, and optionally restarts the systemd
# service.
#
# Environment overrides:
#   CONSTELLATE_VERSION   pin a release tag        (default: latest)
#   CONSTELLATE_BIN       target binary path       (default: command -v constellate-agent || /usr/local/bin/constellate-agent)
#   CONSTELLATE_CHECK     dry-run: report versions and exit 0
#   CONSTELLATE_FORCE     reinstall even if already up to date
#   CONSTELLATE_NO_RESTART  skip systemd restart after update
#
set -eu

REPO="rizquuula/Constellate"
BIN_NAME="constellate-agent"
UNIT="constellate-agent.service"
UNIT_PATH="/etc/systemd/system/${UNIT}"

# ---- pretty output -----------------------------------------------------------
if [ -t 1 ]; then B=$(printf '\033[1m'); G=$(printf '\033[32m'); Y=$(printf '\033[33m'); R=$(printf '\033[31m'); N=$(printf '\033[0m'); else B= G= Y= R= N=; fi
info() { printf '%s==>%s %s\n' "$G" "$N" "$*"; }
warn() { printf '%swarning:%s %s\n' "$Y" "$N" "$*" >&2; }
err()  { printf '%serror:%s %s\n'   "$R" "$N" "$*" >&2; exit 1; }

# ---- prerequisites -----------------------------------------------------------
if command -v curl >/dev/null 2>&1; then
  dl() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  dl() { wget -qO "$2" "$1"; }
else
  err "need curl or wget on PATH"
fi

if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
  err "need sha256sum or shasum on PATH"
fi

command -v tar >/dev/null 2>&1 || err "need tar on PATH"

# ---- resolve target binary path ----------------------------------------------
if [ -n "${CONSTELLATE_BIN:-}" ]; then
  BIN="$CONSTELLATE_BIN"
elif command -v "$BIN_NAME" >/dev/null 2>&1; then
  BIN=$(command -v "$BIN_NAME")
else
  BIN="/usr/local/bin/${BIN_NAME}"
fi

# ---- detect platform ---------------------------------------------------------
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin) ;;
  *) err "unsupported OS: $os (linux and darwin only)" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) err "unsupported architecture: $arch (amd64 and arm64 only)" ;;
esac

# ---- resolve release base URL ------------------------------------------------
if [ -n "${CONSTELLATE_VERSION:-}" ]; then
  base="https://github.com/${REPO}/releases/download/${CONSTELLATE_VERSION}"
  info "Checking ${B}${BIN_NAME}${N} (${CONSTELLATE_VERSION}, ${os}/${arch})"
else
  base="https://github.com/${REPO}/releases/latest/download"
  info "Checking ${B}${BIN_NAME}${N} (latest, ${os}/${arch})"
fi

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t constellate-update)
trap 'rm -rf "$tmp"' EXIT INT TERM

info "Fetching checksums"
dl "${base}/SHA256SUMS" "${tmp}/SHA256SUMS" || err "could not fetch SHA256SUMS — is there a published release?"

line=$(grep -E "  ${BIN_NAME}-.*-${os}-${arch}\.tar\.gz\$" "${tmp}/SHA256SUMS" || true)
[ -n "$line" ] || err "no ${BIN_NAME} asset for ${os}/${arch} in this release"
want_hash=$(printf '%s\n' "$line" | awk '{print $1}')
asset=$(printf '%s\n' "$line" | awk '{print $NF}')

# ---- version up-to-date check ------------------------------------------------
# Extract available semver from the asset filename: constellate-agent-vX.Y.Z-os-arch.tar.gz
available=$(printf '%s\n' "$asset" | sed 's/.*-v\([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\)-.*/\1/' || true)

current=""
if [ -x "$BIN" ]; then
  raw=$("$BIN" version 2>/dev/null || true)
  # Parse: "constellate-agent X.Y.Z (commit ..., proto N)"
  current=$(printf '%s\n' "$raw" | sed 's/constellate-agent \([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\).*/\1/' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' || true)
fi

if [ -n "${CONSTELLATE_CHECK:-}" ]; then
  printf 'current:   %s\n' "${current:-<not installed>}"
  printf 'available: %s\n' "${available:-<unknown>}"
  exit 0
fi

if [ -n "$current" ] && [ "$current" = "$available" ] && [ -z "${CONSTELLATE_FORCE:-}" ]; then
  info "Already up to date (${B}v${current}${N})"
  exit 0
fi

# ---- download + verify -------------------------------------------------------
info "Downloading ${asset}"
dl "${base}/${asset}" "${tmp}/${asset}" || err "download failed: ${asset}"

got_hash=$(sha256 "${tmp}/${asset}")
[ "$got_hash" = "$want_hash" ] || err "checksum mismatch for ${asset}
  expected ${want_hash}
  got      ${got_hash}"
info "Checksum verified"

tar -xzf "${tmp}/${asset}" -C "$tmp" || err "could not extract ${asset}"
[ -f "${tmp}/${BIN_NAME}" ] || err "archive did not contain ${BIN_NAME}"

# ---- atomic swap with rollback -----------------------------------------------
bin_dir=$(dirname "$BIN")

# Write temp files inside the target directory so mv is same-filesystem.
SUDO=
if [ ! -w "$bin_dir" ] && [ "$(id -u)" -ne 0 ]; then
  command -v sudo >/dev/null 2>&1 || err "${bin_dir} is not writable and sudo is unavailable"
  SUDO=sudo
  info "Using sudo to update ${BIN}"
fi

new_tmp="${BIN}.new.$$"
bak="${BIN}.bak"

cleanup_bak() {
  if [ -f "$bak" ]; then
    $SUDO rm -f "$bak" 2>/dev/null || true
  fi
  $SUDO rm -f "$new_tmp" 2>/dev/null || true
}

restore_bak() {
  warn "Update failed; restoring backup"
  if [ -f "$bak" ]; then
    $SUDO mv "$bak" "$BIN" 2>/dev/null || warn "could not restore backup — check ${bak}"
  fi
  $SUDO rm -f "$new_tmp" 2>/dev/null || true
}

$SUDO install -m 0755 "${tmp}/${BIN_NAME}" "$new_tmp" || { restore_bak; err "could not stage new binary"; }

if [ -f "$BIN" ]; then
  $SUDO mv "$BIN" "$bak" || { restore_bak; err "could not back up current binary"; }
fi

if ! $SUDO mv "$new_tmp" "$BIN"; then
  restore_bak
  err "could not move new binary into place"
fi

cleanup_bak
info "Updated ${B}${BIN}${N}"

# ---- restart service (optional) ----------------------------------------------
if [ -z "${CONSTELLATE_NO_RESTART:-}" ]; then
  if [ -f "$UNIT_PATH" ] && systemctl is-active --quiet "$UNIT" 2>/dev/null; then
    info "Restarting ${UNIT}"
    if [ -n "$SUDO" ] || [ "$(id -u)" -eq 0 ]; then
      $SUDO systemctl restart "$UNIT" || warn "systemctl restart ${UNIT} failed"
    else
      systemctl restart "$UNIT" || warn "systemctl restart ${UNIT} failed"
    fi
  else
    printf '\n%sbinary updated%s; restart your agent manually:\n\n' "$B" "$N"
    printf '  systemctl restart %s\n' "$UNIT"
    printf '  # or: constellate-agent connect\n\n'
  fi
fi

info "Done"
"$BIN" version 2>/dev/null || true
