#!/usr/bin/env python3
"""Measure how fast current_now responds to a load step once the ACPI driver cache is lowered,
and use the exact charge_now coulomb integral to debias it.

Probe found: charge_now is a hard ~1 Hz floor (EC coulomb register), but current_now updates
sub-second when cache_time is lowered. This script lowers cache_time, square-waves the backlight
0<->100%, and reports:

  * current_now step-response time (10/63/90%) per toggle edge -- is it actually fast now?
  * exact charge-integral power per phase (the drift-free anchor)
  * a debias factor = charge_integral_mean / current_now_mean per phase, so current_now can be
    scaled to match the coulomb counter's accuracy while keeping its speed.

Run as root (needs brightness write + cache_time write):
    sudo ./fast-current.py [--cache-ms 125] [--phase 40] [--cycles 3] [--poll-ms 50]

Restores cache_time AND brightness on exit (normal, Ctrl-C, or error).
Writes /tmp/fast-current.csv.
"""

import argparse
import os
import signal
import sys
import time

BAT = "/sys/class/power_supply/BAT1"
BL = "/sys/class/backlight/intel_backlight"
CACHE = "/sys/module/battery/parameters/cache_time"
CSV_PATH = "/tmp/fast-current.csv"


def read_int(p):
    with open(p) as f:
        return int(f.read().strip())


def write_int(p, v):
    with open(p, "w") as f:
        f.write(str(v))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--cache-ms", type=int, default=125, help="cache_time during run (default 125)")
    ap.add_argument("--phase", type=float, default=40.0, help="seconds per brightness phase (default 40)")
    ap.add_argument("--cycles", type=int, default=3, help="dark+bright cycles (default 3)")
    ap.add_argument("--poll-ms", type=float, default=50.0, help="poll interval ms (default 50)")
    args = ap.parse_args()

    for path, what in [(f"{BL}/brightness", "brightness"), (CACHE, "cache_time")]:
        if not os.access(path, os.W_OK):
            sys.exit(f"Cannot write {path} ({what}) -- run with sudo.")

    bmax = read_int(f"{BL}/max_brightness")
    orig_bright = read_int(f"{BL}/brightness")
    orig_cache = read_int(CACHE)
    state = {"restored": False}

    def restore(*_):
        if not state["restored"]:
            for path, val, name in [(f"{BL}/brightness", orig_bright, "brightness"),
                                    (CACHE, orig_cache, "cache_time")]:
                try:
                    write_int(path, val)
                except Exception as e:
                    print(f"\nFAILED to restore {name} (was {val}): {e}")
            state["restored"] = True
            print(f"\nRestored brightness={orig_bright}, cache_time={read_int(CACHE)}")

    signal.signal(signal.SIGINT, lambda *a: (restore(), sys.exit(0)))
    signal.signal(signal.SIGTERM, lambda *a: (restore(), sys.exit(0)))

    write_int(CACHE, args.cache_ms)
    poll = args.poll_ms / 1000.0
    series = []   # (elapsed, bright_bool, current_w, charge_uah, voltage_v)
    edges = []    # (elapsed, going_bright)

    csv = open(CSV_PATH, "w")
    csv.write("elapsed_s,brightness_pct,current_now_W,charge_now_uah,voltage_V\n")

    print(f"cache_time={read_int(CACHE)}ms  bmax={bmax}  phase={args.phase}s  cycles={args.cycles}  poll={args.poll_ms}ms")
    print(f"~{args.phase*args.cycles*2/60:.1f} min. Ctrl-C aborts (everything restored).\n")
    print(f"{'t(s)':>6}  {'phase':<6}  {'currentW':>9}  {'charge_uah':>10}")
    last_print = -1.0

    try:
        t0 = time.monotonic()
        for pi in range(args.cycles * 2):
            going_bright = (pi % 2 == 1)
            write_int(f"{BL}/brightness", bmax if going_bright else 0)
            edges.append((time.monotonic() - t0, going_bright))
            phase_end = time.monotonic() + args.phase
            while time.monotonic() < phase_end:
                time.sleep(poll)
                now = time.monotonic()
                el = now - t0
                charge = read_int(f"{BAT}/charge_now")
                current = read_int(f"{BAT}/current_now")
                v = read_int(f"{BAT}/voltage_now") / 1e6
                cw = (current / 1e6) * v
                series.append((el, going_bright, cw, charge, v))
                pct = 100 if going_bright else 0
                csv.write(f"{el:.3f},{pct},{cw:.3f},{charge},{v:.3f}\n")
                if el - last_print >= 1.0:
                    last_print = el
                    print(f"{el:>6.1f}  {'BRIGHT' if going_bright else 'DARK':<6}  {cw:>8.2f}W  {charge:>10}")
    finally:
        restore()
        csv.close()

    analyze(series, edges, args.phase)


