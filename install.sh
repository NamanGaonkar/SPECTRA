#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# SPECTRA installer (Linux / macOS)
#
# Local mode (from a cloned repo):
#   ./install.sh                       # installs the repo-built ./spectra
#
# One-line remote install (no repo, no Go needed):
#   curl -fsSL https://raw.githubusercontent.com/NamanGaonkar/SPECTRA/main/install.sh | bash
#
# Update an existing install to the latest release:
#   curl -fsSL https://raw.githubusercontent.com/NamanGaonkar/SPECTRA/main/install.sh | bash -s -- --update
#
# Flags:
#   --version vX.Y.Z   install a specific release tag (default: latest)
#   --update           alias: reinstall latest over an existing install
# ---------------------------------------------------------------------------
set -euo pipefail

REPO="NamanGaonkar/SPECTRA"
DEST="${SPECTRA_INSTALL_DIR:-${HOME}/.local/bin}"
RAW_URL="https://raw.githubusercontent.com/${REPO}/main/install.sh"
TAG=""

for arg in "$@"; do
  case "$arg" in
    --update) TAG="latest" ;;
    --version) true ;; # value consumed below
    v*) TAG="$arg" ;;
    *) true ;;
  esac
done
# support "--version v1.2.3" two-token form
prev=""
for arg in "$@"; do
  [ "$prev" = "--version" ] && TAG="$arg"
  prev="$arg"
done

say()  { printf '▸ %s\n' "$1"; }
ok()   { printf '✓ %s\n' "$1"; }
die()  { printf '✗ %s\n' "$1" >&2; exit 1; }

# --- mode 1: local repo install -------------------------------------------
if [ -f "./spectra" ] && [ -z "$TAG" ]; then
  say "local install from ./spectra"
  mkdir -p "$DEST"
  install -m 755 ./spectra "$DEST/spectra"
  ok "installed to $DEST/spectra"
  exit 0
fi

# --- mode 2: remote release install ----------------------------------------
# self-fetch when piped (curl | bash), so re-runs work from anywhere
if [ ! -t 0 ] && [ -z "${SPECTRA_NO_SELF_FETCH:-}" ]; then
  SELF=$(mktemp)
  curl -fsSL "$RAW_URL" -o "$SELF" 2>/dev/null || true
fi

say "resolving latest release…"
if [ -z "$TAG" ] || [ "$TAG" = "latest" ]; then
  TAG=$(curl -fsSL -o /dev/null -w '%{url_effective}' \
        "https://github.com/${REPO}/releases/latest" | sed 's|.*/tag/||')
  [ -n "$TAG" ] || die "could not determine the latest release tag"
fi
ok "target: ${TAG}"

# map uname → release artifact name
OS=$(uname -s); ARCH=$(uname -m)
case "$OS" in
  Linux) ART="spectra_${TAG}_linux_${ARCH}" ;;
  Darwin)
    if [ "$ARCH" = "arm64" ]; then
      ART="spectra_${TAG}_macOS_apple-silicon"
    else
      ART="spectra_${TAG}_macOS_intel"
    fi
    ;;
  *) die "unsupported OS: $OS" ;;
esac
BASE="https://github.com/${REPO}/releases/download/${TAG}"

say "downloading ${ART}…"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
curl -fSL --retry 3 -o "$TMP/pkg" "${BASE}/${ART}" \
  || die "download failed — check https://github.com/${REPO}/releases"

# verify against checksums.txt when available
if curl -fsSL -o "$TMP/checksums.txt" "${BASE}/checksums.txt" 2>/dev/null; then
  WANT=$(grep " ${ART}\$" "$TMP/checksums.txt" | awk '{print $1}')
  GOT=$(sha256sum "$TMP/pkg" | awk '{print $1}')
  if [ -n "$WANT" ] && [ "$WANT" != "$GOT" ]; then
    die "checksum mismatch! got $GOT want $WANT"
  fi
  ok "checksum verified"
else
  say "checksums.txt unavailable for this release; skipping verification"
fi

mkdir -p "$DEST"
install -m 755 "$TMP/pkg" "$DEST/spectra"
ok "installed $DEST/spectra (${TAG})"

# PATH guidance
case ":$PATH:" in
  *":$DEST:"*) ;;
  *)
    say "NOTE: $DEST is not on your PATH."
    {
      echo ''
      echo '# added by SPECTRA installer'
      echo "export PATH=\"\$PATH:$DEST\""
    } >> "${HOME}/.bashrc"
    [ -f "${HOME}/.zshrc" ] && echo "export PATH=\"\$PATH:$DEST\"" >> "${HOME}/.zshrc"
    ok "PATH updated — open a new terminal or: source ~/.bashrc"
    ;;
esac

printf '\nNext:  spectra --check     # verify browser + Ollama + model\n       spectra             # launch the TUI\n'
