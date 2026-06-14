"""Average-power estimation from a monotonic battery charge counter.

The battery reports `charge_now` as a coulomb counter that steps by an exact charge quantum
(the LSB, ~1 mAh on tested hardware). Each step ("edge") is observed at some time t[i], a few
hundred ms after the true crossing because of EC poll / driver-cache latency. Per-edge power
(dQ/dt on single ticks) is garbage, but the counter is *monotonic*, which gives two clean
estimators of the average power over a capture:

1. LEAST-SQUARES FIT (the estimate).  Under constant power the true crossing times are evenly
   spaced in charge, hence in time:  t*[i] = t0 + tau * n[i],  where n[i] = (charge crossed) /
   quantum is an EXACT integer tick index and tau is seconds-per-tick. Fit a line with TIME on
   the y-axis and tick index on the x-axis (OLS) -> slope tau ->

       P = quantum_uAh * V * 3.6e-3 / tau        (1 uAh = 3.6e-3 C, so uAh*V*3.6e-3 = joules)

   This folds in every edge (minimum-variance), and a constant EC->gauge latency only shifts
   the intercept, not the slope, so P is immune to it. The slope's standard error is the
   STATISTICAL error bar; we also compute a Bartlett-HAC variant robust to residual
   autocorrelation (an EC-clock beat). Averaging per-tick dt's instead would telescope to the
   two endpoints and gain nothing -- the fit is what uses the interior.

2. GUARANTEED BRACKET (the outer bounds).  The true crossing time of the tick reported at t[i]
   is provably in (t[i-1], t[i]]. Over a window the exact charge dQ divided by the largest /
   smallest feasible elapsed time gives a HARD [lower, upper] envelope on the average power --
   no constant-power assumption, no EC-clock model. The window's timing slop lives only at its
   two endpoints (gap_a + gap_b), so the bracket tightens ~1/span.

`calculate_power` returns both: the fit with its statistical std error, and the guaranteed
outer bounds. See BRACKET-METHOD.md in this repo for the full derivation.
"""

from __future__ import annotations

import math
from dataclasses import dataclass
from typing import Optional, Sequence, Union

# 1 uAh = 3.6e-3 coulomb; energy[J] = charge[uAh] * 3.6e-3 * V; power[W] = energy / seconds.
J_PER_UAH_VOLT = 3.6e-3

VoltageArg = Union[None, float, Sequence[Optional[float]]]


@dataclass(frozen=True)
class PowerEstimate:
    """Result of :func:`calculate_power`. The three headline fields are what calibration wants."""

    # --- headline ---
    power_w: float          # least-squares best estimate of the average power
    std_w: float            # 1-sigma statistical standard error on power_w (OLS slope SE)
    lower_w: float          # GUARANTEED lower bound on the true average power
    upper_w: float          # GUARANTEED upper bound on the true average power

    # --- supporting detail ---
    std_robust_w: float     # autocorrelation-robust (Bartlett-HAC) standard error
    autocorr_lag1: float    # lag-1 residual autocorrelation; |.|<~0.2 => std_w is trustworthy
    tau_s: float            # fitted seconds per charge tick
    quantum_uah: float      # charge LSB used as the tick size
    voltage_v: float        # voltage used in the energy integral
    n_edges: int            # number of edges used in the fit
    span_s: float           # capture span (last edge - first edge)
    resid_rms_s: float      # RMS of edge-time residuals about the fit line
    constant_power_feasible: Optional[bool]  # can one constant-power line thread every tick
                                             # bracket? False => real power drift; None => not checked

    @property
    def rel_std(self) -> float:
        """Statistical std error as a fraction of the estimate."""
        return self.std_w / self.power_w if self.power_w else float("nan")

    @property
    def bracket_w(self) -> float:
        """Width of the guaranteed outer bounds."""
        return self.upper_w - self.lower_w

    def __str__(self) -> str:
        return (f"{self.power_w:.3f} W  +/-{self.std_w:.3f} W (1 sigma)  "
                f"guaranteed [{self.lower_w:.3f}, {self.upper_w:.3f}] W")


def _mean_voltage(voltage: VoltageArg, fallback: float, n: int) -> float:
    if voltage is None:
        return float(fallback)
    if isinstance(voltage, (int, float)):
        return float(voltage)
    seg = [float(v) for v in voltage if v is not None]
    if not seg:
        return float(fallback)
    return sum(seg) / len(seg)


def _tick_indices(charge: Sequence[float]) -> tuple[list[int], float, int]:
    """Map each edge to an exact integer tick index. Returns (n, quantum_uah, direction)."""
    m = len(charge)
    steps = [charge[k] - charge[k - 1] for k in range(1, m)]
    quantum = min((abs(s) for s in steps if s != 0), default=0.0)
    if quantum == 0:
        raise ValueError("charge never changes -- no edges to fit")
    ups = sum(1 for s in steps if s > 0)
    downs = sum(1 for s in steps if s < 0)
    if ups and downs:
        raise ValueError("charge direction flips (mixed charge/discharge); "
                         "split the capture into single-direction segments")
    sign = 1 if charge[-1] >= charge[0] else -1
    n = [round(sign * (charge[i] - charge[0]) / quantum) for i in range(m)]
    return n, float(quantum), sign


