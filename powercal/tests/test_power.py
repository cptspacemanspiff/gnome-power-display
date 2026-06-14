"""Tests for powercal.calculate_power (run: uv run pytest)."""

import math

from powercal import calculate_power
from powercal.measurements import J_PER_UAH_VOLT


def _synth(power_w, *, v=16.5, quantum=1000, m=120, latency_frac=0.3):
    """Synthesise constant-power discharge edges with a deterministic, one-sided latency.

    True crossing of tick i is at i*tau; the observed time adds a latency in [0, latency_frac*tau)
    so the monotonic-counter bracket (t[i-1], t[i]] still contains the true crossing -- letting us
    assert the guaranteed bounds really do bracket the truth.
    """
    tau = quantum * v * J_PER_UAH_VOLT / power_w           # seconds per tick
    times, charge = [], []
    for i in range(m):
        latency = latency_frac * tau * 0.5 * (1 + math.sin(i * 1.3))  # in [0, latency_frac*tau)
        times.append(i * tau + latency)
        charge.append(1_000_000 - i * quantum)             # discharge
    return times, charge, v, tau


def test_recovers_constant_power():
    times, charge, v, _ = _synth(41.0)
    est = calculate_power(times, charge, v)
    assert abs(est.power_w - 41.0) < 0.5, est.power_w           # within ~1%
    assert est.std_w > 0
    assert est.rel_std < 0.02                                   # tight from 120 edges


def test_guaranteed_bounds_contain_truth():
    times, charge, v, _ = _synth(41.0)
    est = calculate_power(times, charge, v)
    assert est.lower_w <= 41.0 <= est.upper_w
    assert est.lower_w < est.power_w < est.upper_w
    assert est.bracket_w > 0


def test_voltage_forms_agree():
    times, charge, v, _ = _synth(30.0)
    scalar = calculate_power(times, charge, v).power_w
    perlist = calculate_power(times, charge, [v] * len(times)).power_w
    fallback = calculate_power(times, charge, None, voltage_fallback=v).power_w
    assert math.isclose(scalar, perlist) and math.isclose(scalar, fallback)


def test_scales_with_power():
    for p in (5.0, 20.0, 60.0):
        times, charge, v, _ = _synth(p)
        assert abs(calculate_power(times, charge, v).power_w - p) < p * 0.02


def test_rejects_direction_flip():
    times = [0, 1, 2, 3, 4]
    charge = [1000, 2000, 3000, 2000, 1000]  # up then down
    try:
        calculate_power(times, charge, 16.5)
    except ValueError:
        return
    raise AssertionError("expected ValueError on mixed charge/discharge")


def test_too_few_edges():
    try:
        calculate_power([0, 1], [1000, 2000], 16.5)
    except ValueError:
        return
    raise AssertionError("expected ValueError for <4 edges")


if __name__ == "__main__":
    fns = [v for k, v in sorted(globals().items()) if k.startswith("test_")]
    for fn in fns:
        fn()
        print(f"ok  {fn.__name__}")
    print(f"\n{len(fns)} tests passed")
