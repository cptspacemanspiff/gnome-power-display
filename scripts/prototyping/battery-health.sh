#!/usr/bin/env bash
# Print battery identity and health info.
set -euo pipefail

BAT_PATH=""
for p in /sys/class/power_supply/BAT*; do
    [ -d "$p" ] && BAT_PATH="$p" && break
done

if [ -z "$BAT_PATH" ]; then
    echo "No battery found" >&2
    exit 1
fi

read_sysfs() {
    local file="$1"
    if [ -f "$BAT_PATH/$file" ]; then
        cat "$BAT_PATH/$file"
    else
        echo "n/a"
    fi
}

echo "--- Identity ---"
printf "%-25s %s\n" "Manufacturer:" "$(read_sysfs manufacturer)"
printf "%-25s %s\n" "Model:" "$(read_sysfs model_name)"
printf "%-25s %s\n" "Serial:" "$(read_sysfs serial_number)"
printf "%-25s %s\n" "Technology:" "$(read_sysfs technology)"

echo
echo "--- Health ---"
design=$(read_sysfs charge_full_design)
full=$(read_sysfs charge_full)
cycles=$(read_sysfs cycle_count)
vmin=$(read_sysfs voltage_min_design)

printf "%-25s %s µAh\n" "Design capacity:" "$design"
printf "%-25s %s µAh\n" "Current capacity:" "$full"

if [ "$design" != "n/a" ] && [ "$full" != "n/a" ] && [ "$design" -gt 0 ]; then
    capacity_pct=$(awk "BEGIN { printf \"%.1f\", ($full/$design) * 100 }")
    wear=$(awk "BEGIN { printf \"%.1f\", (1 - $full/$design) * 100 }")
    printf "%-25s %s%%\n" "Health:" "$capacity_pct"
    printf "%-25s %s%%\n" "Wear:" "$wear"
fi

printf "%-25s %s\n" "Cycle count:" "$cycles"
printf "%-25s %s µV\n" "Voltage min design:" "$vmin"
