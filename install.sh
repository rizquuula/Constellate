#!/bin/sh
# Constellate agent installer.
#
#   curl -fsSL https://raw.githubusercontent.com/rizquuula/Constellate/main/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/rizquuula/Constellate/main/install.sh | sh -s -- --rootless
#
# Downloads the constellate-agent binary for your OS/arch from the latest
# GitHub Release, verifies its SHA-256 against the release's SHA256SUMS, and
# installs it to /usr/local/bin (override with BIN_DIR=...).
#
# Flags:
#   --rootless          install to ~/.local/bin (no sudo); also set via CONSTELLATE_ROOTLESS=1
#
# Environment overrides:
#   BIN_DIR             install dir            (default: /usr/local/bin, or ~/.local/bin with --rootless)
#   CONSTELLATE_ROOTLESS  set to 1 to enable rootless install (installs to ~/.local/bin, no sudo)
#   CONSTELLATE_VERSION pin a release tag      (default: latest, e.g. v20260615-0830)
#   CONSTELLATE_HUB     hub base URL           (if set with TOKEN, auto-runs enroll)
#   CONSTELLATE_TOKEN   enrollment token       (if set with HUB, auto-runs enroll)
#
set -eu

# ---- parse arguments ---------------------------------------------------------
ROOTLESS="${CONSTELLATE_ROOTLESS:-}"
for _arg in "$@"; do
  case "$_arg" in
    --rootless) ROOTLESS=1 ;;
  esac
done

REPO="rizquuula/Constellate"
BIN="constellate-agent"

# Resolve BIN_DIR: an explicit BIN_DIR from the environment always wins.
# If not set, choose /usr/local/bin (system) or ~/.local/bin (rootless).
_BIN_DIR_EXPLICIT="${BIN_DIR:-}"
if [ -n "$_BIN_DIR_EXPLICIT" ]; then
  BIN_DIR="$_BIN_DIR_EXPLICIT"
elif [ -n "$ROOTLESS" ]; then
  BIN_DIR="${XDG_BIN_HOME:-$HOME/.local/bin}"
else
  BIN_DIR="/usr/local/bin"
fi

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
# Asset names carry the agent's semver (constellate-agent-v0.1.0-...), which is
# independent of the datetime release tag — so we read the exact filename and
# hash out of SHA256SUMS rather than guessing it from a version string.
if [ -n "${CONSTELLATE_VERSION:-}" ]; then
  base="https://github.com/${REPO}/releases/download/${CONSTELLATE_VERSION}"
  info "Installing ${B}${BIN}${N} (${CONSTELLATE_VERSION}, ${os}/${arch})"
else
  base="https://github.com/${REPO}/releases/latest/download"
  info "Installing ${B}${BIN}${N} (latest, ${os}/${arch})"
fi

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t constellate)
trap 'rm -rf "$tmp"' EXIT INT TERM

info "Fetching checksums"
dl "${base}/SHA256SUMS" "${tmp}/SHA256SUMS" || err "could not fetch SHA256SUMS — is there a published release?"

line=$(grep -E "  ${BIN}-.*-${os}-${arch}\.tar\.gz\$" "${tmp}/SHA256SUMS" || true)
[ -n "$line" ] || err "no ${BIN} asset for ${os}/${arch} in this release"
want_hash=$(printf '%s\n' "$line" | awk '{print $1}')
asset=$(printf '%s\n' "$line" | awk '{print $NF}')

info "Downloading ${asset}"
dl "${base}/${asset}" "${tmp}/${asset}" || err "download failed: ${asset}"

got_hash=$(sha256 "${tmp}/${asset}")
[ "$got_hash" = "$want_hash" ] || err "checksum mismatch for ${asset}
  expected ${want_hash}
  got      ${got_hash}"
info "Checksum verified"

tar -xzf "${tmp}/${asset}" -C "$tmp" || err "could not extract ${asset}"
[ -f "${tmp}/${BIN}" ] || err "archive did not contain ${BIN}"

# ---- install -----------------------------------------------------------------
# Create the target dir up front, unprivileged, so the writability check below
# reflects a directory that actually exists. This is what lets a first-time
# --rootless install into a not-yet-existing ~/.local/bin work without sudo:
# $HOME is writable, so we can create it ourselves. For a non-writable system
# path the attempt fails harmlessly and we fall through to the sudo branch.
if [ ! -d "$BIN_DIR" ] && [ "$(id -u)" -ne 0 ]; then
  mkdir -p "$BIN_DIR" 2>/dev/null || true
fi

SUDO=
if [ ! -w "$BIN_DIR" ] && [ "$(id -u)" -ne 0 ]; then
  command -v sudo >/dev/null 2>&1 || err "${BIN_DIR} is not writable and sudo is unavailable — set BIN_DIR to a writable path"
  SUDO=sudo
  info "Using sudo to install into ${BIN_DIR}"
fi

$SUDO mkdir -p "$BIN_DIR"
$SUDO install -m 0755 "${tmp}/${BIN}" "${BIN_DIR}/${BIN}"
dest="${BIN_DIR}/${BIN}"
info "Installed ${B}${dest}${N}"
"$dest" version 2>/dev/null || true

# Warn if the install dir isn't on PATH.
case ":${PATH}:" in
  *":${BIN_DIR}:"*) ;;
  *) warn "${BIN_DIR} is not on your PATH — add it, e.g.  export PATH=\"${BIN_DIR}:\$PATH\"" ;;
esac

# ---- next steps (optionally auto-enroll) -------------------------------------
printf '\n'
if [ -n "${CONSTELLATE_HUB:-}" ] && [ -n "${CONSTELLATE_TOKEN:-}" ]; then
  info "Enrolling against ${CONSTELLATE_HUB}"
  "$dest" enroll --hub "$CONSTELLATE_HUB" --token "$CONSTELLATE_TOKEN"
  printf '\n%sNext:%s start the agent\n\n  %s connect\n\n' "$B" "$N" "$BIN"
else
  printf '%sNext steps%s\n\n' "$B" "$N"
  printf '  1. Enroll this machine against your hub (get a token from:  hub enroll-token):\n\n'
  printf '       %s enroll --hub https://your-hub.example --token <ENROLLMENT_TOKEN>\n\n' "$BIN"
  printf '  2. Connect (dial home and serve PTYs):\n\n'
  printf '       %s connect\n\n' "$BIN"
  printf '  Tip: pass CONSTELLATE_HUB and CONSTELLATE_TOKEN to this installer to enroll in one step.\n\n'
fi
