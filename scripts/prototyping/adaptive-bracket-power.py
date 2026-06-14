#!/usr/bin/env python3
"""Adaptive-endpoint bracketed power, with statistical error bars from many windows.

Builds on bracket-power.py / BRACKET-METHOD.md. The hard timing bracket for a window from
tick a to tick b is:

    dt_obs = t[b] - t[a]                                   (observed)
    dt_min = dt_obs - gap_b ,  dt_max = dt_obs + gap_a     (gap_x = t[x]-t[x-1])
    P in [ dQ*V/dt_max , dQ*V/dt_min ]                     GUARANTEED

The crucial fact: the total timing uncertainty is  gap_a + gap_b  -- the inter-edge gaps
straddling the TWO endpoints. It does NOT depend on how many ticks the window spans. So the
fractional bracket is roughly (gap_a + gap_b) / dt_obs.

==> Two ways to shrink the bracket:
      1. grow the window (dt_obs up)          -- the 1/T law, what bracket-power.py does
      2. land both endpoints on SMALL-gap ticks (gap_a + gap_b down)   <-- this script

Endpoint gaps here span ~0.6s..2.4s. A window bounded by two ~0.6s ticks has ~1.2s of slop;
the same window bounded by two ~2.4s ticks has ~4.8s -- 4x wider for ZERO extra waiting. So
rather than waiting longer, we ADAPTIVELY pick endpoints: grow each window only until the
tightest reachable endpoints bring (gap_a+gap_b)/dt under a target, then stop.

PRIMARY METHOD -- combine ALL edges by least squares (combined_fit):
  Tiling into non-overlapping windows wastes data: it estimates ONE number (the power) from
  only 2 of the N inter-edge measurements. Since every tick is an exact charge quantum, under
  constant power the true crossing times are evenly spaced: t*[i] = t0 + tau*n[i] with n[i] an
  EXACT integer tick index. So fit a straight line with TIME on the y-axis and tick index on
  the x-axis -- ordinary least squares -- and P = 3.6 * V / slope. This is the minimum-variance
  combination of every edge; the slope's standard error shrinks like ~span^-1 * N^-1/2 instead
  of giving you just two tiles. (Do NOT average the per-tick dt's: they telescope to
  (t_last - t_first)/N, i.e. back to the two endpoints -- no gain.)
  Error bars: classical OLS SE (residuals independent) AND a Bartlett-HAC SE (robust to the
  autocorrelation an EC-clock beat induces). The reported bar is timing-scatter only; the
  guaranteed bracket bounds the true average unconditionally.

SECONDARY -- the windowed bracket method (below) is kept as a cross-check and for the
per-window CSV. The hard timing bracket for a window from tick a to tick b is:

    dt_obs = t[b] - t[a]                                   (observed)
    dt_min = dt_obs - gap_b ,  dt_max = dt_obs + gap_a     (gap_x = t[x]-t[x-1])
    P in [ dQ*V/dt_max , dQ*V/dt_min ]                     GUARANTEED

The crucial fact: the total timing uncertainty is  gap_a + gap_b  -- the inter-edge gaps
straddling the TWO endpoints. It does NOT depend on how many ticks the window spans. So the
fractional bracket is roughly (gap_a + gap_b) / dt_obs. Two ways to shrink it: grow the window
(1/T), or land both endpoints on SMALL-gap ticks (this script's adaptive endpoint selection).
Tiling into non-overlapping windows then gives independent estimates -> empirical mean +/- s.

Usage:
  # capture live (root; lowers cache_time so charge_now ticks at true crossing times)
  sudo ./adaptive-bracket-power.py --secs 180 [--rel-target 0.03] [--min-window 5]
  # or analyse an existing capture
  ./adaptive-bracket-power.py --csv /tmp/charge-edges.csv [--rel-target 0.03]

Reads/accepts the columns written by dump-charge-edges.py and sample-charge.py
(t_s/edge_t_s, charge_uah, optional voltage_V). Writes /tmp/adaptive-bracket-power.csv.
"""

import argparse
import csv as csvmod
import os
import signal
import statistics
import sys
import time

BAT = "/sys/class/power_supply/BAT1"
CACHE = "/sys/module/battery/parameters/cache_time"
OUT_CSV = "/tmp/adaptive-bracket-power.csv"
RAW_CSV = "/tmp/adaptive-charge-edges.csv"


