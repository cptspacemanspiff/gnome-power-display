# GNOME Power Display

GNOME power monitoring suite: a Go daemon that collects battery/power/backlight/sleep data, a calibration tool for display power measurement, and a GNOME Shell extension for visualization.

## Build

Bazel with `rules_go` and `gazelle`. Dependencies managed via `go.mod`.

```bash
# Build everything
bazel build //cmd/power-monitor-daemon //cmd/power-calibrate

# Run tests (use Bazel, not go test)
bazel test //...

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
internal/collector/           Battery, backlight, process, CPU freq, sleep data collection
internal/storage/             SQLite storage with WAL mode
internal/dbus/                D-Bus service (org.gnome.PowerMonitor) on system bus
internal/config/              TOML config loading with validation
internal/calibration/         CPU pinning, brightness control, power sampling, latency measurement
gnome-extension/              GNOME 45-49 Shell extension (panel button, graphs, zoom)
```

## Power Monitor Daemon

Runs as a system-wide systemd service (as root), collects battery, backlight, process CPU, and CPU frequency data every 5 seconds into SQLite (`/var/lib/power-monitor/data.db`), monitors sleep/wake via systemd-logind D-Bus signals, and exposes data over system D-Bus at `org.gnome.PowerMonitor`.

### Configuration

Default config path: `/etc/power-monitor/config.toml`

```toml
[storage]
db_path = "/var/lib/power-monitor/data.db"
state_log_path = "/var/lib/power-monitor/state-log.jsonl"

[collection]
interval_seconds = 5
top_processes = 10
wall_clock_jump_threshold_seconds = 15

[cleanup]
retention_days = 30
interval_hours = 24
```

Config validation ensures all values are positive/non-negative. Invalid configs will cause startup failure with descriptive errors.

### D-Bus Interface

Service name: `org.gnome.PowerMonitor` (system bus)
Object path: `/org/gnome/PowerMonitor`

Methods:
- `GetCurrentStats()` → JSON with latest battery and backlight samples
- `GetHistory(from_epoch, to_epoch)` → JSON with battery and backlight samples in time range
- `GetSleepEvents(from_epoch, to_epoch)` → JSON with sleep/hibernate/shutdown events
- `GetProcessHistory(from_epoch, to_epoch)` → JSON with process CPU usage and CPU frequency samples

All time range methods validate inputs (non-negative, from ≤ to, range ≤ 1 year) to prevent DoS attacks. Database errors are properly propagated to clients as D-Bus errors.

### Command-line Flags

- `-verbose`: Enable all verbose logging (equivalent to `-log=all`)
- `-log=<topics>`: Comma-separated log topics: `battery`, `backlight`, `process`, `sleep`, or `all`
- `-reset-db`: Delete the database and exit
- `-config=<path>`: Path to config file (default: `/etc/power-monitor/config.toml`)

### Sleep/Hibernate/Shutdown Detection

The daemon uses a file-based state log (`/var/lib/power-monitor/state-log.jsonl`) written by systemd hook scripts that run on power state transitions. This is the authoritative source of power state events.

**State Log Format**: Each line is a JSON object:
```json
{"ts": 1234567890, "action": "pre", "what": "suspend", "sleep_action": "suspend"}
{"ts": 1234567920, "action": "post", "what": "suspend", "sleep_action": "suspend"}
```

**Event Reconstruction**: The daemon atomically reads and consumes the state log, reconstructing `PowerStateEvent` records with:
- `type`: `"suspend"`, `"hibernate"`, `"suspend-then-hibernate"`, or `"shutdown"`
- `start_time` and `end_time`: Unix timestamps
- `suspend_secs` and `hibernate_secs`: Duration in each phase (0 if not applicable)

**Wake Detection**: The daemon listens for `PrepareForSleep(false)` D-Bus signals from systemd-logind. When a wake signal is received, it immediately re-reads the state log to import new events. This catches short sleep cycles that don't produce a wall-clock jump. The wake channel uses a buffered size of 1 with non-blocking send; if multiple wakes occur before the main loop reads, subsequent signals are dropped (benign because the state log contains all events and one re-read captures everything).

**Wall-Clock Jump Detection**: On each ticker cycle, the daemon checks if wall-clock time jumped by more than the configured threshold (default 15 seconds). If so, it re-reads the state log to catch events that occurred while the daemon wasn't running.

