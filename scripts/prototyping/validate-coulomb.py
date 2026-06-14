#!/usr/bin/env python3
"""Validate that charge_now is a true coulomb integrator (responds instantly to load)
rather than the integral of the firmware-averaged current.

Method: square-wave the backlight (0% <-> 100%) on a fixed period and watch two signals:

  * charge_slope_W : power derived from the SLOPE of charge_now over a short trailing
                     window (linear regression). This is the integrator's view.
  * current_now_W  : power from current_now * voltage_now -- the firmware moving average.

If charge_now is a real integrator, charge_slope_W snaps to the new level within ~1 tick
(a few seconds of a brightness toggle) while current_now_W ramps over tens of seconds.
The script measures the step-response time (time to reach 63% of the step) for each signal
across every toggle edge and reports the ratio. charge << current => method validated.

Run as root (needed to write brightness and to read RAPL psys):
    sudo ./validate-coulomb.py [--phase 50] [--cycles 3] [--slope-window 12]

Writes a CSV time series to /tmp/coulomb-validate.csv for offline plotting.
"""

import argparse
import os
import signal
import sys
import time

BAT = "/sys/class/power_supply/BAT1"
BL = "/sys/class/backlight/intel_backlight"
RAPL_PSYS = "/sys/class/powercap/intel-rapl:1/energy_uj"
CSV_PATH = "/tmp/coulomb-validate.csv"


def read_int(path):
    with open(path) as f:
        return int(f.read().strip())


def write_int(path, val):
    with open(path, "w") as f:
        f.write(str(val))


def slope_uah_per_s(samples):
    """Least-squares slope of charge (uAh) vs time (s). Negative while discharging."""
    n = len(samples)
    if n < 2:
        return 0.0
    sx = sum(t for t, _ in samples)
    sy = sum(c for _, c in samples)
    sxx = sum(t * t for t, _ in samples)
    sxy = sum(t * c for t, c in samples)
    denom = n * sxx - sx * sx
    if denom == 0:
        return 0.0
    return (n * sxy - sx * sy) / denom


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--phase", type=float, default=50.0,
                    help="seconds per brightness phase (default 50)")
    ap.add_argument("--cycles", type=int, default=3,
                    help="number of dark+bright cycles (default 3)")
    ap.add_argument("--slope-window", type=float, default=12.0,
                    help="trailing window (s) for charge-slope regression (default 12)")
    ap.add_argument("--poll", type=float, default=0.25,
                    help="poll interval seconds (default 0.25)")
    args = ap.parse_args()

    if not os.access(f"{BL}/brightness", os.W_OK):
        sys.exit(f"Cannot write {BL}/brightness -- run with sudo.")

    have_rapl = os.access(RAPL_PSYS, os.R_OK)
    if not have_rapl:
        print("WARNING: cannot read RAPL psys (need root); psys column will be blank.\n")

    bmax = read_int(f"{BL}/max_brightness")
    orig_brightness = read_int(f"{BL}/brightness")

    restored = {"done": False}

    def restore(*_):
        if not restored["done"]:
            try:
                write_int(f"{BL}/brightness", orig_brightness)
            except Exception as e:
                print(f"(failed to restore brightness: {e})")
            restored["done"] = True

    signal.signal(signal.SIGINT, lambda *a: (restore(), sys.exit(0)))
    signal.signal(signal.SIGTERM, lambda *a: (restore(), sys.exit(0)))

    # Time series: (elapsed_s, phase_is_bright, charge_slope_W, current_now_W, psys_W)
    series = []
    edges = []  # (elapsed_s, going_bright)

    win = []  # trailing (elapsed_s, charge_uah) for slope regression
    prev_rapl = read_int(RAPL_PSYS) if have_rapl else None
    prev_rapl_t = None

    csv = open(CSV_PATH, "w")
    csv.write("elapsed_s,brightness_pct,charge_slope_W,current_now_W,psys_W,charge_now_uah\n")

    t_start = time.monotonic()
    print(f"max_brightness={bmax}  orig={orig_brightness}  phase={args.phase}s  "
          f"cycles={args.cycles}  slope_window={args.slope_window}s")
    print(f"Total run ~{args.phase * args.cycles * 2 / 60:.1f} min. Ctrl-C aborts (brightness restored).\n")
    print(f"{'t(s)':>6}  {'phase':<6}  {'chargeW':>8}  {'currentW':>9}  {'psysW':>7}  {'charge_uah':>10}")

    last_print = -1.0

    # Build phase plan: dark, bright, dark, bright, ...
    n_phases = args.cycles * 2
    for phase_idx in range(n_phases):
        going_bright = (phase_idx % 2 == 1)
        target = bmax if going_bright else 0
        write_int(f"{BL}/brightness", target)
        edge_t = time.monotonic() - t_start
        edges.append((edge_t, going_bright))

        phase_end = time.monotonic() + args.phase
        while time.monotonic() < phase_end:
            time.sleep(args.poll)
            now = time.monotonic()
            el = now - t_start
            charge = read_int(f"{BAT}/charge_now")
            current = read_int(f"{BAT}/current_now")
            voltage = read_int(f"{BAT}/voltage_now")
            v = voltage / 1e6
            current_now_w = (current / 1e6) * v

            win.append((el, charge))
            cutoff = el - args.slope_window
            while len(win) > 2 and win[0][0] < cutoff:
                win.pop(0)
            # slope in uAh/s -> amps: uAh/s * 3.6e-3 ; power = A * V
            charge_slope_w = abs(slope_uah_per_s(win)) * 3.6e-3 * v

            psys_w = ""
            if have_rapl:
                r = read_int(RAPL_PSYS)
                if prev_rapl_t is not None and now > prev_rapl_t:
                    dt = now - prev_rapl_t
                    duj = r - prev_rapl
                    if duj < 0:  # wraparound
                        duj = 0
                    psys_w_val = duj / 1e6 / dt
                    psys_w = f"{psys_w_val:.2f}"
                prev_rapl = r
                prev_rapl_t = now

            series.append((el, going_bright, charge_slope_w, current_now_w,
                           float(psys_w) if psys_w else None))
            pct = round(target / bmax * 100)
            csv.write(f"{el:.2f},{pct},{charge_slope_w:.3f},{current_now_w:.3f},"
                      f"{psys_w},{charge}\n")
            csv.flush()

            if el - last_print >= 1.0:
                last_print = el
                ph = "BRIGHT" if going_bright else "DARK"
                print(f"{el:>6.1f}  {ph:<6}  {charge_slope_w:>7.2f}W  "
                      f"{current_now_w:>8.2f}W  {psys_w:>6}  {charge:>10}")

    restore()
    csv.close()
    analyze(series, edges, args.phase)


