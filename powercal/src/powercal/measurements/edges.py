"""Load or capture battery charge-counter edges for :func:`powercal.calculate_power`.

An "edge" is a single change in `charge_now`. These helpers produce three parallel sequences
(times, charge_uah, voltage) suitable for passing straight into ``calculate_power``.
"""

from __future__ import annotations

import csv as _csv
import os
import time
from typing import List, Optional, Tuple

BAT_DEFAULT = "/sys/class/power_supply/BAT1"
CACHE_PARAM = "/sys/module/battery/parameters/cache_time"

Edges = Tuple[List[float], List[int], List[Optional[float]]]


def load_edges_csv(path: str) -> Edges:
    """Read edges from a CSV written by dump-charge-edges.py / sample-charge.py.

    Recognises a time column named ``t_s`` or ``edge_t_s``, a ``charge_uah`` column, and an
    optional ``voltage_V`` column. Returns (times, charge_uah, voltage).
    """
    with open(path) as f:
        rows = list(_csv.DictReader(f))
    if not rows:
        raise ValueError(f"{path} is empty")
    tkey = "t_s" if "t_s" in rows[0] else ("edge_t_s" if "edge_t_s" in rows[0] else None)
    if tkey is None:
        raise ValueError(f"no t_s/edge_t_s column in {path}; have {list(rows[0])}")
    vkey = "voltage_V" if "voltage_V" in rows[0] else None
    times: List[float] = []
    charge: List[int] = []
    volts: List[Optional[float]] = []
    for r in rows:
        times.append(float(r[tkey]))
        charge.append(int(r["charge_uah"]))
        volts.append(float(r[vkey]) if vkey and r.get(vkey) else None)
    return times, charge, volts


def _read_int(p: str) -> int:
    with open(p) as f:
        return int(f.read().strip())


def capture_edges(
    secs: float,
    *,
    bat: str = BAT_DEFAULT,
    poll_ms: float = 5.0,
    cache_ms: int = 0,
    read_voltage: bool = True,
    progress=None,
) -> Edges:
    """Capture charge edges live (requires root to lower the driver cache).

    Lowers ``cache_time`` so ``charge_now`` changes at the EC's true crossing times, seeds a
    baseline sample at t=0 (so the first real edge already has a preceding interval for its
    bracket), and records every change for ``secs`` seconds. Restores ``cache_time`` on the way
    out. ``progress`` if given is called as ``progress(index, t, charge, gap)`` per edge.

    Returns (times, charge_uah, voltage). Voltage[i] is the mean uncached voltage over the
    interval ending at edge i (None for the seed).
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
        while time.monotonic() - t0 < secs:
            time.sleep(poll)
            now = time.monotonic()
            c = _read_int(f"{bat}/charge_now")
            if read_voltage:
                seg_v_sum += _read_int(f"{bat}/voltage_now") / 1e6
                seg_v_n += 1
            if c == pc:
                continue
            t = now - t0
            times.append(t)
            charge.append(c)
            volts.append(seg_v_sum / seg_v_n if seg_v_n else None)
            if progress is not None:
                progress(len(times) - 1, t, c, times[-1] - times[-2])
            pc = c
            seg_v_sum, seg_v_n = 0.0, 0
        return times, charge, volts
    finally:
        with open(CACHE_PARAM, "w") as f:
            f.write(str(orig))
