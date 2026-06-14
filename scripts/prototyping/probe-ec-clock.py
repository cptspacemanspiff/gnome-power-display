#!/usr/bin/env python3
"""Recover the EC update clock by epoch-folding value-change timestamps.

We can only see value CHANGES, not EC polls, and charge_now changes are multiples of the
poll period (1 mAh quantum) -- both manufacture apparent irregularity even if the EC clock
is perfectly regular. This script instead asks: do all change timestamps lock onto a single
periodic grid?

For each trial period P it computes the Rayleigh concentration R(P) = |mean exp(i 2 pi t / P)|
over the change timestamps. R near 1 = timestamps strongly phase-locked at period P (a regular
clock); R near the chance level ~1/sqrt(N) = no periodicity (genuinely irregular). The largest
P with high R that explains the others is the fundamental EC period.

KEEP THE SYSTEM IN A STEADY STATE while this runs (don't touch load/brightness) -- we want the
EC's own cadence, not real power changes.

Run as root:  sudo ./probe-ec-clock.py [--secs 120] [--poll-ms 3] [--pmin 0.1] [--pmax 3.0]
Restores cache_time on exit. Writes /tmp/ec-clock.csv (the periodogram).
"""

import argparse
import math
import os
import signal
import sys
import time

BAT = "/sys/class/power_supply/BAT1"
CACHE = "/sys/module/battery/parameters/cache_time"
CSV_PATH = "/tmp/ec-clock.csv"


def read_int(p):
    with open(p) as f:
        return int(f.read().strip())


def rayleigh(times, period):
    """Phase concentration R in [0,1] of event times folded at `period`."""
    if len(times) < 2:
        return 0.0
    w = 2 * math.pi / period
    c = sum(math.cos(w * t) for t in times)
    s = sum(math.sin(w * t) for t in times)
    return math.hypot(c, s) / len(times)


def scan(times, pmin, pmax, n_steps=4000):
    """Periodogram over [pmin,pmax] in frequency space. Returns list of (P, R)."""
    if len(times) < 3:
        return []
    fmin, fmax = 1.0 / pmax, 1.0 / pmin
    out = []
    for i in range(n_steps + 1):
        f = fmin + (fmax - fmin) * i / n_steps
        P = 1.0 / f
        out.append((P, rayleigh(times, P)))
    return out


def find_peaks(grid, top=6):
    """Local maxima in R, strongest first."""
    peaks = []
    for i in range(1, len(grid) - 1):
        if grid[i][1] >= grid[i - 1][1] and grid[i][1] >= grid[i + 1][1]:
            peaks.append(grid[i])
    peaks.sort(key=lambda x: x[1], reverse=True)
    return peaks[:top]


def collect(secs, poll_ms):
    """Record monotonic timestamps of charge_now and current_now changes."""
    poll = poll_ms / 1000.0
    ch_t, cur_t = [], []
    pc = read_int(f"{BAT}/charge_now")
    pcur = read_int(f"{BAT}/current_now")
    t0 = time.monotonic()
    while time.monotonic() - t0 < secs:
        time.sleep(poll)
        now = time.monotonic() - t0
        c = read_int(f"{BAT}/charge_now")
        cu = read_int(f"{BAT}/current_now")
        if c != pc:
            ch_t.append(now); pc = c
        if cu != pcur:
            cur_t.append(now); pcur = cu
    return ch_t, cur_t


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--secs", type=float, default=120.0)
    ap.add_argument("--poll-ms", type=float, default=3.0)
    ap.add_argument("--pmin", type=float, default=0.1, help="min trial period s")
    ap.add_argument("--pmax", type=float, default=3.0, help="max trial period s")
    args = ap.parse_args()

    if not os.access(CACHE, os.W_OK):
        sys.exit(f"Cannot write {CACHE} -- run with sudo.")

    orig = read_int(CACHE)
    state = {"done": False}

    def restore(*_):
        if not state["done"]:
            try:
                with open(CACHE, "w") as f:
                    f.write(str(orig))
                print(f"\nRestored cache_time = {read_int(CACHE)}")
            except Exception as e:
                print(f"\nFAILED to restore cache_time (was {orig}): {e}")
            state["done"] = True

    signal.signal(signal.SIGINT, lambda *a: (restore(), sys.exit(0)))
    signal.signal(signal.SIGTERM, lambda *a: (restore(), sys.exit(0)))

    with open(CACHE, "w") as f:
        f.write("0")
    print(f"cache_time {orig} -> 0.  Collecting change events for {args.secs:.0f}s "
          f"at {args.poll_ms:.0f}ms.  KEEP LOAD STEADY.  Ctrl-C aborts.\n")

    try:
        ch_t, cur_t = collect(args.secs, args.poll_ms)
    finally:
        restore()

    combined = sorted(ch_t + cur_t)
    print(f"\ncharge changes: {len(ch_t)}   current changes: {len(cur_t)}   combined: {len(combined)}")

    csv = open(CSV_PATH, "w")
    csv.write("period_s,R_charge,R_current,R_combined\n")
    g_ch = {p: r for p, r in scan(ch_t, args.pmin, args.pmax)}
    g_cur = scan(cur_t, args.pmin, args.pmax)
    g_comb = scan(combined, args.pmin, args.pmax)
    for (p, rc), (_, rk) in zip(g_cur, g_comb):
        csv.write(f"{p:.4f},{g_ch.get(p,''):},{rc:.4f},{rk:.4f}\n")
    csv.close()

    def report(name, times):
        n = len(times)
        if n < 3:
            print(f"\n{name}: too few events ({n})")
            return
        chance = 1.0 / math.sqrt(n)
        grid = scan(times, args.pmin, args.pmax)
        peaks = find_peaks(grid)
        print(f"\n=== {name} ({n} events, chance R~{chance:.2f}) ===")
        print(f"  {'period_s':>9}  {'freq_Hz':>8}  {'R':>5}  {'vs chance':>9}")
        for P, R in peaks:
            flag = "  <-- strong" if R > 0.6 else ("  significant" if R > 3 * chance else "")
            print(f"  {P:>9.4f}  {1/P:>8.3f}  {R:>5.2f}  {R/chance:>7.1f}x{flag}")
        if not peaks:
            print("  => no peaks")
            return
        rmax = peaks[0][1]
        # the fundamental is the LONGEST period among the strong peaks (others are harmonics)
        strong = [(P, R) for P, R in peaks if R >= 0.85 * rmax]
        fund = max(strong, key=lambda x: x[0])
        if rmax > 0.6:
            print(f"  => REGULAR clock, fundamental P={fund[0]:.4f}s ({1/fund[0]:.3f} Hz), "
                  f"R={fund[1]:.2f}. Apparent irregularity was undersampling/quantization.")
        elif rmax > 3 * chance:
            print(f"  => periodic-ish, fundamental P~{fund[0]:.4f}s but loose (R={fund[1]:.2f}).")
        else:
            print(f"  => NO sharp period (max R={rmax:.2f} ~ chance) -> genuinely irregular.")

    report("charge_now", ch_t)
    report("current_now", cur_t)
    report("combined", combined)
    print(f"\n  Periodogram CSV: {CSV_PATH}")
    print("  Note: peaks at P/2, P/3 etc are harmonics; the FUNDAMENTAL is the largest P")
    print("  with high R. A genuinely irregular clock shows no peak above ~3x chance.")


if __name__ == "__main__":
    main()
