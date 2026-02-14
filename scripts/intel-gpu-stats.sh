#!/usr/bin/env bash
# intel-gpu-stats.sh - Intel GPU monitoring (similar to intel_gpu_top)
# Requires root for debugfs/PMU engine busyness data

set -euo pipefail

INTERVAL="${1:-1}"

# Find the i915 card
CARD=""
for c in /sys/class/drm/card*; do
    [[ -f "$c/device/vendor" ]] || continue
    if [[ "$(cat "$c/device/vendor")" == "0x8086" ]]; then
        CARD="$c"
        break
    fi
done

if [[ -z "$CARD" ]]; then
    echo "No Intel GPU found"
    exit 1
fi

CARD_NAME=$(basename "$CARD")
GT_PATH="$CARD/gt/gt0"
ENGINE_PATH="$CARD/engine"

# Find DRI debugfs number
DRI_NUM=""
for d in /sys/kernel/debug/dri/*/name; do
    if grep -q "i915" "$d" 2>/dev/null; then
        DRI_NUM=$(basename "$(dirname "$d")")
        break
    fi
done

DRI_PATH="/sys/kernel/debug/dri/${DRI_NUM}"

read_file() { cat "$1" 2>/dev/null || echo "N/A"; }

# Detect engines and read busyness from debugfs
declare -A PREV_ENGINE_BUSY
declare -A PREV_ENGINE_TOTAL

engine_label() {
    case "$1" in
        rcs0)  echo "Render/3D" ;;
        bcs0)  echo "Blitter"   ;;
        vcs0)  echo "Video/0"   ;;
        vcs1)  echo "Video/1"   ;;
        vecs0) echo "VideoEnhance" ;;
        ccs0)  echo "Compute/0" ;;
        *)     echo "$1"        ;;
    esac
}

get_time_ns() {
    cat /proc/uptime | awk '{printf "%.0f", $1 * 1000000000}'
}

prev_rc6_ms=""
prev_time_ms=""

# RAPL energy tracking
RAPL_PKG="/sys/class/powercap/intel-rapl:0/energy_uj"
RAPL_CORE="/sys/class/powercap/intel-rapl:0:0/energy_uj"
RAPL_UNCORE="/sys/class/powercap/intel-rapl:0:1/energy_uj"
RAPL_PSYS="/sys/class/powercap/intel-rapl:1/energy_uj"

prev_pkg_uj="" prev_core_uj="" prev_uncore_uj="" prev_psys_uj=""
prev_rapl_ms=""
pkg_watts="" core_watts="" uncore_watts="" psys_watts=""

get_time_ms() {
    date +%s%3N
}

sample_rapl() {
    local now_ms=$(get_time_ms)
    local pkg_uj core_uj uncore_uj psys_uj

    pkg_uj=$(read_file "$RAPL_PKG")
    core_uj=$(read_file "$RAPL_CORE")
    uncore_uj=$(read_file "$RAPL_UNCORE")
    psys_uj=$(read_file "$RAPL_PSYS")

    if [[ -n "$prev_pkg_uj" && -n "$prev_rapl_ms" && "$pkg_uj" != "N/A" ]]; then
        local dt_ms=$(( now_ms - prev_rapl_ms ))
        if (( dt_ms > 0 )); then
            # Power in mW = delta_uj / delta_ms
            if [[ "$prev_pkg_uj" != "N/A" ]]; then
                local d=$(( pkg_uj - prev_pkg_uj ))
                # Handle counter wrap
                (( d < 0 )) && d=$(( d + $(read_file "/sys/class/powercap/intel-rapl:0/max_energy_range_uj") ))
                pkg_watts=$(awk "BEGIN {printf \"%.2f\", $d / ($dt_ms * 1000)}")
            fi
            if [[ "$prev_core_uj" != "N/A" && "$core_uj" != "N/A" ]]; then
                local d=$(( core_uj - prev_core_uj ))
                (( d < 0 )) && d=$(( d + $(read_file "/sys/class/powercap/intel-rapl:0:0/max_energy_range_uj") ))
                core_watts=$(awk "BEGIN {printf \"%.2f\", $d / ($dt_ms * 1000)}")
            fi
            if [[ "$prev_uncore_uj" != "N/A" && "$uncore_uj" != "N/A" ]]; then
                local d=$(( uncore_uj - prev_uncore_uj ))
                (( d < 0 )) && d=$(( d + $(read_file "/sys/class/powercap/intel-rapl:0:1/max_energy_range_uj") ))
                uncore_watts=$(awk "BEGIN {printf \"%.2f\", $d / ($dt_ms * 1000)}")
            fi
            if [[ "$prev_psys_uj" != "N/A" && "$psys_uj" != "N/A" ]]; then
                local d=$(( psys_uj - prev_psys_uj ))
                (( d < 0 )) && d=$(( d + $(read_file "/sys/class/powercap/intel-rapl:1/max_energy_range_uj") ))
                psys_watts=$(awk "BEGIN {printf \"%.2f\", $d / ($dt_ms * 1000)}")
            fi
        fi
    fi

    prev_pkg_uj="$pkg_uj"
    prev_core_uj="$core_uj"
    prev_uncore_uj="$uncore_uj"
    prev_psys_uj="$psys_uj"
    prev_rapl_ms="$now_ms"
}

# Read i915 engine busyness from PMU via perf
# Returns "engine_name busy_ns total_ns" lines
declare -A ENGINE_BUSY_NS

read_engine_busyness() {
    # Use /sys/kernel/debug/dri/N/i915_engine_info or similar
    # Alternatively, parse /sys/class/drm/cardN/engine/*/busy_ticks if available
    # Fallback: use the PMU events
    local pmu_dir="/sys/bus/event_source/devices/i915"
    if [[ ! -d "$pmu_dir" ]]; then
        return 1
    fi
    return 0
}

