#!/usr/bin/env python3
"""Measure the FIRMWARE update clock phase/jitter, separate from the Linux driver cache.

With cache_time lowered to ~0, the Linux sample-and-hold is removed, so every change in
charge_now / current_now lands on a real firmware (EC) register-refresh edge, observable to
within the poll interval. This script timestamps those edges and reports:

  * dt distribution between changes (is the firmware clock ~1 Hz, and how regular?)
  * the CLOCK PHASE: fractional-second position of each edge (circular mean + jitter).
    Tight clustering => clean fixed-phase 1 Hz firmware clock. Smeared => irregular cadence.
  * whether charge_now and current_now refresh on the SAME firmware edge or different phases.

Use this to decide where to place integration-window boundaries: snap them to detected
firmware edges and the +/-1 s phase error disappears.

Run as root:  sudo ./probe-phase.py [--cache-ms 0] [--secs 20] [--poll-ms 8]
Restores cache_time on exit (normal / Ctrl-C / error).
"""

import argparse
import math
import os
import signal
import sys
import time

BAT = "/sys/class/power_supply/BAT1"
CACHE = "/sys/module/battery/parameters/cache_time"


def read_int(p):
    with open(p) as f:
        return int(f.read().strip())


def circular_phase(fracs):
    """Mean phase and jitter (both in seconds, period 1.0) of values in [0,1)."""
    if not fracs:
        return None, None
    sx = sum(math.cos(2 * math.pi * f) for f in fracs) / len(fracs)
    sy = sum(math.sin(2 * math.pi * f) for f in fracs) / len(fracs)
    mean = (math.atan2(sy, sx) / (2 * math.pi)) % 1.0
    R = math.hypot(sx, sy)                       # mean resultant length, 0..1
    # circular stddev in radians -> seconds (period 1s)
    jitter = math.sqrt(-2 * math.log(R)) / (2 * math.pi) if R > 1e-9 else float("inf")
    return mean, jitter


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--cache-ms", type=int, default=0)
    ap.add_argument("--secs", type=float, default=20.0)
    ap.add_argument("--poll-ms", type=float, default=8.0)
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
        f.write(str(args.cache_ms))
    print(f"cache_time {orig} -> {read_int(CACHE)} ms.  Sampling {args.secs:.0f}s at {args.poll_ms:.0f}ms.\n")

    poll = args.poll_ms / 1000.0
    # record (monotonic_edge_time) for each signal's change events
    ch_t, cur_t = [], []
    try:
        pc = read_int(f"{BAT}/charge_now")
        pcur = read_int(f"{BAT}/current_now")
        t0 = time.monotonic()
        while time.monotonic() - t0 < args.secs:
            time.sleep(poll)
            now = time.monotonic()
            c = read_int(f"{BAT}/charge_now")
            cu = read_int(f"{BAT}/current_now")
            if c != pc:
                ch_t.append(now); pc = c
            if cu != pcur:
                cur_t.append(now); pcur = cu
    finally:
        restore()

    def report(name, ts):
        print(f"\n=== {name}: {len(ts)} edges ===")
        if len(ts) < 2:
            print("  too few edges")
            return None
        dts = [ts[i] - ts[i - 1] for i in range(1, len(ts))]
        s = sorted(dts)
        print(f"  dt s: min={s[0]:.3f}  median={s[len(s)//2]:.3f}  max={s[-1]:.3f}")
        fracs = [t % 1.0 for t in ts]
        ph, jit = circular_phase(fracs)
        print(f"  clock phase: {ph:.3f}s into each second  |  jitter (circ stddev): {jit*1000:.0f} ms")
        verdict = ("clean fixed-phase ~1 Hz clock" if jit < 0.05
                   else "loose phase" if jit < 0.2 else "irregular / not a single clock")
        print(f"  -> {verdict}")
        return ph

    ph_charge = report("charge_now", ch_t)
    ph_cur = report("current_now", cur_t)

    if ph_charge is not None and ph_cur is not None:
        off = (ph_cur - ph_charge) % 1.0
        off = min(off, 1.0 - off)  # nearest separation
        print(f"\n=== charge vs current phase offset ===")
        print(f"  {off*1000:.0f} ms apart -> "
              f"{'same firmware refresh edge' if off < 0.05 else 'different edges / staggered EC reads'}")
    print("\nIf the phase is clean and fixed, snap integration-window boundaries to detected")
    print("edges (start/stop the window AT a charge_now change) to eliminate the +/-1s phase error.")


if __name__ == "__main__":
    main()
