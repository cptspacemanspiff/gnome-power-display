"""powercal -- average power from a battery charge counter, with error bars.

Quick start::

    from powercal import calculate_power, load_edges_csv

    times, charge, volts = load_edges_csv("/tmp/charge-edges.csv")
    est = calculate_power(times, charge, volts)
    print(est)                       # 41.556 W  +/-0.046 W (1 sigma)  guaranteed [41.40, 42.09] W
    print(est.power_w, est.std_w, est.lower_w, est.upper_w)

The estimate (``power_w``) is a least-squares fit over every charge edge; ``std_w`` is its
statistical standard error; ``lower_w``/``upper_w`` are guaranteed hard bounds on the true
average power. The package is split into two subpackages:

* :mod:`powercal.measurements` -- the estimation engine (calculate_power, edges, measure_power).
* :mod:`powercal.operations`   -- orchestration (Scenario, Runner, setup builders, loader).

See ``measurements/power.py`` for the method and BRACKET-METHOD.md for the derivation.
"""

from .measurements import (
    PowerEstimate,
    calculate_power,
    capture_edges,
    load_edges_csv,
    measure_power,
)
from .operations import (
    Batch,
    Result,
    RunHandle,
    Runner,
    Scenario,
    action,
    backlight_percent,
    command,
    compose,
    find_backlight,
    inhibit_sleep,
    keep_active,
    load_batch,
    load_scenarios,
    rfkill_block,
    select,
    sysfs_writer,
)

__all__ = [
    # estimation
    "calculate_power",
    "PowerEstimate",
    "load_edges_csv",
    "capture_edges",
    "measure_power",
    # scenarios / orchestration
    "Scenario",
    "Runner",
    "Result",
    "RunHandle",
    "compose",
    "load_scenarios",
    "load_batch",
    "Batch",
    "select",
    # setup builders
    "sysfs_writer",
    "command",
    "rfkill_block",
    "backlight_percent",
    "find_backlight",
    "inhibit_sleep",
    "keep_active",
    "action",
]
