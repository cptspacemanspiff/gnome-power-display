#!/usr/bin/env bash
# Polls charge_now rapidly and reports when the value changes.
set -euo pipefail

BAT="/sys/class/power_supply/BAT1/charge_now"
POLL_MS=10

prev=$(cat "$BAT")
prev_time=$(date +%s%3N)
changes=0

echo "Watching $BAT (polling every ${POLL_MS}ms)"
echo "Initial value: $prev uAh"
echo "Waiting for changes..."
echo ""
VOLT="/sys/class/power_supply/BAT1/voltage_now"
WINDOW=20

# Rolling window of recent (delta, dt_ms, voltage) for avg power calculation
declare -a win_deltas=()
declare -a win_dts=()
declare -a win_volts=()

printf "%-4s  %-12s  %10s  %12s  %10s  %8s  %7s  %s\n" \
    "#" "TIME" "INTERVAL" "CHARGE (uAh)" "DELTA" "VOLTAGE" "POWER" "AVG${WINDOW}"

while true; do
    sleep 0.1
    cur=$(cat "$BAT")
    now=$(date +%s%3N)

    if [[ "$cur" != "$prev" ]]; then
        changes=$(( changes + 1 ))
        dt=$(( now - prev_time ))
        delta=$(( cur - prev ))
        volt=$(cat "$VOLT")
        watts=$(awk "BEGIN{printf \"%.2f\", ($delta * $volt * 3.6e-6) / $dt}")
        abs_watts=$(awk "BEGIN{w = $watts; printf \"%.2f\", (w < 0 ? -w : w)}")
        volt_v=$(awk "BEGIN{printf \"%.3f\", $volt / 1000000}")

        # Add to rolling window
        win_deltas+=("$delta")
        win_dts+=("$dt")
        win_volts+=("$volt")
        if (( ${#win_deltas[@]} > WINDOW )); then
            win_deltas=("${win_deltas[@]:1}")
            win_dts=("${win_dts[@]:1}")
            win_volts=("${win_volts[@]:1}")
        fi

        # Compute average power over window: sum(delta_i * volt_i * 3.6e-6) / sum(dt_i)
        avg_watts=$(awk "BEGIN{
            n = ${#win_deltas[@]}
            split(\"${win_deltas[*]}\", d, \" \")
            split(\"${win_dts[*]}\", t, \" \")
            split(\"${win_volts[*]}\", v, \" \")
            sum_energy = 0; sum_dt = 0
            for (i = 1; i <= n; i++) {
                sum_energy += d[i] * v[i] * 3.6e-6
                sum_dt += t[i]
            }
            w = sum_energy / sum_dt
            printf \"%.2f\", (w < 0 ? -w : w)
        }")

        printf "%-4d  %-12s  %8d ms  %12s  %+8d  %6s V  %5s W  %s W\n" \
            "$changes" "$(date +%H:%M:%S.%3N)" "$dt" "$cur" "$delta" "$volt_v" "$abs_watts" "$avg_watts"
        prev="$cur"
        prev_time="$now"
    fi
done
