#!/usr/bin/env python3
"""Probe whether the EC updates battery readings faster than the ACPI driver's 1 Hz cache.

The ACPI battery driver caches _BST results for `cache_time` ms (default 1000) -- this is
the source of the 1 s quantization in charge_now/current_now, NOT the hardware. This script
temporarily lowers cache_time, rapidly samples charge_now and current_now, and reports the
real update cadence and LSB the EC actually provides. It restores cache_time on exit.

If charge_now/current_now still only change on ~1 s boundaries with cache_time=0, the EC
itself is rate-limiting and 1 Hz is a hard floor. If they change faster, we just unlocked
finer timing.

Run as root:  sudo ./probe-cache-time.py [--cache-ms 0] [--secs 10] [--poll-ms 2]
"""

import argparse
import os
import signal
import sys
import time

BAT = "/sys/class/power_supply/BAT1"
CACHE = "/sys/module/battery/parameters/cache_time"


def read_int(path):
    with open(path) as f:
        return int(f.read().strip())


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--cache-ms", type=int, default=0,
                    help="cache_time to set during probe, in ms (default 0)")
    ap.add_argument("--secs", type=float, default=10.0, help="probe duration (default 10)")
    ap.add_argument("--poll-ms", type=float, default=2.0, help="poll interval ms (default 2)")
    args = ap.parse_args()

    if not os.path.exists(CACHE):
        sys.exit(f"{CACHE} not found -- ACPI battery driver not loaded as a module.")
    if not os.access(CACHE, os.W_OK):
        sys.exit(f"Cannot write {CACHE} -- run with sudo.")

    orig = read_int(CACHE)
    restored = {"done": False}

    def restore(*_):
        if not restored["done"]:
            try:
                with open(CACHE, "w") as f:
                    f.write(str(orig))
                print(f"\nRestored cache_time = {read_int(CACHE)}")
            except Exception as e:
                print(f"\nFAILED to restore cache_time (was {orig}): {e}")
            restored["done"] = True

    signal.signal(signal.SIGINT, lambda *a: (restore(), sys.exit(0)))
    signal.signal(signal.SIGTERM, lambda *a: (restore(), sys.exit(0)))

    print(f"Original cache_time = {orig} ms")
    with open(CACHE, "w") as f:
        f.write(str(args.cache_ms))
    print(f"Set cache_time = {read_int(CACHE)} ms")
    print(f"Sampling {BAT} for {args.secs:.0f}s at {args.poll_ms:.0f}ms poll...\n")

    poll = args.poll_ms / 1000.0
    try:
        t0 = time.monotonic()
        pc = read_int(f"{BAT}/charge_now")
        pcur = read_int(f"{BAT}/current_now")
        ch_dt, cur_dt = [], []
        ch_lsb, cur_lsb = set(), set()
        lct = lcurt = t0
        nreads = 0
        while time.monotonic() - t0 < args.secs:
            time.sleep(poll)
            now = time.monotonic()
            c = read_int(f"{BAT}/charge_now")
            cu = read_int(f"{BAT}/current_now")
            nreads += 1
            if c != pc:
                ch_dt.append((now - lct) * 1000.0)
                ch_lsb.add(abs(c - pc))
                lct = now
                pc = c
            if cu != pcur:
                cur_dt.append((now - lcurt) * 1000.0)
                cur_lsb.add(abs(cu - pcur))
                lcurt = now
                pcur = cu
    finally:
        restore()

    def report(name, dts, lsbs):
        print(f"\n{name}: {len(dts)} changes in {nreads} reads")
        if dts:
            s = sorted(dts)
            print(f"  dt ms: min={s[0]:.0f}  median={s[len(s)//2]:.0f}  max={s[-1]:.0f}  mean={sum(s)/len(s):.0f}")
            print(f"  LSB steps seen: {sorted(lsbs)[:6]}")
            sub1s = sum(1 for d in dts if d < 950)
            print(f"  sub-1s intervals: {sub1s}/{len(dts)}  "
                  f"({'EC updates faster than 1 Hz -> finer timing available' if sub1s > len(dts)//4 else 'still ~1 Hz -> EC is the floor'})")

    report("charge_now", ch_dt, ch_lsb)
    report("current_now", cur_dt, cur_lsb)


if __name__ == "__main__":
    main()
