#!/bin/bash
set -e

EXT_UUID="power-monitor@gnome-power-display"
EXT_DIR="$HOME/.local/share/gnome-shell/extensions/$EXT_UUID"
SRC_DIR="$(dirname "$(readlink -f "$0")")"

case "${1:-install}" in
    install)
        rm -rf "$EXT_DIR"
        ln -sfn "$SRC_DIR" "$EXT_DIR"
        glib-compile-schemas "$SRC_DIR/schemas"
        echo "Symlinked $EXT_DIR -> $SRC_DIR"
        echo "Schemas compiled."
        gnome-extensions enable "$EXT_UUID" 2>/dev/null \
            && echo "Extension enabled." \
            || echo "Log out and back in, then: gnome-extensions enable $EXT_UUID"
        ;;
    nested)
        glib-compile-schemas "$SRC_DIR/schemas"
        # Ensure symlink exists
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
        echo "Usage: $0 {install|nested|schemas|log}"
        echo "  install - Symlink extension to GNOME extensions dir (run once)"
        echo "  nested  - Launch nested GNOME Shell with extension auto-enabled"
        echo "  schemas - Recompile gsettings schemas"
        echo "  log     - Tail GNOME Shell logs filtered for this extension"
        ;;
esac
