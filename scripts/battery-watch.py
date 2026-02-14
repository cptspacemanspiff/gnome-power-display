#!/usr/bin/env python3
"""Watch battery charge_now, report update intervals, and show a histogram on Ctrl-C.

Similar to charge-update-interval.sh but adds a histogram of update periods.
"""

import time
import sys
import signal

BAT = "/sys/class/power_supply/BAT1"
RAPL = "/sys/class/powercap/intel-rapl:1/energy_uj"  # psys (whole system)
POLL_S = 0.001  # 1ms
HIST_BUCKET_MS = 100
AVG_WINDOW = 30


def read_int(path):
    with open(path) as f:
        return int(f.read().strip())


def main():
    intervals = []
    power_deltas = []  # avg_w - rapl_avg_w, after warmup
    # Rolling window: list of (delta_uah, dt_ms, voltage_uv, rapl_delta_uj)
    window = []

    prev_charge = read_int(f"{BAT}/charge_now")
    prev_rapl = read_int(RAPL)
    prev_time = time.monotonic()
    sample_num = 0

    print(f"Watching {BAT}/charge_now (polling every {POLL_S*1000:.0f}ms)")
    print(f"Initial value: {prev_charge} uAh")
    print(f"Waiting for changes... Ctrl-C for histogram\n")

    print(f"{'#':<5}  {'TIME':<15}  {'INTERVAL':>10}  {'CHARGE (uAh)':>14}  {'DELTA':>8}  {'VOLTAGE':>9}  {'POWER':>8}  {'AVG'+str(AVG_WINDOW):>8}  {'RAPL':>8}  {'RAPL_AVG':>9}  {'DELTA':>8}")

    def show_histogram(signum=None, frame=None):
        if not intervals:
            print("\nNo intervals recorded.")
            sys.exit(0)
        s = sorted(intervals)
        n = len(s)
        print(f"\n\n{'':=<60}")
        print(f"  Update interval histogram ({n} samples)")
        print(f"  Min: {s[0]:.0f} ms  Median: {s[n//2]:.0f} ms  Max: {s[-1]:.0f} ms  Mean: {sum(s)/n:.0f} ms")
        print()

        lo = int(s[0] // HIST_BUCKET_MS) * HIST_BUCKET_MS
        hi = int(s[-1] // HIST_BUCKET_MS) * HIST_BUCKET_MS + HIST_BUCKET_MS
        buckets = {}
        for iv in intervals:
            b = int(iv // HIST_BUCKET_MS) * HIST_BUCKET_MS
            buckets[b] = buckets.get(b, 0) + 1
        max_count = max(buckets.values()) if buckets else 1
        bar_w = 40
        for b in range(lo, hi + 1, HIST_BUCKET_MS):
            count = buckets.get(b, 0)
            bar = "#" * round(count / max_count * bar_w) if count else ""
            print(f"  {b:>5}-{b+HIST_BUCKET_MS:<5} ms  |{bar:<{bar_w}}  {count}")
        print()

        if power_deltas:
            DELTA_BUCKET_W = 0.5
            sd = sorted(power_deltas)
            nd = len(sd)
            print(f"{'':=<60}")
            print(f"  Battery - RAPL power delta histogram ({nd} samples, after {AVG_WINDOW} warmup)")
            print(f"  Min: {sd[0]:+.2f} W  Median: {sd[nd//2]:+.2f} W  Max: {sd[-1]:+.2f} W  Mean: {sum(sd)/nd:+.2f} W")
            print()

            lo = int(sd[0] // DELTA_BUCKET_W) * DELTA_BUCKET_W
            if sd[0] < 0:
                lo = -((-sd[0] // DELTA_BUCKET_W) + 1) * DELTA_BUCKET_W
            hi = int(sd[-1] // DELTA_BUCKET_W + 1) * DELTA_BUCKET_W
            dbuckets = {}
            for d in power_deltas:
                if d < 0:
                    b = -((-d // DELTA_BUCKET_W) + 1) * DELTA_BUCKET_W
                else:
                    b = int(d // DELTA_BUCKET_W) * DELTA_BUCKET_W
                dbuckets[b] = dbuckets.get(b, 0) + 1
            dmax = max(dbuckets.values())
            b = lo
            while b <= hi + DELTA_BUCKET_W / 2:
                count = dbuckets.get(b, 0)
                bar = "#" * round(count / dmax * bar_w) if count else ""
                print(f"  {b:>+6.1f} to {b+DELTA_BUCKET_W:<+5.1f} W  |{bar:<{bar_w}}  {count}")
                b += DELTA_BUCKET_W
            print()

        sys.exit(0)

    signal.signal(signal.SIGINT, show_histogram)

    while True:
        time.sleep(POLL_S)
        charge = read_int(f"{BAT}/charge_now")
        if charge == prev_charge:
            continue

        now = time.monotonic()
        dt_ms = (now - prev_time) * 1000
        delta = charge - prev_charge
        voltage = read_int(f"{BAT}/voltage_now")
        rapl = read_int(RAPL)
        rapl_delta_uj = rapl - prev_rapl
        rapl_w = rapl_delta_uj / (dt_ms * 1000) if dt_ms > 0 else 0  # uJ / ms = mW, /1000 = W

        # Power from delta: delta uAh * voltage uV * 3.6e-6 / dt_ms
        power_w = abs(delta * voltage * 3.6e-6 / dt_ms) if dt_ms > 0 else 0

        sample_num += 1
        intervals.append(dt_ms)
        window.append((delta, dt_ms, voltage, rapl_delta_uj))
        if len(window) > AVG_WINDOW:
            window.pop(0)

        # Average power over window
        sum_energy = sum(d * v * 3.6e-6 for d, _, v, _ in window)
        sum_dt = sum(t for _, t, _, _ in window)
        avg_w = abs(sum_energy / sum_dt) if sum_dt > 0 else 0
        rapl_avg_w = sum(r for _, _, _, r in window) / (sum_dt * 1000) if sum_dt > 0 else 0

        if sample_num >= AVG_WINDOW:
            power_deltas.append(avg_w - rapl_avg_w)

        ts = time.strftime("%H:%M:%S", time.localtime()) + f".{int(time.time()*1000)%1000:03d}"
        print(
            f"{sample_num:<5}  {ts:<15}  {dt_ms:>7.0f} ms  {charge:>14}  {delta:>+8}  "
            f"{voltage/1e6:>7.3f} V  {power_w:>5.2f} W  {avg_w:>5.2f} W  {rapl_w:>5.2f} W  {rapl_avg_w:>6.2f} W  {avg_w - rapl_avg_w:>+5.2f} W"
        )

        prev_charge = charge
        prev_rapl = rapl
        prev_time = now


if __name__ == "__main__":
    main()
