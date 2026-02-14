#!/usr/bin/env bash
# Measures non-package power (psys - package) with display off.
# Sets backlight to 0, waits 5s for settling, averages over 30s.
# Restores backlight on exit.

set -euo pipefail

SETTLE=${1:-5}
DURATION=${2:-30}

BL_PATH="/sys/class/backlight/intel_backlight"
RAPL_PKG="/sys/class/powercap/intel-rapl:0/energy_uj"
RAPL_PSYS="/sys/class/powercap/intel-rapl:1/energy_uj"

if (( EUID != 0 )); then
    echo "Requires root. Run: sudo $0 [settle_secs] [duration_secs]"
    exit 1
fi

orig_bl=$(cat "$BL_PATH/brightness")
trap 'echo "$orig_bl" > "$BL_PATH/brightness"; echo "Backlight restored to $orig_bl"' EXIT

echo "Original backlight: $orig_bl / $(cat "$BL_PATH/max_brightness")"
echo "Setting backlight to 0..."
echo 0 > "$BL_PATH/brightness"

echo "Settling for ${SETTLE}s..."
sleep "$SETTLE"

BAT_PATH="/sys/class/power_supply/BAT1"

echo "Sampling for ${DURATION}s..."
pkg1=$(cat "$RAPL_PKG")
psys1=$(cat "$RAPL_PSYS")
charge1=$(cat "$BAT_PATH/charge_now")
voltage1=$(cat "$BAT_PATH/voltage_now")
t1=$(date +%s%3N)

sleep "$DURATION"

pkg2=$(cat "$RAPL_PKG")
psys2=$(cat "$RAPL_PSYS")
charge2=$(cat "$BAT_PATH/charge_now")
voltage2=$(cat "$BAT_PATH/voltage_now")
t2=$(date +%s%3N)

dt=$(( t2 - t1 ))
d_pkg=$(( pkg2 - pkg1 ))
d_psys=$(( psys2 - psys1 ))
d_other=$(( d_psys - d_pkg ))

pkg_w=$(awk "BEGIN{printf \"%.3f\", $d_pkg / ($dt * 1000)}")
psys_w=$(awk "BEGIN{printf \"%.3f\", $d_psys / ($dt * 1000)}")
other_w=$(awk "BEGIN{printf \"%.3f\", $d_other / ($dt * 1000)}")
d_charge=$(( charge1 - charge2 ))  # positive when discharging
avg_voltage=$(( (voltage1 + voltage2) / 2 ))
# power W = delta_uAh * avg_voltage_uV * 3.6e-6 / dt_ms
bat_w=$(awk "BEGIN{printf \"%.3f\", $d_charge * $avg_voltage * 3.6e-6 / $dt}")

echo ""
echo "  Duration:  ${dt} ms"
echo "  RAPL psys: ${psys_w} W"
echo "  RAPL pkg:  ${pkg_w} W"
echo "  RAPL other:${other_w} W  (psys - package)"
echo "  Battery:   ${bat_w} W  (from charge_now delta * voltage)"
