#!/usr/bin/env python3
"""Power from charge edges with GUARANTEED bounds, derived from the monotonic-counter brackets.

charge_now is a monotonic counter, so the true crossing time of the tick reported at t[i] is
provably in (t[i-1], t[i]]. For a window from tick a to tick b:

    dQ = (b-a) mAh                         EXACT, no error
    t*_a in (t[a-1], t[a]],  t*_b in (t[b-1], t[b]]
    dt* = t*_b - t*_a  in ( t[b-1]-t[a] , t[b]-t[a-1] )

    P_max = dQ*V / (t[b-1] - t[a])    (b earliest, a latest)
    P_min = dQ*V / (t[b]   - t[a-1])  (b latest,   a earliest)

These are HARD bounds (not confidence intervals): the true average power over the window is
guaranteed to lie in [P_min, P_max], regardless of whether the EC clock is regular or jittery.
The bracket width shrinks ~1/window-length because the timing slop lives only at the two
endpoints and does not accumulate.

Usage:  ./bracket-power.py [csv] [--window-s 30] [--voltage 16.5] [--step 1]
Reads /tmp/charge-edges.csv (cols: t_s, charge_uah[, voltage_V/edge_t_s]) by default.
Writes /tmp/bracket-power.csv.
"""

import argparse
import csv as csvmod
import sys

DEFAULT_CSV = "/tmp/charge-edges.csv"
OUT_CSV = "/tmp/bracket-power.csv"


def load(path):
    times, charge, volts = [], [], []
    with open(path) as f:
        rows = list(csvmod.DictReader(f))
    if not rows:
        sys.exit(f"{path} is empty")
    tkey = "t_s" if "t_s" in rows[0] else ("edge_t_s" if "edge_t_s" in rows[0] else None)
    if tkey is None:
        sys.exit(f"No t_s/edge_t_s column; have {list(rows[0])}")
    vkey = "voltage_V" if "voltage_V" in rows[0] else None
    for r in rows:
        times.append(float(r[tkey]))
        charge.append(int(r["charge_uah"]))
        volts.append(float(r[vkey]) if vkey and r.get(vkey) else None)
    return times, charge, volts


def power_w(dq_uah, v, dt_s):
    # dQ[uAh] -> Ah*1e-6 ; energy = *V (Wh) ; / (dt/3600 h)  ==  dQ * V * 3.6e-3 / dt
    if dt_s <= 0:
        return float("inf")
    return dq_uah * v * 3.6e-3 / dt_s


def vmean(volts, i, j, fallback):
    seg = [v for v in volts[i:j + 1] if v is not None]
    return sum(seg) / len(seg) if seg else fallback


def bracket(times, charge, volts, i, j, vfallback):
    """Hard [P_min, P_point, P_max] for the window spanning tick index i..j (i>=1)."""
    dq = abs(charge[j] - charge[i])
    v = vmean(volts, i, j, vfallback)
    dt_obs = times[j] - times[i]
    dt_min = times[j - 1] - times[i]      # b earliest, a latest
    dt_max = times[j] - times[i - 1]      # b latest, a earliest
    p_pt = power_w(dq, v, dt_obs)
    p_max = power_w(dq, v, dt_min)        # smaller dt -> larger P
    p_min = power_w(dq, v, dt_max)        # larger dt -> smaller P
    return dq, v, dt_obs, p_min, p_pt, p_max


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("csv", nargs="?", default=DEFAULT_CSV)
    ap.add_argument("--window-s", type=float, default=30.0, help="trailing window length (s)")
    ap.add_argument("--voltage", type=float, default=16.5,
                    help="fallback voltage if CSV has no voltage_V column")
    ap.add_argument("--step", type=int, default=1, help="emit every Nth tick (default 1)")
    args = ap.parse_args()

    times, charge, volts = load(args.csv)
    M = len(times)
    if M < 4:
        sys.exit(f"Only {M} edges -- need more for a windowed bracket.")

    # warn if the counter isn't monotonic over the file (charge<->discharge flip)
    diffs = [charge[k + 1] - charge[k] for k in range(M - 1)]
    if any(d > 0 for d in diffs) and any(d < 0 for d in diffs):
        print("WARNING: charge direction flips in this file; abs(dQ) under-counts across the flip.\n")

    has_v = any(v is not None for v in volts)
    print(f"{M} edges, span {times[-1]-times[0]:.1f}s, voltage {'from CSV' if has_v else f'fixed {args.voltage}V'}, "
          f"trailing window {args.window_s:.0f}s\n")
    print(f"{'t_s':>8}  {'ticks':>5}  {'dt_obs':>7}  {'P_min':>7}  {'P_pt':>7}  {'P_max':>7}  {'bracket':>13}")

    out = open(OUT_CSV, "w")
    out.write("t_s,n_ticks,dQ_uAh,voltage_V,dt_obs_s,P_min_W,P_point_W,P_max_W,width_W,rel_pct\n")

    rel_widths = []
    for j in range(2, M):
        if (j % args.step) != 0:
            continue
        # earliest index i>=1 still inside the trailing window
        i = None
        for k in range(1, j):
            if times[k] >= times[j] - args.window_s:
                i = k
                break
        if i is None or j - i < 2:
            continue
        dq, v, dt_obs, p_min, p_pt, p_max = bracket(times, charge, volts, i, j, args.voltage)
        width = p_max - p_min
        rel = 100 * width / p_pt if p_pt and p_pt != float("inf") else float("inf")
        rel_widths.append(rel)
        out.write(f"{times[j]:.4f},{j-i},{dq},{v:.4f},{dt_obs:.4f},"
                  f"{p_min:.4f},{p_pt:.4f},{p_max:.4f},{width:.4f},{rel:.3f}\n")
        print(f"{times[j]:>8.2f}  {j-i:>5}  {dt_obs:>6.2f}s  {p_min:>6.2f}W  {p_pt:>6.2f}W  "
              f"{p_max:>6.2f}W  [{'%.2f'%p_min}-{'%.2f'%p_max}]")
    out.close()

    if rel_widths:
        s = sorted(rel_widths)
        print(f"\nGuaranteed bracket width at {args.window_s:.0f}s window: "
              f"median {s[len(s)//2]:.1f}%  (min {s[0]:.1f}%, max {s[-1]:.1f}%)")

    # show the 1/T shrinkage: median relative bracket width vs window length
    print(f"\nBracket width vs window length (full file, fixed-V={args.voltage if not has_v else 'csv'}):")
    print(f"  {'window_s':>9}  {'med_rel_width':>13}  {'P_point':>8}")
    for W in [10, 20, 30, 60, 120, 240]:
        rels, pts = [], []
        for j in range(2, M):
            i = next((k for k in range(1, j) if times[k] >= times[j] - W), None)
            if i is None or j - i < 2:
                continue
            _, _, _, p_min, p_pt, p_max = bracket(times, charge, volts, i, j, args.voltage)
            if p_pt and p_pt != float("inf"):
                rels.append(100 * (p_max - p_min) / p_pt)
                pts.append(p_pt)
        if rels:
            rels.sort()
            print(f"  {W:>8}s  {rels[len(rels)//2]:>12.1f}%  {sum(pts)/len(pts):>7.2f}W")
        else:
            print(f"  {W:>8}s  {'(window > capture)':>13}")
    print(f"\nCSV: {OUT_CSV}")


if __name__ == "__main__":
    main()