# ----------------------------------------------------------------------------- data sources

def read_int(p):
    with open(p) as f:
        return int(f.read().strip())


def _chown_to_sudo_user(path):
    """Hand a root-created file back to the invoking user (so it's usable after sudo)."""
    uid, gid = os.environ.get("SUDO_UID"), os.environ.get("SUDO_GID")
    if uid and gid:
        try:
            os.chown(path, int(uid), int(gid))
        except OSError:
            pass


def write_csv(path, header, rows):
    """Write a CSV robustly. A stale file owned by another user (common when alternating
    `sudo` capture with unprivileged analysis) carries a foreign SELinux label that even root
    can't truncate -- so unlink it and recreate fresh on the first failure. Never fatal: the
    full results are already on stdout."""
    body = header + "".join(rows)
    for attempt in (0, 1):
        try:
            with open(path, "w") as out:
                out.write(body)
            _chown_to_sudo_user(path)
            return True
        except OSError as e:
            if attempt == 0:
                try:
                    os.unlink(path)
                except OSError:
                    pass
                continue
            print(f"  (could not write {path}: {e}; results above are complete)")
            return False


def load_csv(path):
    """Return (times, charge_uah, volts) from a dump-charge / sample-charge CSV."""
    with open(path) as f:
        rows = list(csvmod.DictReader(f))
    if not rows:
        sys.exit(f"{path} is empty")
    tkey = "t_s" if "t_s" in rows[0] else ("edge_t_s" if "edge_t_s" in rows[0] else None)
    if tkey is None:
        sys.exit(f"No t_s/edge_t_s column; have {list(rows[0])}")
    vkey = "voltage_V" if "voltage_V" in rows[0] else None
    times, charge, volts = [], [], []
    for r in rows:
        times.append(float(r[tkey]))
        charge.append(int(r["charge_uah"]))
        volts.append(float(r[vkey]) if vkey and r.get(vkey) else None)
    return times, charge, volts


def capture_live(secs, poll_ms, cache_ms):
    """Record charge_now edges with cache_time lowered. Returns (times, charge, volts) where
    volts[i] is the mean uncached voltage over the interval ending at edge i."""
    if not os.access(CACHE, os.W_OK):
        sys.exit(f"Cannot write {CACHE} -- run with sudo (or pass --csv to analyse a capture).")

    orig = read_int(CACHE)
    done = {"v": False}

    def restore(*_):
        if not done["v"]:
            try:
                with open(CACHE, "w") as f:
                    f.write(str(orig))
                print(f"\nRestored cache_time = {read_int(CACHE)}")
            except Exception as e:
                print(f"\nFAILED to restore cache_time (was {orig}): {e}")
            done["v"] = True

    signal.signal(signal.SIGINT, lambda *a: (restore(), sys.exit(0)))
    signal.signal(signal.SIGTERM, lambda *a: (restore(), sys.exit(0)))
    with open(CACHE, "w") as f:
        f.write(str(cache_ms))

    print(f"cache_time {orig} -> {read_int(CACHE)} ms.  Capturing charge edges for {secs:.0f}s "
          f"at {poll_ms:.0f}ms poll.  Ctrl-C to stop early.\n")

    poll = poll_ms / 1000.0
    times, charge, volts = [], [], []
    seg_v_sum, seg_v_n = 0.0, 0
    try:
        pc = read_int(f"{BAT}/charge_now")
        t0 = time.monotonic()
        # seed endpoint 0 so the first real edge already has a preceding interval
        times.append(0.0)
        charge.append(pc)
        volts.append(None)
        while time.monotonic() - t0 < secs:
            time.sleep(poll)
            now = time.monotonic()
            c = read_int(f"{BAT}/charge_now")
            v = read_int(f"{BAT}/voltage_now") / 1e6
            seg_v_sum += v
            seg_v_n += 1
            if c == pc:
                continue
            times.append(now - t0)
            charge.append(c)
            volts.append(seg_v_sum / seg_v_n)
            print(f"  edge {len(times)-1:>4}  t={now-t0:>7.2f}s  q={c}  dq={c-pc:+}  "
                  f"gap={times[-1]-times[-2]:.3f}s")
            pc = c
            seg_v_sum, seg_v_n = 0.0, 0
    finally:
        restore()
    return times, charge, volts


