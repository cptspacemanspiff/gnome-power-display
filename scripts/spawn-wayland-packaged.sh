#!/bin/bash
set -euo pipefail

EXT_UUID="power-monitor@gnome-power-display"
EXT_DIR="$HOME/.local/share/gnome-shell/extensions/$EXT_UUID"
SYSTEM_EXT_DIR="/usr/share/gnome-shell/extensions/$EXT_UUID"

if [ ! -d "$SYSTEM_EXT_DIR" ]; then
  echo "Packaged extension not found at $SYSTEM_EXT_DIR" >&2
  echo "Install it first: ./scripts/install-packages.sh reinstall" >&2
  exit 1
fi

EXT_BACKUP_DIR=""
if [ -e "$EXT_DIR" ]; then
  EXT_BACKUP_DIR="${EXT_DIR}.bak-packaged-$$"
  mv "$EXT_DIR" "$EXT_BACKUP_DIR"
  echo "Temporarily moved local extension to $EXT_BACKUP_DIR"
fi

cleanup() {
  if [ -n "$EXT_BACKUP_DIR" ] && [ -e "$EXT_BACKUP_DIR" ]; then
    mv "$EXT_BACKUP_DIR" "$EXT_DIR"
    echo "Restored local extension at $EXT_DIR"
  fi
}
trap cleanup EXIT

echo "Starting nested GNOME Shell using packaged extension (close window to stop)..."
dbus-run-session -- bash -c '
  gnome-shell --devkit --wayland &
  SHELL_PID=$!
  for i in $(seq 1 30); do
    if busctl --user list 2>/dev/null | grep -q org.gnome.Shell; then
      sleep 1
      gnome-extensions enable '"$EXT_UUID"' 2>/dev/null && echo "Packaged extension enabled in nested shell."
      break
    fi
    sleep 1
  done
  wait $SHELL_PID
'