Wall-clock time is used for sleep duration calculation (Go's monotonic clock stops during suspend — `time.Now().Round(0)` strips the monotonic component so `Sub` uses wall time).

### Process and CPU Frequency Collection

**Process Tracking**: Every collection cycle, the daemon reads `/proc/*/stat` to track per-process CPU usage (utime + stime ticks). It computes tick deltas from the previous cycle and stores the top N processes by CPU usage (default 10). Process cmdlines are cached and pruned when PIDs no longer exist in `prevTicks` to prevent memory leaks.

**CPU Topology Detection**: On startup, the daemon detects P-cores vs E-cores by reading `/sys/devices/system/cpu/cpu*/cpufreq/base_frequency` (or `cpuinfo_max_freq` as fallback). Cores with the highest base frequency are classified as P-cores. This distinction is stored in process samples and CPU frequency samples.

**CPU Frequency Sampling**: Each cycle, the daemon reads `/sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq` for all cores, storing the current frequency along with P-core/E-core classification.

### Data Cleanup

The daemon automatically prunes data older than the configured retention period (default 30 days) on startup and every cleanup interval (default 24 hours). Cleanup runs in a transaction across all tables (battery_samples, backlight_samples, sleep_events, power_state_events, process_samples, cpu_freq_samples).

## GNOME Extension

GNOME 45-49 ESM extension at `gnome-extension/`. UUID: `power-monitor@gnome-power-display`.

### Features

- **Panel indicator**: Shows current power draw in watts
- **Popup stats**: Power draw, battery percentage, charge status, brightness
- **Battery Level graph**: Line chart with filled area, 0-100% scale. Green line with shaded fill. Charging periods shown as a green bar below the axis.
- **Energy Usage graph**: Bar chart showing average power per time bucket. Blue bars for discharging, green for charging. Bucket granularity adapts to zoom level (15s at max zoom up to 1h at 7d view).
- **Time ranges**: 6h, 24h, 7d presets
- **Zoom**: Click and drag on either graph to select a time region. Back button to return to previous view. Supports multiple zoom levels with a stack-based history.
- **Sleep/hibernate regions**: Shaded overlay with labeled "Sleep" or "Hibernate" text
- **No-data regions**: Hatched diagonal pattern with "No data" label for gaps where the daemon wasn't running (distinct from sleep regions)
- **Gap handling**: Lines break at data gaps instead of connecting across missing periods

### Development Workflow

```bash
# Install (run once — symlinks source dir, no copying needed)
./gnome-extension/install.sh install

# Test in nested GNOME Shell (auto-enables extension and starts daemon)
./gnome-extension/install.sh nested

# Recompile schemas after changing gschema.xml
./gnome-extension/install.sh schemas

# Tail GNOME Shell logs
./gnome-extension/install.sh log
```

The `nested` command uses `dbus-run-session -- gnome-shell --devkit --wayland` (requires `mutter-devel` on Fedora). It launches a GNOME Shell window, waits for D-Bus, enables the extension, and starts the daemon automatically.

On Wayland, there is no way to reload GNOME Shell without logging out. The nested session avoids this for development.

### Extension Technical Details

- All graphs drawn with Cairo via `St.DrawingArea`
- `get_surface_size()` can only be called inside repaint handlers; use `get_width()` outside
- `getSettings()` requires the schema ID string explicitly: `this.getSettings('org.gnome.shell.extensions.power-monitor')`
- Settings schema path must match the extension UUID

## Display Calibration Tool

`power-calibrate` is a separate root CLI that measures display power consumption and writes results to `~/.config/power-monitor/calibration.json`. When run via `sudo`, it detects `SUDO_USER` and writes to the real user's home directory with correct ownership.

### How it works

1. **Preparation**: User must close programs, disable WiFi/Bluetooth, unplug devices, run on battery. Any system change takes 1-2 minutes to flush through the battery controller's internal averaging window.

2. **CPU pinning**: Disables turbo boost (`intel_pstate/no_turbo`), locks all cores to `base_frequency`. On hybrid Intel (P-cores + E-cores), each core type gets locked to its own base frequency. Frequency ordering (min before max) is handled to avoid constraint violations.

3. **Initial settling**: Sets brightness to 0% and waits 90 seconds for the battery averaging window to flush.

4. **Update interval measurement**: Rapidly polls `power_now` every 10ms, detects when the value changes, measures the time between consecutive changes. Reports median/min/max across 6 transitions.

5. **Latency measurement**: Measures how long the battery controller's averaging window takes to fully reflect a power change.
   - Waits for baseline to stabilize using `WaitForStable` (slope-matching on quarter-windows)
   - Steps brightness 0% -> 100%
   - Polls at each update cycle, tracking a rolling stddev of the last 5 readings
   - "Settled" = window stddev drops to within 2x of the baseline stddev
   - This measured latency becomes the minimum settling wait for brightness level measurements

6. **Brightness level measurement**: For each level (0%, 25%, 50%, 75%, 100%):
   - Set brightness
   - Wait for the full averaging window to flush (max of measured latency or 90 seconds)
   - Sample power for 30 seconds at 500ms intervals, take the average

7. **Restore**: CPU governor, frequency limits, turbo, and brightness are all restored to original values via deferred closures.

### Key technical details

- **Battery averaging**: The battery firmware/kernel reports power via `/sys/class/power_supply/BAT*/uevent`. The reported `power_now` value has an internal averaging window (typically 60-90 seconds on tested hardware). Any change in system load takes this long to fully reflect in readings.

- **Stabilization detection** (`WaitForStable`): Uses a 20-sample rolling window split into quarters. Compares the slope (rate of change) of the older half vs newer half. Settled = slopes within 1 sigma and overall stddev < 2% of mean. This correctly handles background drift from battery discharge (readings always drift slightly) by only looking for matching rates of change, not absolute flatness.

- **Config output** (`~/.config/power-monitor/calibration.json`):
  ```json
  {
    "update_interval_ms": 1003,
    "latency_ms": 85200,
    "stale_cycles": 82,
    "baseline_power_uw": 5170000,
    "samples": [
      {"brightness_pct": 0, "avg_power_uw": 5170000},
      {"brightness_pct": 25, "avg_power_uw": 5450000}
    ],
    "cpu_frequency_khz": 2400000,
    "calibrated_at": "2026-02-09T12:00:00Z"
  }
  ```

### Pitfalls discovered during development

**Calibration Tool**:
- Battery `power_now` is NOT instantaneous -- it has a long averaging window. You cannot take a quick measurement and trust it.
- A fixed percentage threshold (e.g., 10% above baseline) doesn't work for detecting display power changes because the display delta (~1-2W) is small relative to total system power (~16W). Use stddev-based thresholds instead.
- Trying to detect "stability" by low stddev alone fails because a slow downward drift looks stable in a small window. Must also check for trend/slope.
- Background drift from battery discharge is real and permanent -- readings will always trend slightly. Don't wait for absolute stability; detect when the transient is over and only steady-state drift remains.
- When run as `sudo`, config files end up owned by root. Use `SUDO_USER` to resolve the real user and `os.Chown` after writing.
- On hybrid Intel CPUs, `scaling_min_freq` must be set before `scaling_max_freq` can be lowered below it (and vice versa). Write min first, then max, then min again to handle both directions.

**Power Monitor Daemon**:
- Integer overflow in power calculation: When computing power from voltage × current (if `power_now` isn't reported), divide each by 1000 first to avoid int64 overflow: `(VoltageUV / 1000) * (CurrentUA / 1000)` instead of `(VoltageUV * CurrentUA) / 1000000`.
- ProcessCollector cmdlineCache pruning: Prune based on `prevTicks` (the PIDs being tracked), not `currentTicks` (current snapshot), otherwise transient PIDs accumulate forever causing a memory leak.
- D-Bus error handling: Always check and propagate database errors to clients. Silent failures (using `_` blank identifier) hide critical issues like database corruption or connection failures.
- File descriptor cleanup: Use `defer` for file close and temp file removal to ensure cleanup happens even on early returns or panics (state log processing).
- Config validation: Validate all config values on load. Reject negative intervals, zero retention periods, etc., before the daemon starts to prevent runtime issues.
- SQL identifier construction: When using `fmt.Sprintf` to build SQL with table/column names (not supported by placeholders), document that the identifiers are from a hardcoded compile-time constant slice, not user input, to clarify safety.