def step_response_time(series, idx_field, edge_t, phase_dur, t_total_end):
    """Time after edge for a signal to reach 63% of its step toward the phase's
    steady-state value. Returns None if it never gets there within the phase."""
    pre = [s[idx_field] for s in series
           if s[idx_field] is not None and edge_t - 5 <= s[0] < edge_t]
    post_steady = [s[idx_field] for s in series
                   if s[idx_field] is not None
                   and edge_t + phase_dur - 8 <= s[0] < edge_t + phase_dur]
    if not pre or not post_steady:
        return None
    baseline = sum(pre) / len(pre)
    target = sum(post_steady) / len(post_steady)
    step = target - baseline
    if abs(step) < 0.15:  # step too small to time reliably
        return None
    thresh = baseline + 0.63 * step
    rising = step > 0
    for s in series:
        if s[0] < edge_t or s[idx_field] is None:
            continue
        if s[0] > edge_t + phase_dur:
            break
        if (rising and s[idx_field] >= thresh) or (not rising and s[idx_field] <= thresh):
            return s[0] - edge_t
    return None


def analyze(series, edges, phase_dur):
    print(f"\n{'':=<70}")
    print("  STEP-RESPONSE ANALYSIS (time to reach 63% of each toggle's step)")
    print(f"{'':=<70}")
    print(f"  {'edge@s':>8}  {'dir':<6}  {'charge_slope':>13}  {'current_now':>13}  {'ratio':>7}")

    charge_times, current_times, ratios = [], [], []
    # skip the very first edge (cold start, no pre-baseline)
    for edge_t, going_bright in edges[1:]:
        ct = step_response_time(series, 2, edge_t, phase_dur, None)  # charge_slope_W
        it = step_response_time(series, 3, edge_t, phase_dur, None)  # current_now_W
        direction = "->100%" if going_bright else "->0%"
        cs = f"{ct:.1f}s" if ct is not None else "  --"
        is_ = f"{it:.1f}s" if it is not None else "  --"
        ratio = f"{it / ct:.1f}x" if (ct and it and ct > 0) else "  --"
        print(f"  {edge_t:>8.1f}  {direction:<6}  {cs:>13}  {is_:>13}  {ratio:>7}")
        if ct is not None:
            charge_times.append(ct)
        if it is not None:
            current_times.append(it)
        if ct and it and ct > 0:
            ratios.append(it / ct)

    print()
    if charge_times:
        cm = sorted(charge_times)[len(charge_times) // 2]
        print(f"  charge_slope median response: {cm:.1f} s")
    if current_times:
        im = sorted(current_times)[len(current_times) // 2]
        print(f"  current_now  median response: {im:.1f} s")
    if ratios:
        rm = sorted(ratios)[len(ratios) // 2]
        print(f"  median lag ratio: {rm:.1f}x")
        print()
        if rm >= 3:
            print("  ==> VALIDATED: charge_now responds far faster than current_now.")
            print("      charge_now is a true integrator -> windowed coulomb integration")
            print("      escapes the firmware averaging window.")
        else:
            print("  ==> NOT validated: charge_now lags about as much as current_now,")
            print("      suggesting it integrates the already-averaged current.")
    print(f"\n  CSV time series: {CSV_PATH}")


if __name__ == "__main__":
    main()
