#!/usr/bin/env bash
# SPECTRA installer for Linux/macOS
# Copies the binary to ~/.local/bin and checks PATH.
set -euo pipefail

DEST="${HOME}/.local/bin"
SRC="$(dirname "$0")/spectra"

if [ ! -f "$SRC" ]; then
    echo "[!] spectra binary not found next to install.sh. Build it first:"
    echo "    go build -trimpath -ldflags=\"-s -w\" -o spectra ."
    exit 1
fi

mkdir -p "$DEST"
install -m 755 "$SRC" "$DEST/spectra"

case ":$PATH:" in
    *":$DEST:"*) ;;  # already on PATH
    *)
        SHELL_RC="${HOME}/.bashrc"
        [ -n "${ZSH_VERSION:-}" ] && SHELL_RC="${HOME}/.zshrc"
        {
            echo ""
            echo "# SPECTRA"
            echo "export PATH=\"\$PATH:$DEST\""
        } >> "$SHELL_RC"
        echo "[+] Added $DEST to PATH via $SHELL_RC (open a new shell to apply)."
        ;;
esac

echo ""
echo "[+] SPECTRA installed to $DEST/spectra"
echo "    Run it from any terminal:  spectra"
echo "    Verify the setup first:    spectra --check"