def _guaranteed_bracket(times, charge, v) -> tuple[float, float]:
    """Hard [lower, upper] bound on the AVERAGE power over the largest fully-bracketed window
    (second edge .. last edge), from the crossing-time brackets t*[k] in (t[k-1], t[k]].
    Assumes nothing about power being constant."""
    m = len(times)
    a, b = 1, m - 1
    dq = abs(charge[b] - charge[a])
    dt_obs = times[b] - times[a]
    gap_a = times[a] - times[a - 1]
    gap_b = times[b] - times[b - 1]
    dt_min = dt_obs - gap_b      # b earliest, a latest  -> shortest time -> highest power
    dt_max = dt_obs + gap_a      # b latest,  a earliest -> longest time  -> lowest power
    upper = dq * v * J_PER_UAH_VOLT / dt_min if dt_min > 0 else float("inf")
    lower = dq * v * J_PER_UAH_VOLT / dt_max if dt_max > 0 else 0.0
    return lower, upper


def _constant_power_feasible(times, n, max_edges=3000) -> Optional[bool]:
    """Does a single constant-power line t0 + tau*n thread every bracket (t[i-1], t[i]]?
    Feasible tau interval from the pairwise constraints; empty => real drift. O(M^2)."""
    m = len(times)
    if m > max_edges:
        return None
    lo, hi = -math.inf, math.inf
    for i in range(1, m):
        ti1 = times[i - 1]
        ni = n[i]
        for j in range(m):
            dn = ni - n[j]
            if dn > 0:
                lo = max(lo, (ti1 - times[j]) / dn)
            elif dn < 0:
                hi = min(hi, (times[j] - ti1) / (-dn))
    return lo < hi


def calculate_power(
    times: Sequence[float],
    charge_uah: Sequence[float],
    voltage: VoltageArg = None,
    *,
    voltage_fallback: float = 16.5,
    hac_lag: int = 8,
) -> PowerEstimate:
    """Estimate average power from charge-counter edges.

    Args:
        times: edge timestamps in seconds (monotonically increasing).
        charge_uah: `charge_now` value (uAh) at each edge; same length as ``times``. Must move
            in a single direction (pure charge or pure discharge).
        voltage: per-edge voltages (V), a single scalar voltage, or None. A list is averaged.
        voltage_fallback: voltage used when ``voltage`` is None or empty.
        hac_lag: Bartlett-kernel truncation lag for the robust standard error.

    Returns:
        PowerEstimate with the least-squares ``power_w``, its statistical ``std_w`` (and
        autocorrelation-robust ``std_robust_w``), and the guaranteed outer bounds
        ``lower_w`` / ``upper_w``.

    Raises:
        ValueError: fewer than 4 edges, no charge change, or mixed charge/discharge.
    """
    times = [float(t) for t in times]
    charge = [float(c) for c in charge_uah]
    m = len(times)
    if m != len(charge):
        raise ValueError("times and charge_uah must have equal length")
    if m < 4:
        raise ValueError(f"need at least 4 edges, got {m}")

    n, quantum, _ = _tick_indices(charge)
    v = _mean_voltage(voltage, voltage_fallback, m)

    # --- OLS: time (y) vs tick index (x) ---
    nb = sum(n) / m
    tb = sum(times) / m
    sxx = sum((ni - nb) ** 2 for ni in n)
    if sxx == 0:
        raise ValueError("all edges share one tick index -- cannot fit")
    sxy = sum((n[i] - nb) * (times[i] - tb) for i in range(m))
    tau = sxy / sxx
    if tau <= 0:
        raise ValueError("non-positive seconds-per-tick -- check that times increase with charge")
    intercept = tb - tau * nb
    resid = [times[i] - (intercept + tau * n[i]) for i in range(m)]
    rss = sum(e * e for e in resid)
    s2 = rss / (m - 2) if m > 2 else float("inf")
    se_ols = math.sqrt(s2 / sxx)

    # --- HAC (Newey-West, Bartlett weights): robust to residual autocorrelation ---
    xt = [n[i] - nb for i in range(m)]
    lag = max(0, min(hac_lag, m // 4))
    meat = sum((xt[i] * resid[i]) ** 2 for i in range(m))
    for k in range(1, lag + 1):
        wk = 1 - k / (lag + 1)
        gk = sum(xt[i] * resid[i] * xt[i - k] * resid[i - k] for i in range(k, m))
        meat += 2 * wk * gk
    se_hac = math.sqrt(max(meat, 0.0)) / sxx

    r1 = (sum(resid[i] * resid[i - 1] for i in range(1, m)) / rss) if rss > 0 else 0.0

    power = quantum * v * J_PER_UAH_VOLT / tau
    rel_ols = se_ols / tau
    rel_hac = se_hac / tau

    lower, upper = _guaranteed_bracket(times, charge, v)
    feasible = _constant_power_feasible(times, n)

    return PowerEstimate(
        power_w=power,
        std_w=power * rel_ols,
        lower_w=lower,
        upper_w=upper,
        std_robust_w=power * rel_hac,
        autocorr_lag1=r1,
        tau_s=tau,
        quantum_uah=quantum,
        voltage_v=v,
        n_edges=m,
        span_s=times[-1] - times[0],
        resid_rms_s=math.sqrt(rss / m),
        constant_power_feasible=feasible,
    )
