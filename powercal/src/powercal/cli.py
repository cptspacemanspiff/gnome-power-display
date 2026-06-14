"""Command-line entry point for powercal.

    powercal analyze edges.csv            # estimate from a captured CSV
    powercal measure --target 0.1         # capture live until error <= 0.1 W (needs root)
    powercal list   scenarios/            # show scenarios defined in a folder
    powercal run    scenarios/            # run a scenario batch (needs root)
"""

from __future__ import annotations

import argparse
import sys

from .measurements import BAT_DEFAULT, calculate_power, load_edges_csv, measure_power
from .operations import Runner, compose, inhibit_sleep, keep_active, load_batch, load_scenarios, select


def _keep_awake():
    """Start the universal keep-awake guard (no suspend + jiggle so no blank/lock), for any DE.

    Returns a teardown callable. On failure (no /dev/uinput access, no systemd) it warns and
    returns a no-op rather than aborting the measurement.
    """
    try:
        return compose(inhibit_sleep(), keep_active())()
    except (RuntimeError, OSError) as e:
        print(f"warning: keep-awake unavailable ({e}); display/suspend NOT inhibited",
              file=sys.stderr, flush=True)
        return lambda: None


class _Activity:
    """A live, single-line progress indicator fed by ``measure_power``'s progress callback.

    On a TTY it redraws one animated line (spinner + elapsed + edges + current estimate vs
    target); when piped it falls back to one appended log line per update. Activity goes to
    stderr so stdout stays clean for the final result.
    """

    _SPIN = "|/-\\"

    def __init__(self, target_w: float, sigma: float = 2.0, stream=sys.stderr) -> None:
        self.target = target_w
        self.sigma = sigma
        self.stream = stream
        self.tty = getattr(stream, "isatty", lambda: False)()
        self._i = 0

    def __call__(self, n_edges: int, elapsed: float, est) -> None:
        ch = self._SPIN[self._i % len(self._SPIN)]
        self._i += 1
        if est is None:
            body = f"{elapsed:6.1f}s  {n_edges:3d} edges  warming up..."
        else:
            band = self.sigma * max(est.std_w, est.std_robust_w)
            mark = "target met" if band <= self.target else f"need <= {self.target:g} W"
            body = (f"{elapsed:6.1f}s  {n_edges:3d} edges  {est.power_w:8.3f} W"
                    f"  +/-{band:.4f} W ({self.sigma:g} sigma)  [{mark}]")
        line = f"  {ch} {body}"
        if self.tty:
            print(f"\r\033[K{line}", end="", file=self.stream, flush=True)
        else:
            print(line, file=self.stream, flush=True)

    def done(self) -> None:
        if self.tty:
            print(file=self.stream)  # end the live line with a newline


def _print(est, sigma: float = 2.0) -> None:
    band = sigma * est.std_w
    print(f"{est.power_w:.3f} W  +/-{band:.3f} W ({sigma:g} sigma)  "
          f"guaranteed [{est.lower_w:.3f}, {est.upper_w:.3f}] W")
    print(
        f"  edges={est.n_edges}  span={est.span_s:.1f}s  tau={est.tau_s:.4f}s  "
        f"quantum={est.quantum_uah:.0f}uAh  V={est.voltage_v:.3f}"
    )
    print(
        f"  std(1 sigma)={est.std_w:.4f}W  std_robust(1 sigma)={est.std_robust_w:.4f}W  "
        f"autocorr_lag1={est.autocorr_lag1:+.2f}  bracket={est.bracket_w:.3f}W  "
        f"constant_power_feasible={est.constant_power_feasible}"
    )


def _analyze(args) -> int:
    times, charge, volts = load_edges_csv(args.csv)
    est = calculate_power(
        times, charge, volts,
        voltage_fallback=args.voltage_fallback, hac_lag=args.hac_lag,
    )
    _print(est, args.sigma)
    return 0


def _measure(args) -> int:
    print(f"measuring: target <= {args.target:g} W ({args.sigma:g} sigma), poll {args.poll_ms:g} ms, "
          f"max {args.max_secs:g}s, battery {args.bat}", file=sys.stderr, flush=True)
    release = _keep_awake()
    activity = _Activity(args.target, args.sigma)
    try:
        est = measure_power(
            args.target,
            bat=args.bat,
            poll_ms=args.poll_ms,
            voltage_fallback=args.voltage_fallback,
            hac_lag=args.hac_lag,
            max_secs=args.max_secs,
            sigma=args.sigma,
            progress=activity,
        )
    finally:
        activity.done()
        release()
    print("---")
    _print(est, args.sigma)
    return 0


def _list(args) -> int:
    scenarios = load_scenarios(args.path, recursive=args.recursive)
    for s in scenarios:
        extra = []
        if s.settle_s is not None:
            extra.append(f"settle={s.settle_s:g}s")
        if s.target_w is not None:
            extra.append(f"target={s.target_w:g}W")
        print(f"{s.name}" + (f"  ({', '.join(extra)})" if extra else ""))
    print(f"\n{len(scenarios)} scenario(s) in {args.path}")
    return 0


