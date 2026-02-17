#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# Package names
DAEMON_PKG="power-monitor-daemon"
EXTENSION_PKG="power-monitor-gnome-extension"

# Detect package manager
detect_pkg_manager() {
  if command -v dnf &>/dev/null; then
    echo "rpm"
  elif command -v apt &>/dev/null; then
    echo "deb"
  else
    echo "Error: Neither dnf nor apt found." >&2
    exit 1
  fi
}

PKG_TYPE="$(detect_pkg_manager)"
LOCAL_ONLY=0

if [ "${2:-}" = "--local-only" ]; then
  LOCAL_ONLY=1
fi

# Bazel target suffix matches package type
build_packages() {
  echo "Building packages ($PKG_TYPE)..."
  (cd "$PROJECT_DIR" && bazel build "//packaging:daemon-${PKG_TYPE}" "//packaging:extension-${PKG_TYPE}")
}

# Find the built package file from bazel-bin
find_package() {
  local name="$1"
  local pattern
  if [ "$PKG_TYPE" = "rpm" ]; then
    pattern="${PROJECT_DIR}/bazel-bin/packaging/${name}*.rpm"
  else
    pattern="${PROJECT_DIR}/bazel-bin/packaging/${name}*.deb"
  fi
  # shellcheck disable=SC2086
  local found
  found=$(ls $pattern 2>/dev/null | head -1)
  if [ -z "$found" ]; then
    echo "Error: Package not found matching $pattern" >&2
    echo "Run '$0 install' or '$0 reinstall' to build first." >&2
    exit 1
  fi
  echo "$found"
}

do_install() {
  build_packages
  local daemon_pkg extension_pkg
  daemon_pkg="$(find_package "$DAEMON_PKG")"
  extension_pkg="$(find_package "$EXTENSION_PKG")"

  echo "Installing packages..."
  if [ "$LOCAL_ONLY" -eq 1 ]; then
    if [ "$PKG_TYPE" = "rpm" ]; then
      sudo rpm -i "$daemon_pkg" "$extension_pkg"
    else
      sudo dpkg -i "$daemon_pkg" "$extension_pkg"
    fi
  else
    if [ "$PKG_TYPE" = "rpm" ]; then
      sudo dnf install -y "$daemon_pkg" "$extension_pkg"
    else
      sudo apt install -y "$daemon_pkg" "$extension_pkg"
    fi
  fi
  echo "Done."
}

do_uninstall() {
  echo "Uninstalling packages..."
  if [ "$PKG_TYPE" = "rpm" ]; then
    sudo dnf remove -y "$EXTENSION_PKG" "$DAEMON_PKG" 2>/dev/null || true
  else
    sudo apt remove -y "$EXTENSION_PKG" "$DAEMON_PKG" 2>/dev/null || true
  fi
  echo "Done."
}

do_reinstall() {
  do_uninstall
  do_install
}

do_status() {
  echo "=== Installed packages ==="
  if [ "$PKG_TYPE" = "rpm" ]; then
    rpm -q "$DAEMON_PKG" "$EXTENSION_PKG" 2>/dev/null || echo "(not installed)"
  else
    dpkg -l "$DAEMON_PKG" "$EXTENSION_PKG" 2>/dev/null || echo "(not installed)"
  fi
  echo ""
  echo "=== Daemon service ==="
  systemctl status power-monitor-daemon.service --no-pager 2>/dev/null || echo "(not running)"
}

case "${1:-}" in
  install)   do_install ;;
  uninstall) do_uninstall ;;
  reinstall) do_reinstall ;;
  status)    do_status ;;
  *)
    echo "Usage: $0 {install|uninstall|reinstall|status} [--local-only]"
    echo ""
    echo "  install    - Build and install daemon + extension packages"
    echo "  uninstall  - Remove both packages"
    echo "  reinstall  - Uninstall then build and install fresh"
    echo "  status     - Show installed package versions and daemon status"
    echo ""
    echo "  --local-only"
    echo "             - Install local packages directly (rpm -i or dpkg -i)"
    exit 1
    ;;
esac
