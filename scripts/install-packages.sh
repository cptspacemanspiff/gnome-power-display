#!/bin/bash
set -euo pipefail

# Single entry point for building and installing all power-monitor packages:
#   - power-monitor-daemon          (daemon + calibrate, systemd, dbus, config)  [bazel]
#   - power-monitor-gui             (GTK desktop app)                            [bazel]
#   - power-monitor-gnome-extension (GNOME Shell extension)                      [bazel]
#   - power-monitor-cosmic          (COSMIC panel applet)                        [cargo]
#
# Everything installs system-wide to /usr via RPM/DEB. The three bazel packages
# are produced by `bazel build //packaging:...`; the cosmic applet is a cargo
# build, so it is packed by invoking nfpm directly (same nfpm binary bazel uses).

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

VERSION="0.1.0"

DAEMON_PKG="power-monitor-daemon"
GUI_PKG="power-monitor-gui"
EXTENSION_PKG="power-monitor-gnome-extension"
COSMIC_PKG="power-monitor-cosmic"

BAZEL_PKG_DIR="$PROJECT_DIR/bazel-bin/packaging"
DIST_DIR="$PROJECT_DIR/dist"

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
CLEAN=0

for arg in "${@:2}"; do
  case "$arg" in
    --local-only) LOCAL_ONLY=1 ;;
    --clean)      CLEAN=1 ;;
    *) echo "Error: Unknown option: $arg" >&2; exit 1 ;;
  esac
done

# Resolve an nfpm binary: prefer one on PATH, else the one bazel downloaded into
# its external repo (@nfpm_toolchain). The latter exists after any bazel package
# build, which build_bazel_packages runs first.
resolve_nfpm() {
  if command -v nfpm &>/dev/null; then
    command -v nfpm
    return 0
  fi
  local ob nfpm
  ob="$(cd "$PROJECT_DIR" && bazel info output_base 2>/dev/null)" || return 1
  nfpm="$(find "$ob/external" -maxdepth 4 -type f -name nfpm 2>/dev/null | head -1)"
  if [ -n "$nfpm" ] && [ -x "$nfpm" ]; then
    echo "$nfpm"
    return 0
  fi
  echo "Error: nfpm not found. Install nfpm, or ensure bazel can fetch @nfpm_toolchain." >&2
  return 1
}

build_bazel_packages() {
  echo "Building bazel packages ($PKG_TYPE)..."
  (cd "$PROJECT_DIR" && bazel build \
    "//packaging:daemon-${PKG_TYPE}" \
    "//packaging:gui-${PKG_TYPE}" \
    "//packaging:extension-${PKG_TYPE}")
}

build_cosmic_package() {
  echo "Building cosmic applet (cargo)..."
  (cd "$PROJECT_DIR/cosmic-applet" && cargo build --release)
  local nfpm
  nfpm="$(resolve_nfpm)"
  mkdir -p "$DIST_DIR"
  echo "Packaging cosmic applet ($PKG_TYPE)..."
  (cd "$PROJECT_DIR" && VERSION="$VERSION" "$nfpm" package \
    --config packaging/nfpm-cosmic.yaml \
    --packager "$PKG_TYPE" \
    --target "$DIST_DIR/")
}

build_packages() {
  build_bazel_packages
  build_cosmic_package
}

# Resolve a single built package file by name in a directory.
resolve_pkg_file() {
  local name="$1" dir="$2" ext found
  if [ "$PKG_TYPE" = "rpm" ]; then ext="rpm"; else ext="deb"; fi
  # shellcheck disable=SC2086
  found="$(ls "$dir/${name}"*."$ext" 2>/dev/null | head -1)"
  if [ -z "$found" ]; then
    echo "Error: Package not found: ${name}*.${ext} in $dir" >&2
    echo "Run '$0 install' to build first." >&2
    exit 1
  fi
  echo "$found"
}

# Prints the four built package files, one per line (daemon first, since the
# others depend on it).
collect_pkg_files() {
  resolve_pkg_file "$DAEMON_PKG"    "$BAZEL_PKG_DIR"
  resolve_pkg_file "$GUI_PKG"       "$BAZEL_PKG_DIR"
  resolve_pkg_file "$EXTENSION_PKG" "$BAZEL_PKG_DIR"
  resolve_pkg_file "$COSMIC_PKG"    "$DIST_DIR"
}

do_install() { do_install_with_mode 0; }

do_install_with_mode() {
  local force_reinstall="$1"
  build_packages

  local files=()
  mapfile -t files < <(collect_pkg_files)

  echo "Installing packages..."
  if [ "$LOCAL_ONLY" -eq 1 ]; then
    if [ "$PKG_TYPE" = "rpm" ]; then
      if [ "$force_reinstall" -eq 1 ]; then
        sudo rpm -U --replacepkgs "${files[@]}"
      else
        sudo rpm -U "${files[@]}"
      fi
    else
      if [ "$force_reinstall" -eq 1 ]; then
        sudo dpkg -i --force-reinstall "${files[@]}"
      else
        sudo dpkg -i "${files[@]}"
      fi
    fi
  else
    if [ "$PKG_TYPE" = "rpm" ]; then
      if [ "$force_reinstall" -eq 1 ]; then
        sudo dnf reinstall -y "${files[@]}" || sudo dnf install -y "${files[@]}"
      else
        sudo dnf install -y "${files[@]}"
      fi
    else
      if [ "$force_reinstall" -eq 1 ]; then
        sudo apt install --reinstall -y "${files[@]}"
      else
        sudo apt install -y "${files[@]}"
      fi
    fi
  fi
  echo "Done."
}

do_uninstall() {
  echo "Uninstalling packages..."
  # Remove frontends before the daemon they depend on.
  if [ "$PKG_TYPE" = "rpm" ]; then
    sudo dnf remove -y "$COSMIC_PKG" "$EXTENSION_PKG" "$GUI_PKG" "$DAEMON_PKG" 2>/dev/null || true
  else
    sudo apt remove -y "$COSMIC_PKG" "$EXTENSION_PKG" "$GUI_PKG" "$DAEMON_PKG" 2>/dev/null || true
  fi
  echo "Done."
}

do_reinstall() {
  if [ "$CLEAN" -eq 1 ]; then
    do_uninstall
    do_install
    return
  fi
  do_install_with_mode 1
}

do_status() {
  echo "=== Installed packages ==="
  if [ "$PKG_TYPE" = "rpm" ]; then
    rpm -q "$DAEMON_PKG" "$GUI_PKG" "$EXTENSION_PKG" "$COSMIC_PKG" 2>/dev/null || echo "(not installed)"
  else
    dpkg -l "$DAEMON_PKG" "$GUI_PKG" "$EXTENSION_PKG" "$COSMIC_PKG" 2>/dev/null || echo "(not installed)"
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
    echo "Usage: $0 {install|uninstall|reinstall|status} [--local-only] [--clean]"
    echo ""
    echo "  install    - Build and install all four packages (daemon, gui, extension, cosmic)"
    echo "  uninstall  - Remove all four packages"
    echo "  reinstall  - Build and reinstall packages in place"
    echo "  status     - Show installed package versions and daemon status"
    echo ""
    echo "  --local-only  Install local package files directly (rpm -U / dpkg -i)"
    echo "  --clean       With reinstall, uninstall first (wipes package-owned data)"
    exit 1
    ;;
esac