def _run(args) -> int:
    batch = load_batch(args.path, recursive=args.recursive)
    scenarios = select(batch.scenarios, args.only)

    activity = {"cur": None}

    def on_event(kind, scenario, data):
        if kind == "prepare":
            print("preparing shared baseline (once for the batch) ...", flush=True)
        elif kind == "start":
            print(f"\n[{scenario.name}]", flush=True)
            target = scenario.target_w if scenario.target_w is not None else args.target
            activity["cur"] = _Activity(target, args.sigma)
        elif kind == "settle":
            print(f"  settling {data['seconds']:g}s ...", flush=True)
        elif kind == "progress" and activity["cur"] is not None:
            activity["cur"](data["n_edges"], data["elapsed"], data["est"])
        elif kind in ("done", "error"):
            if activity["cur"] is not None:
                activity["cur"].done()
                activity["cur"] = None
            if kind == "error":
                print(f"  ERROR: {data['error']}", flush=True)

    runner = Runner(
        target_w=args.target, max_secs=args.max_secs, settle_s=args.settle,
        sigma=args.sigma, bat=args.bat, on_event=on_event,
    )
    release = _keep_awake()
    try:
        results = runner.run(scenarios, prepare=batch.prepare)
    finally:
        release()

    print(f"\n=== results ({args.sigma:g} sigma) ===")
    for r in results:
        if r.ok:
            band = args.sigma * r.estimate.std_w
            print(f"{r.scenario.name:24s} {r.estimate.power_w:.3f} W  +/-{band:.3f} W"
                  f"  guaranteed [{r.estimate.lower_w:.3f}, {r.estimate.upper_w:.3f}]")
        else:
            print(f"{r.scenario.name:24s} FAILED: {r.error}")
    return 0 if all(r.ok for r in results) else 1


def main(argv=None) -> int:
    p = argparse.ArgumentParser(prog="powercal", description=__doc__,
                                formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--voltage-fallback", type=float, default=16.5,
                   help="voltage (V) used when the source has none (default: 16.5)")
    p.add_argument("--hac-lag", type=int, default=8,
                   help="Bartlett-HAC truncation lag for the robust error (default: 8)")
    sub = p.add_subparsers(dest="cmd", required=True)

    # shared by commands that report an error band
    sigma_opt = argparse.ArgumentParser(add_help=False)
    sigma_opt.add_argument("--sigma", type=float, default=2.0,
                           help="confidence multiplier for reported/target error bands "
                                "(default: 2, ~95 percent; use 1 for ~68 percent)")

    a = sub.add_parser("analyze", parents=[sigma_opt],
                       help="estimate power from a captured edges CSV")
    a.add_argument("csv", help="CSV with t_s/edge_t_s, charge_uah, optional voltage_V columns")
    a.set_defaults(func=_analyze)

    m = sub.add_parser("measure", parents=[sigma_opt],
                       help="capture live until the error bar is small enough (root)")
    m.add_argument("--target", type=float, default=0.1,
                   help="stop when the sigma-scaled error bar <= this many watts (default: 0.1)")
    m.add_argument("--bat", default=BAT_DEFAULT, help=f"battery sysfs dir (default: {BAT_DEFAULT})")
    m.add_argument("--poll-ms", type=float, default=5.0, help="poll interval in ms (default: 5)")
    m.add_argument("--max-secs", type=float, default=600.0,
                   help="hard cap; return best estimate if target not met (default: 600)")
    m.set_defaults(func=_measure)

    ls = sub.add_parser("list", help="list scenarios defined in a folder")
    ls.add_argument("path", help="folder (or single .py) of scenario definitions")
    ls.add_argument("-r", "--recursive", action="store_true", help="recurse into subfolders")
    ls.set_defaults(func=_list)

    r = sub.add_parser("run", parents=[sigma_opt],
                       help="run a scenario batch from a folder (root)")
    r.add_argument("path", help="folder (or single .py) of scenario definitions")
    r.add_argument("--only", action="append", metavar="NAME",
                   help="run only this scenario (repeatable); default: all")
    r.add_argument("-r", "--recursive", action="store_true", help="recurse into subfolders")
    r.add_argument("--target", type=float, default=0.1,
                   help="default error target in watts when a scenario sets none (default: 0.1)")
    r.add_argument("--settle", type=float, default=0.0,
                   help="default settle seconds when a scenario sets none (default: 0)")
    r.add_argument("--max-secs", type=float, default=300.0,
                   help="default per-scenario hard cap in seconds (default: 300)")
    r.add_argument("--bat", default=BAT_DEFAULT, help=f"battery sysfs dir (default: {BAT_DEFAULT})")
    r.set_defaults(func=_run)

    args = p.parse_args(argv)
    try:
        return args.func(args)
    except (PermissionError, ValueError, FileNotFoundError, KeyError) as e:
        print(f"error: {e}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
