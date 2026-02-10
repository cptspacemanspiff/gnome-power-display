# Spiff Power Tools

GNOME power monitoring suite: a Go daemon that collects battery/power/backlight/sleep data, a calibration tool for display power measurement, and a GNOME Shell extension for visualization.

## Build

Bazel with `rules_go` and `gazelle`. Dependencies managed via `go.mod`.

```bash
# Build everything
bazel build //cmd/power-monitor-daemon //cmd/power-calibrate

# Run daemon
bazel run //cmd/power-monitor-daemon
bazel run //cmd/power-monitor-daemon -- -verbose

# Run calibration (requires root for CPU freq and backlight control)
sudo bazel-bin/cmd/power-calibrate/power-calibrate_/power-calibrate
```

## Project Structure

```
cmd/power-monitor-daemon/     Daemon: collects data, exposes D-Bus service
cmd/power-calibrate/          CLI tool: measures display power at brightness levels
internal/collector/           Battery, backlight, sleep data collection from sysfs
internal/storage/             SQLite storage (battery_samples, backlight_samples, sleep_events)
internal/dbus/                D-Bus service (org.gnome.PowerMonitor)
internal/calibration/         CPU pinning, brightness control, power sampling, latency measurement
gnome-extension/              GNOME 45+ Shell extension (panel button, power graph)
```

## Power Monitor Daemon

Runs as a user service, collects battery and backlight data every 5 seconds into SQLite (`~/.local/share/power-monitor/data.db`), monitors sleep/wake via systemd-logind D-Bus signals, and exposes data over session D-Bus at `org.gnome.PowerMonitor`.

D-Bus methods: `GetCurrentStats()`, `GetHistory(from, to)`, `GetSleepEvents(from, to)`.

Use `-verbose` flag to log every sample and collector errors.

## Display Calibration Tool

`power-calibrate` is a separate root CLI that measures display power consumption and writes results to `~/.config/power-monitor/calibration.json`. When run via `sudo`, it detects `SUDO_USER` and writes to the real user's home directory with correct ownership.

### How it works

1. **Preparation**: User must close programs, disable WiFi/Bluetooth, unplug devices, run on battery. Any system change takes 1-2 minutes to flush through the battery controller's internal averaging window.

2. **CPU pinning**: Disables turbo boost (`intel_pstate/no_turbo`), locks all cores to `base_frequency`. On hybrid Intel (P-cores + E-cores), each core type gets locked to its own base frequency. Frequency ordering (min before max) is handled to avoid constraint violations.

3. **Initial settling**: Sets brightness to 0% and waits 90 seconds for the battery averaging window to flush.

4. **Update interval measurement**: Rapidly polls `power_now` every 10ms, detects when the value changes, measures the time between consecutive changes. Reports median/min/max across 6 transitions.

5. **Latency measurement**: Measures how long the battery controller's averaging window takes to fully reflect a power change.
   - Waits for baseline to stabilize using `WaitForStable` (slope-matching on quarter-windows)
   - Steps brightness 0% → 100%
   - Polls at each update cycle, tracking a rolling stddev of the last 5 readings
   - "Settled" = window stddev drops to within 2× of the baseline stddev
   - This measured latency becomes the minimum settling wait for brightness level measurements

6. **Brightness level measurement**: For each level (0%, 25%, 50%, 75%, 100%):
   - Set brightness
   - Wait for the full averaging window to flush (max of measured latency or 90 seconds)
   - Sample power for 30 seconds at 500ms intervals, take the average

7. **Restore**: CPU governor, frequency limits, turbo, and brightness are all restored to original values via deferred closures.

### Key technical details

- **Battery averaging**: The battery firmware/kernel reports power via `/sys/class/power_supply/BAT*/uevent`. The reported `power_now` value has an internal averaging window (typically 60-90 seconds on tested hardware). Any change in system load takes this long to fully reflect in readings.

- **Stabilization detection** (`WaitForStable`): Uses a 20-sample rolling window split into quarters. Compares the slope (rate of change) of the older half vs newer half. Settled = slopes within 1σ and overall stddev < 2% of mean. This correctly handles background drift from battery discharge (readings always drift slightly) by only looking for matching rates of change, not absolute flatness.

- **Config output** (`~/.config/power-monitor/calibration.json`):
  ```json
  {
    "update_interval_ms": 1003,
    "latency_ms": 85200,
    "stale_cycles": 82,
    "baseline_power_uw": 5170000,
    "samples": [
      {"brightness_pct": 0, "avg_power_uw": 5170000},
      {"brightness_pct": 25, "avg_power_uw": 5450000},
      ...
    ],
    "cpu_frequency_khz": 2400000,
    "calibrated_at": "2026-02-09T12:00:00Z"
  }
  ```

### Pitfalls discovered during development

- Battery `power_now` is NOT instantaneous — it has a long averaging window. You cannot take a quick measurement and trust it.
- A fixed percentage threshold (e.g., 10% above baseline) doesn't work for detecting display power changes because the display delta (~1-2W) is small relative to total system power (~16W). Use stddev-based thresholds instead.
- Trying to detect "stability" by low stddev alone fails because a slow downward drift looks stable in a small window. Must also check for trend/slope.
- Background drift from battery discharge is real and permanent — readings will always trend slightly. Don't wait for absolute stability; detect when the transient is over and only steady-state drift remains.
- When run as `sudo`, config files end up owned by root. Use `SUDO_USER` to resolve the real user and `os.Chown` after writing.
- On hybrid Intel CPUs, `scaling_min_freq` must be set before `scaling_max_freq` can be lowered below it (and vice versa). Write min first, then max, then min again to handle both directions.

## GNOME Extension

GNOME 45+ ESM extension at `gnome-extension/`. Shows current power draw in the panel, popup with stats and a Cairo-drawn power graph (1h/6h/24h/7d). Uses D-Bus proxy to query the daemon. Install by copying to `~/.local/share/gnome-shell/extensions/power-monitor@gnome-power-display/`.

Settings schema at `gnome-extension/schemas/` provides `refresh-interval` preference.
