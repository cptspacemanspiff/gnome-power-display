#!/usr/bin/env bash
# Build / install / uninstall the COSMIC Power Monitor applet.
#
# A user-local install touches exactly three files in standard XDG dirs:
#   ~/.local/bin/<bin>
#   ~/.local/share/applications/<appid>.desktop
#   ~/.local/share/metainfo/<appid>.metainfo.xml
# `uninstall` removes precisely those three. Nothing else is scattered around.
# Build artifacts live in ./target (clear with: cargo clean).

set -euo pipefail

APPID="com.github.nlong.CosmicPowerMonitor"
BIN="cosmic-power-monitor"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

BIN_DIR="$HOME/.local/bin"
APP_DIR="$HOME/.local/share/applications"
META_DIR="$HOME/.local/share/metainfo"

BIN_DST="$BIN_DIR/$BIN"
DESKTOP_DST="$APP_DIR/$APPID.desktop"
META_DST="$META_DIR/$APPID.metainfo.xml"

DESKTOP_SRC="resources/$APPID.desktop"
META_SRC="resources/$APPID.metainfo.xml"

usage() {
    cat <<EOF
Usage: ./install.sh <command>

Commands:
  build       cargo build --release
  install     build, then copy the 3 files into ~/.local (no root)
  uninstall   remove the 3 installed files
  list        show what is installed and whether each file exists
  clean       cargo clean (removes ./target build artifacts)

After 'install', add it via COSMIC Settings > Desktop > Panel (or Dock)
> Add applet > Power Monitor. Ensure ~/.local/bin is on your PATH.
EOF
}

build() {
    echo ">> cargo build --release"
    cargo build --release
}

do_install() {
    build
    echo ">> installing to ~/.local"
    install -Dm0755 "target/release/$BIN" "$BIN_DST"
    install -Dm0644 "$DESKTOP_SRC" "$DESKTOP_DST"
    install -Dm0644 "$META_SRC" "$META_DST"
    echo "   $BIN_DST"
    echo "   $DESKTOP_DST"
    echo "   $META_DST"
    if ! command -v "$BIN" >/dev/null 2>&1; then
        echo "!! ~/.local/bin is not on your PATH; add it so the panel can exec the applet:"
        echo "     export PATH=\"\$HOME/.local/bin:\$PATH\""
    fi
    echo ">> done. Add it from COSMIC Settings > Desktop > Panel > Add applet."
}

do_uninstall() {
    echo ">> removing installed files"
    rm -fv "$BIN_DST" "$DESKTOP_DST" "$META_DST"
    echo ">> done. If it was on the panel, also remove it in COSMIC panel settings."
}

do_list() {
    for f in "$BIN_DST" "$DESKTOP_DST" "$META_DST"; do
        if [[ -e "$f" ]]; then echo "  [present] $f"; else echo "  [absent ] $f"; fi
    done
}

case "${1:-}" in
    build) build ;;
    install) do_install ;;
    uninstall) do_uninstall ;;
    list) do_list ;;
    clean) echo ">> cargo clean"; cargo clean ;;
    *) usage; exit 1 ;;
esac