# Collect engine busy times via perf stat (one-shot sampling)
sample_engines() {
    local duration="$1"
    local result
    # Build perf event list for all engines
    local events=()
    local pmu_dir="/sys/bus/event_source/devices/i915"

    if [[ ! -d "$pmu_dir" ]]; then
        return 1
    fi

    # i915 PMU events are named: rcs0-busy, bcs0-busy, etc.
    for eng_dir in "$ENGINE_PATH"/*/; do
        local eng=$(basename "$eng_dir")
        if [[ -f "$pmu_dir/events/${eng}-busy" ]]; then
            events+=("i915/${eng}-busy/")
        fi
    done

    if (( ${#events[@]} == 0 )); then
        return 1
    fi

    local event_str
    event_str=$(IFS=,; echo "${events[*]}")

    # Run perf stat for the interval
    result=$(perf stat -e "$event_str" -a sleep "$duration" 2>&1) || return 1

    # Parse output: lines like "  1,234,567 ns  i915/rcs0-busy/"
    # perf stat format varies; value is the first numeric field
    while IFS= read -r line; do
        for eng_dir in "$ENGINE_PATH"/*/; do
            local eng=$(basename "$eng_dir")
            if echo "$line" | grep -q "${eng}-busy"; then
                local ns
                ns=$(echo "$line" | sed 's/,//g' | awk '{print $1}')
                if [[ "$ns" =~ ^[0-9]+$ ]]; then
                    ENGINE_BUSY_NS["$eng"]="$ns"
                fi
            fi
        done
    done <<< "$result"
}

bar() {
    local pct=$1 width=${2:-40}
    local filled=$(( pct * width / 100 ))
    (( filled > width )) && filled=$width
    (( filled < 0 )) && filled=0
    local empty=$(( width - filled ))
    local bar_str=""
    for ((i=0; i<filled; i++)); do bar_str+="█"; done
    for ((i=0; i<empty; i++)); do bar_str+=" "; done
    echo "$bar_str"
}

