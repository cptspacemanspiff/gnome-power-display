"""Idle baseline -- whole-system power with no state change, just settle and measure.

Shows the other supported convention: a module-level ``SCENARIOS`` list instead of ``build()``.
"""

from powercal import Scenario

SCENARIOS = [
    Scenario(name="idle-baseline", settle_s=120, target_w=0.05),
]
