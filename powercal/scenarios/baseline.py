"""Shared batch setup, applied ONCE per run -- not per scenario.

``PREPARE`` is a module-level setup the loader composes and the Runner applies before the first
scenario, tearing it down after the last. Put state here that should hold for the whole batch:
radios off, CPU pinned, etc. Per-scenario state (a specific backlight level) belongs in each
Scenario's own ``setup`` instead -- see brightness.py.

NOTE: keeping the machine awake (no suspend, no screen blank/lock) is handled automatically by the
`powercal measure`/`run` commands via a virtual-mouse jiggle that works on every desktop -- you do
not need to add it here. Toggling radios is done here (once) rather than per scenario because an
rfkill toggle perturbs power for many seconds.
"""

from powercal import compose, rfkill_block

PREPARE = compose(
    rfkill_block("wifi", "bluetooth"),
    # add more shared setup here, e.g. a lock_cpu() built from sysfs_writer/command, or
    # sysfs_writer("/sys/class/leds/.../brightness", 0) to kill the keyboard backlight.
)
