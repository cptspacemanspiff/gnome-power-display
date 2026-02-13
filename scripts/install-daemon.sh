#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
SVC_SRC="$SCRIPT_DIR/power-monitor-daemon.service"
SVC_DST="$HOME/.config/systemd/user/power-monitor-daemon.service"

case "${1:-}" in
  install)
    echo "Installing power-monitor-daemon user service..."
    mkdir -p "$(dirname "$SVC_DST")"
    cp "$SVC_SRC" "$SVC_DST"
    systemctl --user daemon-reload
    systemctl --user enable --now power-monitor-daemon.service
    echo "Done. Status:"
    systemctl --user status power-monitor-daemon.service --no-pager || true
    ;;
  uninstall)
    echo "Uninstalling power-monitor-daemon user service..."
    systemctl --user disable --now power-monitor-daemon.service 2>/dev/null || true
    rm -f "$SVC_DST"
    systemctl --user daemon-reload
    echo "Done."
    ;;
  status)
    systemctl --user status power-monitor-daemon.service --no-pager
    ;;
  log)
    journalctl --user -u power-monitor-daemon.service -f
    ;;
  *)
    echo "Usage: $0 {install|uninstall|status|log}"
    echo "  install   - Install and start the daemon as a user service"
    echo "  uninstall - Stop and remove the user service"
    echo "  status    - Show service status"
    echo "  log       - Tail daemon logs"
    exit 1
    ;;
esac
