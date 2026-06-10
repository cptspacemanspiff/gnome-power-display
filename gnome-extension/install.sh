#!/bin/bash
# Dev helper for the GNOME Power Monitor extension.
#
# This is for local iteration only (nested-shell testing, schema compiles, log
# tailing). The supported install path is the system package: from the repo root
# run `scripts/install-packages.sh install`, which installs the extension into
# /usr/share/gnome-shell/extensions via RPM/DEB.
set -e

EXT_UUID="power-monitor@gnome-power-display"
EXT_DIR="$HOME/.local/share/gnome-shell/extensions/$EXT_UUID"
SRC_DIR="$(dirname "$(readlink -f "$0")")"

case "${1:-}" in
    nested)
        glib-compile-schemas "$SRC_DIR/schemas"
        # The nested shell loads extensions from the user dir, so symlink the
        # source into it as a throwaway dev sandbox (not a system install).
        if [ ! -L "$EXT_DIR" ]; then
            rm -rf "$EXT_DIR"
            ln -sfn "$SRC_DIR" "$EXT_DIR"
        fi
        echo "Starting nested GNOME Shell (close window to stop)..."
        # Launch gnome-shell, wait for it to be ready, then enable the extension
        DAEMON_BIN="$(dirname "$SRC_DIR")/bazel-bin/cmd/power-monitor-daemon/power-monitor-daemon_/power-monitor-daemon"
        dbus-run-session -- bash -c '
            gnome-shell --devkit --wayland &
            SHELL_PID=$!
            # Wait for shell to register on D-Bus
            for i in $(seq 1 30); do
                if busctl --user list 2>/dev/null | grep -q org.gnome.Shell; then
                    sleep 1
                    gnome-extensions enable '"$EXT_UUID"' 2>/dev/null && echo "Extension enabled in nested shell."
                    # Start daemon if built
                    if [ -x "'"$DAEMON_BIN"'" ]; then
                        "'"$DAEMON_BIN"'" -verbose &
                        DAEMON_PID=$!
                        echo "Daemon started (PID $DAEMON_PID)."
                    else
                        echo "Daemon not built. Run: bazel build //cmd/power-monitor-daemon"
                    fi
                    break
                fi
                sleep 1
            done
            wait $SHELL_PID
            # Kill daemon when gnome-shell exits
            if [ -n "$DAEMON_PID" ]; then
                kill $DAEMON_PID 2>/dev/null
                wait $DAEMON_PID 2>/dev/null
            fi
        '
        ;;
    schemas)
        glib-compile-schemas "$SRC_DIR/schemas"
        echo "Schemas compiled."
        ;;
    log)
        journalctl -f /usr/bin/gnome-shell -o cat | grep -i --line-buffered power
        ;;
    *)
        echo "Usage: $0 {nested|schemas|log}"
        echo "  nested  - Launch nested GNOME Shell with extension auto-enabled (dev sandbox)"
        echo "  schemas - Recompile gsettings schemas"
        echo "  log     - Tail GNOME Shell logs filtered for this extension"
        echo
        echo "To install for real, use the system package:"
        echo "  (repo root) scripts/install-packages.sh install"
        ;;
esac
