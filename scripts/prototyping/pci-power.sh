#!/bin/bash
# Shows PCI device power states, runtime PM status, and link info.

printf "%-14s %-40s %-10s %-8s %-12s %-10s %s\n" \
    "BDF" "DEVICE" "PM STATE" "RUNTIME" "LINK SPEED" "ASPM" "ACTIVE/SUSPENDED (ms)"
printf "%s\n" "$(printf '=%.0s' {1..130})"

for dev in /sys/bus/pci/devices/*; do
    bdf=$(basename "$dev")

    # Device name from class/vendor/device
    class_code=$(cat "$dev/class" 2>/dev/null)
    vendor=$(cat "$dev/vendor" 2>/dev/null)
    device=$(cat "$dev/device" 2>/dev/null)

    # Try to get a human-readable name
    name=""
    if command -v lspci &>/dev/null; then
        name=$(lspci -s "$bdf" 2>/dev/null | cut -d: -f3- | xargs)
    fi
    [ -z "$name" ] && name="$vendor:$device"
    name="${name:0:40}"

    # PCI PM state (D0=full power, D1/D2=intermediate, D3hot/D3cold=lowest)
    pm_state=$(cat "$dev/power_state" 2>/dev/null || echo "?")

    # Runtime PM
    runtime=$(cat "$dev/power/runtime_status" 2>/dev/null || echo "?")
    active_ms=$(cat "$dev/power/runtime_active_time" 2>/dev/null || echo "?")
    suspended_ms=$(cat "$dev/power/runtime_suspended_time" 2>/dev/null || echo "?")

    # PCIe link speed
    link_speed=$(cat "$dev/current_link_speed" 2>/dev/null || echo "-")

    # ASPM
    aspm=""
    if [ -f "$dev/link/l1_aspm" ]; then
        l1=$(cat "$dev/link/l1_aspm" 2>/dev/null)
        [ "$l1" = "1" ] && aspm="L1"
    fi
    if [ -f "$dev/link/l0s_aspm" ]; then
        l0s=$(cat "$dev/link/l0s_aspm" 2>/dev/null)
        [ "$l0s" = "1" ] && aspm="${aspm:+$aspm+}L0s"
    fi
    [ -z "$aspm" ] && aspm="-"

    printf "%-14s %-40s %-10s %-8s %-12s %-10s %s/%s\n" \
        "$bdf" "$name" "$pm_state" "$runtime" "$link_speed" "$aspm" \
        "$active_ms" "$suspended_ms"
done
