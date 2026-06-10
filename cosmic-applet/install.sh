#!/usr/bin/env bash
# Dev helper for the COSMIC Power Monitor applet.
#
# This is for local iteration only. The supported install path is the system
# package: from the repo root run `scripts/install-packages.sh install`, which
# builds and installs power-monitor-cosmic (and the daemon it depends on) into
# /usr via RPM/DEB.

set -euo pipefail

BIN="power-monitor-cosmic"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

usage() {
    cat <<EOF
Usage: ./install.sh <command>

Dev commands:
  build       cargo build --release
  restart     restart cosmic-panel so a running applet reloads
  clean       cargo clean (removes ./target build artifacts)

To actually install, use the system package instead:
  (repo root) scripts/install-packages.sh install
EOF
}

build() {
    echo ">> cargo build --release"
    cargo build --release
    echo ">> built target/release/$BIN"
}

# Restart the COSMIC panel so it respawns the applet. cosmic-session relaunches
# cosmic-panel automatically after it exits.
restart_panel() {
    if pgrep -x cosmic-panel >/dev/null 2>&1; then
        echo ">> restarting cosmic-panel to reload the applet"
        pkill -x cosmic-panel || true
    else
        echo ">> cosmic-panel not running; skipping restart"
    fi
}

case "${1:-}" in
    build) build ;;
    restart) restart_panel ;;
    clean) echo ">> cargo clean"; cargo clean ;;
    *) usage; exit 1 ;;
esac
