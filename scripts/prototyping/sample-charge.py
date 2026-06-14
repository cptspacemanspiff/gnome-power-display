#!/usr/bin/env python3
"""Sample charge_now with the Linux driver cache removed (cache_time -> 0).

With the cache gone, charge_now changes at the true mAh-crossing times reported by the EC
(no 1 s grid). This logs every charge edge -- timestamp, delta, inter-edge dt, and the
implied per-tick power -- so you can see the real cadence and jitter without the cache artifact.

Run as root:  sudo ./sample-charge.py [--secs 90] [--cache-ms 0] [--poll-ms 5]
Restores cache_time on exit (normal / Ctrl-C / error). Writes /tmp/sample-charge.csv.
"""

import argparse
import os
import signal
import sys
import time

BAT = "/sys/class/power_supply/BAT1"
CACHE = "/sys/module/battery/parameters/cache_time"
CSV_PATH = "/tmp/sample-charge.csv"


def read_int(p):
    with open(p) as f:
        return int(f.read().strip())


def vmean_run_partial(edges, start_t):
    """Mean of per-interval voltages for edges at/after start_t (voltage is ~flat, so this
    is a fine representative voltage for the rolling-window energy integral)."""
    vs = [e[4] for e in edges if e[0] >= start_t]
    return sum(vs) / len(vs) if vs else edges[-1][4]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--secs", type=float, default=90.0, help="run duration (default 90)")
    ap.add_argument("--cache-ms", type=int, default=0, help="cache_time during run (default 0)")
    ap.add_argument("--poll-ms", type=float, default=5.0, help="poll interval ms (default 5)")
    ap.add_argument("--window-s", type=float, default=15.0,
                    help="trailing window (s) for rolling integrated power (default 15)")
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
    csv.write("edge_t_s,charge_uah,delta_uah,dt_s,voltage_V,tick_power_W,roll_power_W\n")

    print(f"cache_time {orig} -> {read_int(CACHE)} ms.  Sampling charge_now for {args.secs:.0f}s "
          f"at {args.poll_ms:.0f}ms.  Ctrl-C to stop early.\n")
    print(f"{'#':>4}  {'edge_t':>9}  {'charge_uah':>11}  {'delta':>7}  {'dt_s':>7}  {'volt':>6}  {'tickW':>7}  {'rollW':>7}")

    poll = args.poll_ms / 1000.0
    edges = []  # (edge_t, charge, delta, dt, mean_v_over_dt, power)
    # voltage sampled uncached every poll; track full-run + per-interval means
    v_sum = 0.0
    v_count = 0
    v_min = float("inf")
    v_max = 0.0
    v_distinct = set()
    seg_v_sum = 0.0   # voltage accumulated since last edge (for per-tick mean V)
    seg_v_count = 0
    try:
        pc = read_int(f"{BAT}/charge_now")
        t0 = time.monotonic()
        last_edge = t0
        n = 0
        while time.monotonic() - t0 < args.secs:
            time.sleep(poll)
            now = time.monotonic()
            c = read_int(f"{BAT}/charge_now")
            v = read_int(f"{BAT}/voltage_now") / 1e6  # uncached (cache_time=0)
            v_sum += v
            v_count += 1
            v_min = min(v_min, v)
            v_max = max(v_max, v)
            v_distinct.add(round(v, 4))
            seg_v_sum += v
            seg_v_count += 1
            if c == pc:
                continue
            dt = now - last_edge
            delta = c - pc
            seg_v = seg_v_sum / seg_v_count if seg_v_count else v  # mean V over this interval
            # per-tick power: |delta_uAh| -> Wh = uAh*1e-6*V ; / (dt/3600 h)
            pw = (abs(delta) * 1e-6 * seg_v) / (dt / 3600.0) if dt > 0 else 0.0
            et = now - t0
            n += 1
            edges.append((et, c, delta, dt, seg_v, pw))
            # rolling integrated power over trailing --window-s: exact dQ / measured dt,
            # using the earliest edge still inside the window as the window start.
            roll = float("nan")
            start = None
            for e in edges:
                if e[0] >= et - args.window_s:
                    start = e
                    break
            if start is not None and start is not edges[-1]:
                wq = abs(c - start[1])           # exact uAh over the window
                wdt = et - start[0]
                wv = vmean_run_partial(edges, start[0])
                if wdt > 0:
                    roll = (wq * 1e-6 * wv) / (wdt / 3600.0)
            roll_s = f"{roll:.2f}" if roll == roll else ""
            csv.write(f"{et:.3f},{c},{delta},{dt:.3f},{seg_v:.4f},{pw:.3f},{roll_s}\n")
            csv.flush()
            rollcol = f"{roll:>6.2f}W" if roll == roll else "    -- "
            print(f"{n:>4}  {et:>8.2f}s  {c:>11}  {delta:>+7}  {dt:>6.3f}s  {seg_v:>5.2f}V  {pw:>6.2f}W  {rollcol}")
            pc = c
            last_edge = now
            seg_v_sum = 0.0
            seg_v_count = 0
    finally:
        restore()
        csv.close()

    vstats = (v_sum / v_count if v_count else 0.0, v_min, v_max, len(v_distinct), v_count)
    summarize(edges, vstats)


def summarize(edges, vstats):
    vmean_run, vmin, vmax, vdistinct, vcount = vstats
    print(f"\n{'':=<64}")
    print(f"  voltage (uncached, {vcount} polls): mean={vmean_run:.4f}V  "
          f"min={vmin:.4f}  max={vmax:.4f}  range={1e3*(vmax-vmin):.0f}mV  distinct={vdistinct}")
    print(f"  {len(edges)} charge edges")
    if len(edges) < 2:
        print("  too few edges for stats")
        print(f"\n  CSV: {CSV_PATH}")
        return
    dts = [e[3] for e in edges[1:]]  # skip first (dt vs t0 is not a real inter-edge)
    s = sorted(dts)
    n = len(s)
    mean = sum(s) / n
    print(f"  inter-edge dt (s): min={s[0]:.3f}  median={s[n//2]:.3f}  max={s[-1]:.3f}  mean={mean:.3f}")
    # is it gridded to 1s? check residuals to nearest integer
    resid = [abs(d - round(d)) for d in dts]
    near_int = sum(1 for r in resid if r < 0.1)
    print(f"  dt within 100ms of an integer second: {near_int}/{n} "
          f"({'looks 1s-gridded' if near_int > 0.7 * n else 'NOT 1s-gridded -> true crossing times'})")
    deltas = sorted(set(abs(e[2]) for e in edges))
    print(f"  charge LSB steps (uAh): {deltas[:6]}")
    # exact integrated power over whole run, edge-to-edge (the drift-free anchor)
    q0, qN = edges[0][1], edges[-1][1]
    span = edges[-1][0] - edges[0][0]
    if span > 0 and q0 != qN:
        # use the full-run time-averaged uncached voltage for the energy integral
        w = (abs(q0 - qN) * 1e-6 * vmean_run) / (span / 3600.0)
        print(f"  edge-to-edge integrated power: {w:.3f} W "
              f"(dQ={abs(q0-qN)} uAh over {span:.1f}s, exact -- this is the anchor)")
    tickW = sorted(e[5] for e in edges[1:])
    print(f"  per-tick power (noisy): min={tickW[0]:.2f}  median={tickW[len(tickW)//2]:.2f}  max={tickW[-1]:.2f} W")
    print(f"\n  CSV: {CSV_PATH}")


if __name__ == "__main__":
    main()