# Check root
if (( EUID != 0 )); then
    echo "Warning: Running without root. Engine busyness data will be unavailable."
    echo "Run with: sudo $0 [interval_seconds]"
    echo ""
    HAS_ROOT=0
else
    HAS_ROOT=1
fi

# Check for perf
HAS_PERF=0
if command -v perf &>/dev/null && (( HAS_ROOT )); then
    HAS_PERF=1
fi

# Clear screen once, hide cursor
printf "\033[2J\033[H\033[?25l"
trap 'printf "\033[?25h"; rm -f /tmp/.gpu-stats-buf' EXIT

# Initial baselines
prev_rc6_ms=$(read_file "$GT_PATH/rc6_residency_ms")
prev_time_ms=$(get_time_ms)
prev_pkg_uj=$(read_file "$RAPL_PKG")
prev_core_uj=$(read_file "$RAPL_CORE")
prev_uncore_uj=$(read_file "$RAPL_UNCORE")
prev_psys_uj=$(read_file "$RAPL_PSYS")
prev_rapl_ms=$(get_time_ms)

while true; do
    # Sample engines in background during the interval
    ENGINE_BUSY_NS=()
    if (( HAS_PERF )); then
        sample_engines "$INTERVAL" || true
    else
        sleep "$INTERVAL"
    fi

    sample_rapl

    # Build all output into a buffer, then write once
    {
    echo "═══════════════════════════════════════════════════════════"
    echo " Intel GPU Monitor - $(lspci 2>/dev/null | grep -i vga | sed 's/.*: //' | head -1)"
    echo " $(date '+%Y-%m-%d %H:%M:%S') | Interval: ${INTERVAL}s"
    echo "═══════════════════════════════════════════════════════════"

    # --- Frequencies ---
    act=$(read_file "$GT_PATH/rps_act_freq_mhz")
    req=$(read_file "$GT_PATH/punit_req_freq_mhz")
    cur=$(read_file "$GT_PATH/rps_cur_freq_mhz")
    min=$(read_file "$GT_PATH/rps_min_freq_mhz")
    max=$(read_file "$GT_PATH/rps_max_freq_mhz")
    boost=$(read_file "$GT_PATH/rps_boost_freq_mhz")
    rp0=$(read_file "$GT_PATH/rps_RP0_freq_mhz")
    rpn=$(read_file "$GT_PATH/rps_RPn_freq_mhz")

    echo ""
    echo " Frequency"
    printf "   Actual: \033[1m%-6s\033[0m MHz  Requested: %-6s MHz\n" "$act" "$req"
    printf "   Range:  %s - %s MHz  Boost: %s MHz\n" "$min" "$max" "$boost"

    if [[ "$act" != "N/A" && "$rp0" != "N/A" && "$rpn" != "N/A" ]]; then
        local_pct=$(( (act - rpn) * 100 / (rp0 - rpn + 1) ))
        (( local_pct > 100 )) && local_pct=100
        (( local_pct < 0 )) && local_pct=0
        printf "   %s MHz [%s] %s MHz  %d%%\n" "$rpn" "$(bar $local_pct)" "$rp0" "$local_pct"
    fi

    # --- Power (RAPL) ---
    echo ""
    echo " Power (RAPL)"
    if [[ -n "$pkg_watts" ]]; then
        printf "   Package:  \033[1m%s W\033[0m  (CPU cores: %s W  GPU/Uncore: %s W)\n" "$pkg_watts" "$core_watts" "$uncore_watts"
        if [[ -n "$psys_watts" ]]; then
            printf "   Platform: \033[1m%s W\033[0m\n" "$psys_watts"
            other_watts=$(awk "BEGIN {printf \"%.2f\", $psys_watts - $pkg_watts}")
            printf "   Other:    \033[1m%s W\033[0m  (platform - package: memory, WiFi, etc.)\n" "$other_watts"
        fi
    else
        echo "   (collecting baseline...)"
    fi

    # --- RC6 ---
    rc6_ms=$(read_file "$GT_PATH/rc6_residency_ms")
    now_ms=$(get_time_ms)

    echo ""
    echo " RC6 (GPU Idle Power State)"

    if [[ -n "$prev_rc6_ms" && "$rc6_ms" != "N/A" && "$prev_rc6_ms" != "N/A" ]]; then
        delta_rc6=$(( rc6_ms - prev_rc6_ms ))
        delta_time=$(( now_ms - prev_time_ms ))
        if (( delta_time > 0 )); then
            rc6_pct=$(( delta_rc6 * 100 / delta_time ))
            (( rc6_pct > 100 )) && rc6_pct=100
            (( rc6_pct < 0 )) && rc6_pct=0
            busy_pct=$(( 100 - rc6_pct ))
            printf "   GPU Busy:  [%s] %d%%\n" "$(bar $busy_pct)" "$busy_pct"
            printf "   RC6 Idle:  [%s] %d%%\n" "$(bar $rc6_pct)" "$rc6_pct"
        fi
    else
        echo "   (collecting baseline...)"
    fi

    prev_rc6_ms="$rc6_ms"
    prev_time_ms="$now_ms"

    # --- Engine Busyness ---
    echo ""
    echo " Engine Busyness"
    if (( ${#ENGINE_BUSY_NS[@]} > 0 )); then
        interval_ns=$(( INTERVAL * 1000000000 ))
        for eng_dir in "$ENGINE_PATH"/*/; do
            eng=$(basename "$eng_dir")
            if [[ -n "${ENGINE_BUSY_NS[$eng]+x}" ]]; then
                busy_ns="${ENGINE_BUSY_NS[$eng]}"
                if [[ "$busy_ns" =~ ^[0-9]+$ ]] && (( interval_ns > 0 )); then
                    eng_pct=$(( busy_ns * 100 / interval_ns ))
                    (( eng_pct > 100 )) && eng_pct=100
                    printf "   %-14s [%s] %d%%\n" "$(engine_label "$eng")" "$(bar $eng_pct)" "$eng_pct"
                fi
            fi
        done
    else
        for eng_dir in "$ENGINE_PATH"/*/; do
            [[ ! -d "$eng_dir" ]] && continue
            eng=$(basename "$eng_dir")
            printf "   %-14s [%s] --%%\n" "$(engine_label "$eng")" "$(bar 0)"
        done
        if (( ! HAS_PERF )); then
            echo "   (requires root + perf for engine busyness)"
        fi
    fi

    # --- Throttle ---
    echo ""
    echo " Throttle Status"
    any_throttle=0
    for f in "$GT_PATH"/throttle_reason_*; do
        [[ ! -f "$f" ]] && continue
        name=$(basename "$f" | sed 's/throttle_reason_//')
        [[ "$name" == "status" ]] && continue
        val=$(cat "$f" 2>/dev/null || echo "0")
        if [[ "$val" != "0" ]]; then
            printf "   \033[1;31m⚠ %-20s ACTIVE\033[0m\n" "$name"
            any_throttle=1
        fi
    done
    if (( any_throttle == 0 )); then
        printf "   \033[32m✓ No throttling\033[0m\n"
    fi

    # --- Power Profile ---
    slpc=$(read_file "$GT_PATH/slpc_power_profile")
    if [[ "$slpc" != "N/A" ]]; then
        echo ""
        echo " SLPC Power Profile: $slpc"
    fi

    echo ""
    echo "───────────────────────────────────────────────────────────"
    echo " Press Ctrl+C to exit"

    # Pad with blank lines to overwrite any leftover from previous frame
    for ((i=0; i<5; i++)); do
        printf "\033[K\n"
    done
    } > /tmp/.gpu-stats-buf

    # Move cursor home and write buffer in one shot (no clear)
    # Append \033[K (erase to EOL) to every line to clear leftover text
    printf "\033[H"
    sed 's/$/\x1b[K/' /tmp/.gpu-stats-buf
done