def crossings(series, edge_t, phase, fracs):
    """Time after edge for current_now_W to reach each fraction of its step toward steady state."""
    pre = [s[2] for s in series if edge_t - 5 <= s[0] < edge_t]
    steady = [s[2] for s in series if edge_t + phase - 8 <= s[0] < edge_t + phase]
    if not pre or not steady:
        return {f: None for f in fracs}
    base = sum(pre) / len(pre)
    tgt = sum(steady) / len(steady)
    step = tgt - base
    out = {}
    if abs(step) < 0.15:
        return {f: None for f in fracs}
    rising = step > 0
    for f in fracs:
        thr = base + f * step
        out[f] = None
        for s in series:
            if s[0] < edge_t or s[0] > edge_t + phase:
                continue
            if (rising and s[2] >= thr) or (not rising and s[2] <= thr):
                out[f] = s[0] - edge_t
                break
    return out


def charge_integral_W(series, lo, hi):
    """Exact coulomb-integral power between the first and last charge tick in [lo,hi]."""
    seg = [s for s in series if lo <= s[0] < hi]
    if len(seg) < 2:
        return None, 0
    # find first and last DISTINCT charge values (tick edges) and their times
    first = seg[0]
    last = seg[-1]
    # walk to first edge where charge differs from start, and last edge
    t_first = c_first = t_last = c_last = None
    for s in seg:
        if c_first is None:
            t_first, c_first = s[0], s[3]
        elif s[3] != c_first and t_last is None:
            pass
    # simpler: use endpoints' charge; quantization handled by long window
    c0, c1 = seg[0][3], seg[-1][3]
    dt = seg[-1][0] - seg[0][0]
    vmean = sum(s[4] for s in seg) / len(seg)
    if dt <= 0 or c0 == c1:
        return None, dt
    w = (abs(c0 - c1) * 1e-6 * vmean) / (dt / 3600.0)
    return w, dt


def analyze(series, edges, phase):
    print(f"\n{'':=<72}")
    print("  current_now STEP RESPONSE (uncached) + charge-integral debias")
    print(f"{'':=<72}")
    print(f"  {'edge@s':>7}  {'dir':<7}  {'t10%':>6}  {'t63%':>6}  {'t90%':>6}")
    t63s = []
    for et, gb in edges[1:]:
        c = crossings(series, et, phase, [0.10, 0.63, 0.90])
        fmt = lambda x: f"{x:.1f}s" if x is not None else "  --"
        print(f"  {et:>7.1f}  {'->100%' if gb else '->0%':<7}  {fmt(c[0.10]):>6}  {fmt(c[0.63]):>6}  {fmt(c[0.90]):>6}")
        if c[0.63] is not None:
            t63s.append(c[0.63])
    if t63s:
        print(f"\n  median current_now 63% response: {sorted(t63s)[len(t63s)//2]:.1f} s")
        print("  (compare to the ~30-60s you saw with cache_time=1000)")

    print(f"\n  {'phase':<8}  {'charge_intW':>11}  {'current_meanW':>13}  {'debias':>7}")
    edge_ts = [e[0] for e in edges] + [series[-1][0] if series else 0]
    for i, (et, gb) in enumerate(edges):
        lo, hi = et + 12, edge_ts[i + 1]  # trim 12s transient
        cint, dt = charge_integral_W(series, lo, hi)
        seg = [s[2] for s in series if lo <= s[0] < hi]
        cmean = sum(seg) / len(seg) if seg else None
        if cint and cmean:
            print(f"  {'BRIGHT' if gb else 'DARK':<8}  {cint:>10.2f}W  {cmean:>12.2f}W  {cint/cmean:>6.3f}")
    print(f"\n  CSV: {CSV_PATH}")
    print("  If current_now responds in ~1-3s and debias factors are stable/consistent,")
    print("  use debiased high-rate current_now as the primary signal, charge as the anchor.")


if __name__ == "__main__":
    main()
