"""Capture charge edges until the power estimate's error bar drops below a target.

`calculate_power`'s statistical error shrinks like ~1/T^1.5 with capture time (Sxx grows ~N^3
while the residual scatter stays put), so rather than guess a fixed duration we capture
incrementally, re-fit every few edges, and stop as soon as the error is small enough. This is
the entry point calibration should use when it has a hard accuracy requirement (e.g. <0.1 W).
"""

from __future__ import annotations

import os
import time
from typing import List, Optional

from .edges import BAT_DEFAULT, CACHE_PARAM, _read_int
from .power import PowerEstimate, calculate_power


def measure_power(
    target_w: float,
    *,
    bat: str = BAT_DEFAULT,
    poll_ms: float = 5.0,
    cache_ms: int = 0,
    voltage_fallback: float = 16.5,
    hac_lag: int = 8,
    min_edges: int = 8,
    min_secs: float = 5.0,
    max_secs: float = 600.0,
    recompute_every: int = 4,
    conservative: bool = True,
    sigma: float = 2.0,
    heartbeat_s: float = 1.0,
    progress=None,
) -> PowerEstimate:
    """Capture live until the power error bar is <= ``target_w``, then return the estimate.

    Polls ``charge_now`` every ``poll_ms`` (root required, to lower the driver cache so edges
    land at the EC's true crossing times). After ``min_edges`` edges and ``min_secs`` seconds it
    re-fits every ``recompute_every`` edges and stops when the ``sigma``-scaled error bar falls to
    ``target_w``. With the default ``sigma=2.0`` the target is a ~95% band: stopping at
    ``target_w=0.1`` means the 2-sigma error is <= 0.1 W (the 1-sigma std error is <= 0.05 W).

    The stopping error is ``max(std_w, std_robust_w)`` when ``conservative`` (default) -- so a
    hidden EC-clock beat (autocorrelation) can't let you stop early -- otherwise just ``std_w``.

    Stops at ``max_secs`` regardless; if the target wasn't met by then the returned estimate's
    ``std_w`` tells you how close you got.

    ``progress`` if given is called ``progress(n_edges, elapsed_s, est)`` for activity reporting:
    on every new edge, and at least every ``heartbeat_s`` seconds even during quiet stretches (so
    a caller can animate a spinner). ``est`` is the most recent fit, or None before the first one.

    Returns the final :class:`PowerEstimate` (fit over every captured edge).
    """
    if not os.access(CACHE_PARAM, os.W_OK):
        raise PermissionError(f"cannot write {CACHE_PARAM} -- run as root")

    orig = _read_int(CACHE_PARAM)
    with open(CACHE_PARAM, "w") as f:
        f.write(str(cache_ms))
    try:
        poll = poll_ms / 1000.0
        times: List[float] = [0.0]
        charge: List[int] = [_read_int(f"{bat}/charge_now")]
        volts: List[Optional[float]] = [None]
        seg_v_sum, seg_v_n = 0.0, 0
        t0 = time.monotonic()
        pc = charge[0]
        last_fit_edges = 0
        est: Optional[PowerEstimate] = None
        last_emit = -1e9

        def emit(elapsed: float) -> None:
            nonlocal last_emit
            last_emit = elapsed
            if progress is not None:
                progress(len(times) - 1, elapsed, est)

        while True:
            time.sleep(poll)
            now = time.monotonic()
            elapsed = now - t0
            c = _read_int(f"{bat}/charge_now")
            seg_v_sum += _read_int(f"{bat}/voltage_now") / 1e6
            seg_v_n += 1

            new_edge = c != pc
            if new_edge:
                times.append(elapsed)
                charge.append(c)
                volts.append(seg_v_sum / seg_v_n if seg_v_n else None)
                pc = c
                seg_v_sum, seg_v_n = 0.0, 0

                n_edges = len(times) - 1  # excluding the seed baseline
                ready = n_edges >= min_edges and elapsed >= min_secs
                due = n_edges - last_fit_edges >= recompute_every
                if ready and due:
                    last_fit_edges = n_edges
                    try:
                        est = calculate_power(
                            times, charge, volts,
                            voltage_fallback=voltage_fallback, hac_lag=hac_lag,
                        )
                    except ValueError:
                        est = None
                    if est is not None:
                        err = max(est.std_w, est.std_robust_w) if conservative else est.std_w
                        if sigma * err <= target_w:
                            emit(elapsed)
                            return est

            # activity reporting: every edge, plus a heartbeat during quiet stretches
            if new_edge or (elapsed - last_emit) >= heartbeat_s:
                emit(elapsed)

            if elapsed >= max_secs:
                if est is None or last_fit_edges != len(times) - 1:
                    est = calculate_power(
                        times, charge, volts,
                        voltage_fallback=voltage_fallback, hac_lag=hac_lag,
                    )
                emit(elapsed)
                return est
    finally:
        with open(CACHE_PARAM, "w") as f:
            f.write(str(orig))
