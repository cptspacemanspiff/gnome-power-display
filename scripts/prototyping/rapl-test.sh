#!/bin/bash
# Reads RAPL energy counters twice, N seconds apart, and computes power (watts) per domain.
# Includes MSR-based GPU and DRAM counters not exposed via sysfs powercap.

INTERVAL=${1:-5}

# MSR addresses for energy counters not in sysfs
MSR_PP1_ENERGY=0x641   # GPU (GFX)
MSR_DRAM_ENERGY=0x619  # DRAM

# RAPL energy unit from MSR_RAPL_POWER_UNIT (bits 12:8)
read_msr() {
    local msr=$1
    rdmsr -u "$msr" 2>/dev/null
}

declare -A names energies

# --- sysfs powercap domains ---
shopt -s nullglob
for dir in /sys/class/powercap/intel-rapl:* /sys/class/powercap/amd-rapl:*; do
    [ -d "$dir" ] || continue
    id=$(basename "$dir")
    names[$id]=$(cat "$dir/name" 2>/dev/null)
    energies[$id]=$(cat "$dir/energy_uj" 2>/dev/null) || continue
done

# --- MSR-based domains (GPU, DRAM) ---
msr_available=false
if command -v rdmsr &>/dev/null && [ -r /dev/cpu/0/msr ]; then
    msr_available=true
    # Get energy units: MSR 0x606, bits 12:8 = ESU
    raw_unit=$(rdmsr -u 0x606 2>/dev/null)
    if [ -n "$raw_unit" ]; then
        esu=$(( (raw_unit >> 8) & 0x1F ))
        # energy_unit = 1 / 2^ESU (in joules), we'll compute in the final step

        for msr_name in "gpu:$MSR_PP1_ENERGY" "dram:$MSR_DRAM_ENERGY"; do
            label=${msr_name%%:*}
            addr=${msr_name##*:}
            val=$(read_msr "$addr")
            if [ -n "$val" ] && [ "$val" != "0" ]; then
                names["msr-$label"]="$label"
                energies["msr-$label"]="$val"
            fi
        done
    fi
fi

if [ ${#energies[@]} -eq 0 ]; then
    echo "No RAPL domains found." >&2
    exit 1
fi

sleep "$INTERVAL"

printf "%-25s %-15s %s\n" "DOMAIN" "NAME" "POWER (W)"
printf "%-25s %-15s %s\n" "------" "----" "---------"

for id in $(echo "${!energies[@]}" | tr ' ' '\n' | sort); do
    e1=${energies[$id]}

    if [[ "$id" == msr-* ]]; then
        # MSR-based: read raw counter again
        label=${id#msr-}
        if [ "$label" = "gpu" ]; then addr=$MSR_PP1_ENERGY; else addr=$MSR_DRAM_ENERGY; fi
        e2=$(read_msr "$addr")
        [ -z "$e2" ] && continue

        # Handle 32-bit wraparound
        delta=$(( (e2 - e1) & 0xFFFFFFFF ))
        # Convert: delta * (1/2^ESU) / interval
        watts=$(awk "BEGIN {printf \"%.3f\", ($delta / (2^$esu)) / $INTERVAL}")
    else
        # sysfs powercap: values in microjoules
        dir="/sys/class/powercap/$id"
        e2=$(cat "$dir/energy_uj" 2>/dev/null) || continue
        max=$(cat "$dir/max_energy_range_uj" 2>/dev/null)

        delta=$((e2 - e1))
        if [ "$delta" -lt 0 ]; then
            delta=$((delta + max))
        fi

        watts=$(awk "BEGIN {printf \"%.3f\", $delta / ($INTERVAL * 1000000)}")
    fi

    printf "%-25s %-15s %s\n" "$id" "${names[$id]}" "$watts"
done