# ----------------------------------------------------------------------------- bracket math

def power_w(dq_uah, v, dt_s):
    if dt_s <= 0:
        return float("inf")
    return dq_uah * v * 3.6e-3 / dt_s  # 1 mAh = 3.6 C


def vmean(volts, a, b, fallback):
    seg = [x for x in volts[a:b + 1] if x is not None]
    return sum(seg) / len(seg) if seg else fallback


def bracket(times, charge, volts, a, b, vfb):
    """Hard window result for tick indices a..b (a>=1). Returns dict."""
    dq = abs(charge[b] - charge[a])
    v = vmean(volts, a, b, vfb)
    dt_obs = times[b] - times[a]
    gap_a = times[a] - times[a - 1]
    gap_b = times[b] - times[b - 1]
    dt_min = dt_obs - gap_b      # b earliest, a latest  -> largest P
    dt_max = dt_obs + gap_a      # b latest,  a earliest -> smallest P
    p_pt = power_w(dq, v, dt_obs)
    p_max = power_w(dq, v, dt_min)
    p_min = power_w(dq, v, dt_max)
    rel = (gap_a + gap_b) / dt_obs if dt_obs > 0 else float("inf")
    return dict(a=a, b=b, dq=dq, v=v, dt=dt_obs, gap_a=gap_a, gap_b=gap_b,
                p_min=p_min, p_pt=p_pt, p_max=p_max, rel=rel)


# ------------------------------------------------------------------- adaptive window builder

def best_start(times, cursor, lookahead_s, M):
    """From `cursor`, return the tick within `lookahead_s` with the smallest preceding gap
    -> tightest available start endpoint."""
    best, best_gap = cursor, times[cursor] - times[cursor - 1]
    k = cursor
    while k < M and times[k] - times[cursor] <= lookahead_s:
        g = times[k] - times[k - 1]
        if g < best_gap:
            best, best_gap = k, g
        k += 1
    return best


def grow_to_target(times, a, rel_target, min_window_s, max_window_s, M):
    """Smallest b>a with dt>=min_window and (gap_a+gap_b)/dt <= rel_target. If the target is
    never met before max_window, return the b that got closest (tightest rel achieved)."""
    gap_a = times[a] - times[a - 1]
    best_b, best_rel = None, float("inf")
    b = a + 1
    while b < M:
        dt = times[b] - times[a]
        if dt > max_window_s:
            break
        if dt >= min_window_s:
            rel = (gap_a + (times[b] - times[b - 1])) / dt
            if rel < best_rel:
                best_rel, best_b = rel, b
            if rel <= rel_target:
                return b
        b += 1
    return best_b


def adaptive_windows(times, charge, volts, rel_target, min_window_s, max_window_s,
                     lookahead_s, vfb):
    """Tile the capture into non-overlapping windows, each grown only until its tightest
    reachable endpoints bring the guaranteed relative bracket under rel_target."""
    M = len(times)
    wins = []
    cursor = 1
    while cursor < M - 1:
        a = best_start(times, cursor, lookahead_s, M)
        b = grow_to_target(times, a, rel_target, min_window_s, max_window_s, M)
        if b is None:
            break
        wins.append(bracket(times, charge, volts, a, b, vfb))
        cursor = b + 1  # non-overlapping -> estimates are independent
    return wins


def naive_windows(times, charge, volts, window_s, vfb):
    """Comparison: fixed-length trailing windows with arbitrary (un-chosen) endpoints,
    sampled at the same count of non-overlapping tiles."""
    M = len(times)
    wins = []
    cursor = 1
    while cursor < M - 1:
        a = cursor
        b = next((k for k in range(a + 1, M) if times[k] - times[a] >= window_s), None)
        if b is None:
            break
        wins.append(bracket(times, charge, volts, a, b, vfb))
        cursor = b + 1
    return wins


# ----------------------------------------------------------------------------------- report

