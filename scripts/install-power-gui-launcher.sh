#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

DESKTOP_SRC="$REPO_ROOT/packaging/power-gui.desktop"
ICON_SRC="$REPO_ROOT/packaging/PowerLog_logo.png"

MODE="user"
if [[ "${2:-}" == "--system" ]]; then
  MODE="system"
fi

if [[ "$MODE" == "system" ]]; then
  PREFIX="/usr/share"
  BIN_DIR="/usr/local/bin"
else
  PREFIX="$HOME/.local/share"
  BIN_DIR="$HOME/.local/bin"
fi

APP_DIR="$PREFIX/applications"
ICON_THEME_DIR="$PREFIX/icons/hicolor"
ICON_DIR="$ICON_THEME_DIR/256x256/apps"
PIXMAP_DIR="$PREFIX/pixmaps"

DESKTOP_DST="$APP_DIR/power-gui.desktop"
ICON_DST="$ICON_DIR/power-gui.png"
PIXMAP_ICON_DST="$PIXMAP_DIR/power-gui.png"
BIN_DST="$BIN_DIR/power-gui"

BINARY_SRC="$REPO_ROOT/bazel-bin/cmd/power-gui/power-gui_/power-gui"

write_desktop_file() {
	local icon_value="power-gui"

	while IFS= read -r line; do
		if [[ "$line" == Icon=* ]]; then
			printf 'Icon=%s\n' "$icon_value"
		else
			printf '%s\n' "$line"
		fi
	done < "$DESKTOP_SRC" > "$DESKTOP_DST"

	chmod 0644 "$DESKTOP_DST"
}

refresh_caches() {
  if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "$APP_DIR" >/dev/null 2>&1 || true
  fi

  if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -f -t "$ICON_THEME_DIR" >/dev/null 2>&1 || true
  fi

  if command -v xdg-icon-resource >/dev/null 2>&1; then
    xdg-icon-resource forceupdate >/dev/null 2>&1 || true
  fi
}

require_root_if_system() {
  if [[ "$MODE" == "system" && "$EUID" -ne 0 ]]; then
    echo "System mode requires root. Re-run with sudo." >&2
    exit 1
  fi
}

install_files() {
  require_root_if_system

  if [[ ! -f "$DESKTOP_SRC" ]]; then
    echo "Desktop file not found: $DESKTOP_SRC" >&2
    exit 1
  fi

  if [[ ! -f "$ICON_SRC" ]]; then
    echo "Icon file not found: $ICON_SRC" >&2
    exit 1
  fi

  if [[ ! -x "$BINARY_SRC" ]]; then
    echo "GUI binary not found: $BINARY_SRC" >&2
    echo "Build it first: bazel build //cmd/power-gui" >&2
    exit 1
  fi

  echo "Installing power-gui launcher ($MODE mode)..."
  mkdir -p "$APP_DIR" "$ICON_DIR" "$PIXMAP_DIR" "$BIN_DIR"
  install -m 0644 "$ICON_SRC" "$ICON_DST"
  install -m 0644 "$ICON_SRC" "$PIXMAP_ICON_DST"
  write_desktop_file
  ln -sfn "$BINARY_SRC" "$BIN_DST"
  refresh_caches

  echo "Installed: $DESKTOP_DST"
  echo "Installed: $ICON_DST"
  echo "Installed: $PIXMAP_ICON_DST"
  echo "Installed: $BIN_DST -> $BINARY_SRC"
}

uninstall_files() {
  require_root_if_system

  echo "Uninstalling power-gui launcher ($MODE mode)..."
  rm -f "$DESKTOP_DST" "$ICON_DST" "$PIXMAP_ICON_DST"
  if [[ -L "$BIN_DST" ]]; then
    rm -f "$BIN_DST"
  fi
  refresh_caches

  rmdir --ignore-fail-on-non-empty "$ICON_DIR" "$APP_DIR" >/dev/null 2>&1 || true
  echo "Removed: $DESKTOP_DST"
  echo "Removed: $ICON_DST"
  echo "Removed: $PIXMAP_ICON_DST"
  echo "Removed symlink: $BIN_DST"
}

case "${1:-}" in
  install)
    install_files
    ;;
  uninstall)
    uninstall_files
    ;;
  *)
    echo "Usage: $0 {install|uninstall} [--system]"
    echo "  install   - Install desktop launcher and icon"
    echo "  uninstall - Remove desktop launcher and icon"
    echo "  --system  - Install to /usr/share (requires sudo)"
    echo "             Default is user install to ~/.local/share"
    exit 1
    ;;
esac
