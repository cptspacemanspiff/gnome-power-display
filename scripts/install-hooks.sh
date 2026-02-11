#!/bin/bash
set -euo pipefail

SLEEP_HOOK_SRC="$(dirname "$0")/power-monitor-sleep-hook"
SLEEP_HOOK_DST="/usr/lib/systemd/system-sleep/power-monitor-sleep-hook"
SHUTDOWN_SVC_SRC="$(dirname "$0")/power-monitor-shutdown.service"
SHUTDOWN_SVC_DST="/etc/systemd/system/power-monitor-shutdown.service"
STATE_DIR="/var/lib/power-monitor"

case "${1:-}" in
  install)
    echo "Installing power-monitor systemd hooks..."
    mkdir -p "$STATE_DIR"
    chmod 777 "$STATE_DIR"
    cp "$SLEEP_HOOK_SRC" "$SLEEP_HOOK_DST"
    chmod +x "$SLEEP_HOOK_DST"
    cp "$SHUTDOWN_SVC_SRC" "$SHUTDOWN_SVC_DST"
    systemctl daemon-reload
    systemctl enable power-monitor-shutdown.service
    echo "Done."
    ;;
  uninstall)
    echo "Uninstalling power-monitor systemd hooks..."
    systemctl disable power-monitor-shutdown.service 2>/dev/null || true
    rm -f "$SLEEP_HOOK_DST"
    rm -f "$SHUTDOWN_SVC_DST"
    systemctl daemon-reload
    rm -rf "$STATE_DIR"
    echo "Done."
    ;;
  *)
    echo "Usage: sudo $0 {install|uninstall}"
    exit 1
    ;;
esac