def summarize(wins, label):
    pts = [w["p_pt"] for w in wins]
    n = len(pts)
    mean = statistics.fmean(pts)
    sd = statistics.stdev(pts) if n > 1 else 0.0
    sem = sd / (n ** 0.5) if n else 0.0
    rels = sorted(w["rel"] for w in wins)
    med_rel = rels[len(rels) // 2] if rels else float("nan")
    dts = [w["dt"] for w in wins]
    mean_dt = statistics.fmean(dts) if dts else 0.0
    return dict(label=label, n=n, mean=mean, sd=sd, sem=sem, med_rel=med_rel, mean_dt=mean_dt)


def combined_fit(times, charge, volts, vfb, hac_lag=8):
    """Combine ALL edges into one power estimate by least squares.

    Each tick is an exact charge quantum, so under constant power the true crossing times are
    evenly spaced: t*[i] = t0 + tau * n[i], where n[i] = (charge crossed)/quantum is an EXACT
    integer and tau is seconds-per-tick. We observe t[i] = t*[i] + (detection latency). Fitting
    a line with TIME on the y-axis and tick index on the x-axis (ordinary least squares) is the
    minimum-variance combination of every edge; the slope is tau and  P = 3.6 * V / tau.

    A constant latency offset only shifts the intercept, not the slope, so P is immune to it
    (matches the bracket method's "fixed gauge->EC latency cancels in differences").

    Returns the fit plus two error bars on tau: the classical OLS SE (valid when residuals are
    independent) and a Bartlett-kernel HAC SE (robust to the autocorrelation an EC-clock beat
    would induce). Reports residual RMS and lag-1 autocorrelation so you can see which to trust.
    """
    M = len(times)
    steps = [charge[k] - charge[k - 1] for k in range(1, M)]
    quant = min((abs(s) for s in steps if s != 0), default=0)
    if quant == 0:
        return None
    sign = 1 if charge[-1] >= charge[0] else -1
    raw = [sign * (charge[i] - charge[0]) / quant for i in range(M)]
    n = [round(x) for x in raw]
    nonint = max(abs(raw[i] - n[i]) for i in range(M))

    nb = statistics.fmean(n)
    tb = statistics.fmean(times)
    Sxx = sum((ni - nb) ** 2 for ni in n)
    Sxy = sum((n[i] - nb) * (times[i] - tb) for i in range(M))
    tau = Sxy / Sxx
    intercept = tb - tau * nb
    resid = [times[i] - (intercept + tau * n[i]) for i in range(M)]
    rss = sum(e * e for e in resid)
    s2 = rss / (M - 2) if M > 2 else float("inf")
    se_ols = (s2 / Sxx) ** 0.5

    # HAC (Newey-West, Bartlett weights): robust to residual autocorrelation
    xt = [n[i] - nb for i in range(M)]
    L = max(0, min(hac_lag, M // 4))
    g0 = sum((xt[i] * resid[i]) ** 2 for i in range(M))
    S = g0
    for k in range(1, L + 1):
        wk = 1 - k / (L + 1)
        gk = sum(xt[i] * resid[i] * xt[i - k] * resid[i - k] for i in range(k, M))
        S += 2 * wk * gk
    se_hac = (max(S, 0.0) ** 0.5) / Sxx

    r1 = (sum(resid[i] * resid[i - 1] for i in range(1, M)) / rss) if rss > 0 else 0.0
    v = vmean(volts, 0, M - 1, vfb)
    P = 3.6 * v / tau
    return dict(M=M, quant=quant, tau=tau, v=v, P=P,
                P_se_ols=P * se_ols / tau, P_se_hac=P * se_hac / tau,
                resid_rms=(rss / M) ** 0.5, r1=r1, nonint=nonint, L=L)


def drift_check(times, charge):
    """Can a single constant-power line thread every monotonic-counter bracket
    (t[i-1], t[i]]?  Feasible tau range comes from the pairwise constraints
        i>j: tau >= (t[i-1]-t[j])/(n_i-n_j) ,  i<j: tau <= (t[j]-t[i-1])/(n_j-n_i).
    If the range is empty, power was NOT perfectly constant to within tick quantization
    -- a real (if mild) drift. O(M^2); skipped on very large captures."""
    M = len(times)
    if M > 3000:
        return None
    steps = [charge[k] - charge[k - 1] for k in range(1, M)]
    quant = min((abs(s) for s in steps if s != 0), default=0)
    if quant == 0:
        return None
    sign = 1 if charge[-1] >= charge[0] else -1
    n = [round(sign * (charge[i] - charge[0]) / quant) for i in range(M)]
    lo, hi = -float("inf"), float("inf")
    for i in range(1, M):
        for j in range(M):
            dn = n[i] - n[j]
            if dn > 0:
                lo = max(lo, (times[i - 1] - times[j]) / dn)
            elif dn < 0:
                hi = min(hi, (times[j] - times[i - 1]) / (-dn))
    return dict(lo=lo, hi=hi, feasible=lo < hi)


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    src = ap.add_mutually_exclusive_group()
    src.add_argument("--csv", help="analyse an existing charge-edge CSV instead of capturing")
    src.add_argument("--secs", type=float, help="capture live for this many seconds (root)")
    ap.add_argument("--poll-ms", type=float, default=5.0, help="live poll interval (default 5)")
    ap.add_argument("--cache-ms", type=int, default=0, help="cache_time during capture (default 0)")
    ap.add_argument("--rel-target", type=float, default=0.03,
                    help="target guaranteed relative bracket per window (default 0.03 = +/-3%%)")
    ap.add_argument("--min-window", type=float, default=5.0,
                    help="minimum window length before a window may close (default 5s)")
    ap.add_argument("--max-window", type=float, default=60.0,
                    help="cap on window growth if target unreachable (default 60s)")
    ap.add_argument("--lookahead", type=float, default=2.0,
                    help="search span for the tightest start endpoint (default 2s)")
    ap.add_argument("--voltage", type=float, default=16.5,
                    help="fallback voltage if CSV has no voltage_V column")
    ap.add_argument("--verbose", action="store_true", help="print every adaptive window")
    args = ap.parse_args()

    live = False
    if args.csv:
        times, charge, volts = load_csv(args.csv)
    elif args.secs:
        live = True
        times, charge, volts = capture_live(args.secs, args.poll_ms, args.cache_ms)
        # preserve the raw capture so it can be re-analysed at other targets without recapturing
        write_csv(RAW_CSV, "edge,t_s,charge_uah,voltage_V\n",
                  [f"{i},{times[i]:.4f},{charge[i]},"
                   f"{'' if volts[i] is None else f'{volts[i]:.4f}'}\n" for i in range(len(times))])
        print(f"\nRaw edges saved: {RAW_CSV}")
    else:
        # default: analyse the standard dump location if present, else ask for input
        default = "/tmp/charge-edges.csv"
        if os.path.exists(default):
            print(f"(no --csv/--secs given; analysing {default})\n")
            times, charge, volts = load_csv(default)
        else:
            ap.error("give --secs N to capture (root) or --csv PATH to analyse")

    M = len(times)
    if M < 6:
        sys.exit(f"Only {M} edges -- need more for windowed statistics. "
                 f"Capture longer (--secs) -- a 1mAh tick takes a few seconds on battery.")

    diffs = [charge[k + 1] - charge[k] for k in range(M - 1)]
    if any(d > 0 for d in diffs) and any(d < 0 for d in diffs):
        print("WARNING: charge direction flips in this capture; abs(dQ) under-counts "
              "across the flip -- keep load steady (pure charge OR pure discharge).\n")

    has_v = any(v is not None for v in volts)
    vsrc = "measured" if (live and has_v) else ("from CSV" if has_v else f"fixed {args.voltage}V")
    gaps = sorted(times[k] - times[k - 1] for k in range(1, M))
    print(f"{M} edges over {times[-1] - times[0]:.1f}s. "
          f"endpoint gaps: min={gaps[0]:.3f} med={gaps[len(gaps)//2]:.3f} max={gaps[-1]:.3f}s. "
          f"voltage {vsrc}.")
    print(f"target guaranteed bracket per window: +/-{args.rel_target*100:.1f}%  "
          f"(min-window {args.min_window:.0f}s, cap {args.max_window:.0f}s)\n")

    wins = adaptive_windows(times, charge, volts, args.rel_target, args.min_window,
                            args.max_window, args.lookahead, args.voltage)
    if not wins:
        sys.exit("No windows met the constraints -- capture longer or relax --rel-target.")

    if args.verbose:
        print(f"{'#':>3}  {'a..b':>9}  {'dt':>6}  {'gapA+gapB':>9}  "
              f"{'P_min':>7}  {'P_pt':>7}  {'P_max':>7}  {'rel':>6}")
        for i, w in enumerate(wins):
            print(f"{i:>3}  {w['a']:>3}..{w['b']:<4}  {w['dt']:>5.1f}s  "
                  f"{w['gap_a']+w['gap_b']:>8.2f}s  {w['p_min']:>6.2f}W  {w['p_pt']:>6.2f}W  "
                  f"{w['p_max']:>6.2f}W  {w['rel']*100:>5.1f}%")
        print()

    a = summarize(wins, "adaptive")

    # comparison: fixed window at the SAME mean length, endpoints not chosen
    nwins = naive_windows(times, charge, volts, a["mean_dt"], args.voltage)
    nv = summarize(nwins, "fixed") if nwins else None

    # write per-window CSV (robust to stale foreign-owned files under sudo)
    write_csv(OUT_CSV,
              "idx,a,b,dt_s,gap_a_s,gap_b_s,dQ_uAh,voltage_V,P_min_W,P_point_W,P_max_W,rel_pct\n",
              [f"{i},{w['a']},{w['b']},{w['dt']:.4f},{w['gap_a']:.4f},{w['gap_b']:.4f},"
               f"{w['dq']},{w['v']:.4f},{w['p_min']:.4f},{w['p_pt']:.4f},"
               f"{w['p_max']:.4f},{w['rel']*100:.3f}\n" for i, w in enumerate(wins)])

    # --- headline: least-squares fit over ALL edges (the efficient combination) ---
    fit = combined_fit(times, charge, volts, args.voltage)
    # guaranteed bound on the AVERAGE power over the whole capture (no constancy assumption):
    # the full-span endpoint bracket -- this is what holds unconditionally.
    full = bracket(times, charge, volts, 1, M - 1, args.voltage)
    drift = drift_check(times, charge)

    print("=" * 72)
    if fit:
        se = max(fit["P_se_ols"], fit["P_se_hac"])  # report the more conservative
        print(f"COMBINED FIT (least squares over all {fit['M']} edges; time on y, tick index on x)")
        print(f"\n  Power = {fit['P']:.3f} W")
        print(f"    statistical:  +/-{se:.3f} W ({100*se/fit['P']:.2f}%)   "
              f"[OLS +/-{fit['P_se_ols']:.3f}, HAC +/-{fit['P_se_hac']:.3f}]")
        print(f"    95% CI:       [{fit['P']-1.96*se:.3f}, {fit['P']+1.96*se:.3f}] W")
        print(f"    diagnostics:  tau={fit['tau']:.4f}s/{fit['quant']/1000:.0f}mAh-tick, "
              f"resid RMS={fit['resid_rms']:.3f}s, lag-1 autocorr={fit['r1']:+.2f} "
              f"({'~iid, OLS=HAC' if abs(fit['r1'])<0.2 else 'autocorr present, HAC-corrected'})")
        print(f"    NB: this bar is timing scatter only; the guaranteed bracket below bounds "
              f"the true average.")
    print(f"\n  guaranteed:   [{full['p_min']:.3f}, {full['p_max']:.3f}] W "
          f"(hard bound on the AVERAGE over the full {full['dt']:.0f}s, assumes nothing)")
    if drift is not None:
        if drift["feasible"]:
            print(f"  drift check:  a single constant-power line fits all brackets "
                  f"(tau in [{drift['lo']:.4f}, {drift['hi']:.4f}]) -> power was steady.")
        else:
            print(f"  drift check:  no constant-power line threads every tick bracket "
                  f"(by {1000*(drift['lo']-drift['hi']):.0f}ms) -> real power drifted slightly; "
                  f"the fit gives the time-average.")

    # cross-check: the windowed estimators (kept for comparison / per-window CSV)
    print(f"\n  cross-check (windowed): adaptive {a['n']} tiles -> {a['mean']:.3f} "
          f"+/-{a['sd']:.3f} W", end="")
    if nv:
        print(f" ;  fixed {nv['n']} tiles -> {nv['mean']:.3f} +/-{nv['sd']:.3f} W", end="")
    print()
    print("=" * 72)
    print(f"\nPer-window CSV: {OUT_CSV}")


if __name__ == "__main__":
    main()
