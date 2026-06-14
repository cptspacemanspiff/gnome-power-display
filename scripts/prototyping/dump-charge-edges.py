#!/usr/bin/env python3
"""Dump raw charge_now change events with the Linux cache removed -- no analysis, just samples.

With cache_time=0, charge_now changes at the true mAh-crossing times reported by the EC.
This records every change: high-precision timestamp, charge value, and the interval since the
previous edge. The point is to give you the raw timing record (CSV + stdout) so you can inspect
or plot the actual interval distribution yourself.

Run as root:  sudo ./dump-charge-edges.py [--secs 120] [--poll-ms 2]
Restores cache_time on exit. Writes /tmp/charge-edges.csv.
"""

import argparse
import os
import signal
import sys
import time

BAT = "/sys/class/power_supply/BAT1"
CACHE = "/sys/module/battery/parameters/cache_time"
CSV_PATH = "/tmp/charge-edges.csv"


def read_int(p):
    with open(p) as f:
        return int(f.read().strip())


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--secs", type=float, default=120.0)
    ap.add_argument("--poll-ms", type=float, default=2.0)
    ap.add_argument("--cache-ms", type=int, default=0)
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

    csv = open(CSV_PATH, "w")
    csv.write("edge,t_s,charge_uah,delta_uah,dt_s\n")
    print(f"cache_time {orig} -> {read_int(CACHE)}.  Recording charge edges for {args.secs:.0f}s "
          f"at {args.poll_ms:.0f}ms.  Ctrl-C to stop.\n")
    print(f"{'edge':>5}  {'t_s':>10}  {'charge_uah':>11}  {'delta':>7}  {'dt_s':>8}")

    poll = args.poll_ms / 1000.0
    dts = []
    try:
        pc = read_int(f"{BAT}/charge_now")
        t0 = time.monotonic()
        last = t0
        n = 0
        while time.monotonic() - t0 < args.secs:
            time.sleep(poll)
            now = time.monotonic()
            c = read_int(f"{BAT}/charge_now")
            if c == pc:
                continue
            t = now - t0
            dt = now - last
            delta = c - pc
            n += 1
            dts.append(dt)
            csv.write(f"{n},{t:.4f},{c},{delta},{dt:.4f}\n")
            csv.flush()
            print(f"{n:>5}  {t:>10.4f}  {c:>11}  {delta:>+7}  {dt:>8.4f}")
            pc = c
            last = now
    finally:
        restore()
        csv.close()

    if dts:
        body = dts[1:] if len(dts) > 1 else dts   # first dt is vs t0, not a real interval
        print(f"\n{len(dts)} edges. Raw inter-edge dt (s), in order:")
        print("  " + "  ".join(f"{d:.3f}" for d in body))
        # plain histogram of the intervals (showing the samples, not interpreting them)
        bucket = 0.05
        hist = {}
        for d in body:
            b = round(d / bucket) * bucket
            hist[b] = hist.get(b, 0) + 1
        mx = max(hist.values())
        print(f"\nInterval histogram ({bucket*1000:.0f}ms buckets):")
        for b in sorted(hist):
            bar = "#" * round(hist[b] / mx * 40)
            print(f"  {b:>5.2f}s | {bar:<40} {hist[b]}")
    print(f"\nCSV: {CSV_PATH}")


if __name__ == "__main__":
    main()
