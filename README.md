# GNOME Power Display

<p align="center">
  <img src="packaging/PowerLog_logo.png" alt="GNOME Power Display logo" width="280" />
</p>

A power monitoring suite for GNOME on Linux laptops. Tracks battery power draw, charge level, sleep/hibernate events, and display brightness over time.

## Components

### Power Monitor Daemon

A background Go daemon that:
- Samples battery power, voltage, current, capacity, and charge status every 5 seconds
- Tracks display backlight brightness
- Detects sleep and hibernate events (distinguishing between the two)
- Stores all data in a local SQLite database
- Exposes data over D-Bus (`org.gnome.PowerMonitor`)

### GNOME Shell Extension

A panel extension (GNOME 45-49) that visualizes the daemon's data:

- **Panel indicator** showing current power draw in watts
- **Battery Level graph** - line chart with filled area showing charge percentage over time, with charging periods highlighted
- **Energy Usage graph** - bar chart showing average power consumption in configurable time buckets
- **Interactive zoom** - click and drag on any graph to zoom into a time region; back button to return
- **Sleep/hibernate visualization** - shaded regions with labels distinguishing suspend from hibernate
- **Data gap detection** - hatched "No data" regions where the daemon wasn't running, distinct from sleep periods
- **Adaptive detail** - bar granularity scales from 15-second buckets when zoomed in to 1-hour buckets at the 7-day view

### Power Monitor GUI

A GTK desktop app (`power-monitor-gui`) that visualizes the daemon's data — battery level, power draw, and energy history — in a standalone window.

### COSMIC Applet

A panel applet for the COSMIC desktop (`power-monitor-cosmic`) showing live power draw, with a popup for battery percentage, charge status, and brightness.

### Display Calibration Tool

A standalone root CLI (installed as `power-monitor-calibrate`) that measures how much power the display consumes at different brightness levels. Accounts for the battery controller's internal averaging window (~60-90 seconds) by:
- Pinning CPU frequency (including hybrid Intel P-core/E-core support)
- Measuring the battery's update interval and averaging latency
- Waiting for readings to stabilize before sampling

## Installing

Everything installs system-wide via RPM/DEB. One command builds and installs all four
packages (daemon, GUI, GNOME extension, COSMIC applet):

```bash
./scripts/install-packages.sh install      # build + install
./scripts/install-packages.sh status       # show installed versions + daemon status
./scripts/install-packages.sh uninstall    # remove everything
```

After install: the daemon starts automatically; enable the GNOME extension with
`gnome-extensions enable power-monitor@gnome-power-display` (or after logging back in);
add the COSMIC applet from COSMIC Settings → Desktop → Panel → Add applet.

## Building (development)

Requires [Bazel](https://bazel.build/) with `rules_go`:

```bash
# Build everything
bazel build //cmd/power-monitor-daemon //cmd/power-calibrate

# Run the daemon
bazel run //cmd/power-monitor-daemon -- -verbose

# Run calibration (requires root)
sudo bazel-bin/cmd/power-calibrate/power-calibrate_/power-calibrate
```

## Development

```bash
# Test the GNOME extension in a nested GNOME Shell window (no logout required)
# Requires mutter-devel on Fedora
./gnome-extension/install.sh nested

# Test the packaged extension in a nested shell (no local symlink)
./scripts/spawn-wayland-packaged.sh

# Rebuild/reinstall RPMs, then launch packaged nested session
./scripts/debug-packaged-extension.sh

# Same as above, but install local package files directly (rpm -i / dpkg -i)
./scripts/debug-packaged-extension.sh --local-only

# Tail extension logs
./gnome-extension/install.sh log
```

The `nested` command launches a GNOME Shell devkit window with the extension and daemon running inside it. Edit source files, close the window, re-run to test.

## Requirements

- Linux with `/sys/class/power_supply/` battery interface
- GNOME Shell 45-49
- Go 1.22+ (via Bazel)
- systemd-logind (for sleep/hibernate detection)
- `mutter-devel` package (for nested shell development)
