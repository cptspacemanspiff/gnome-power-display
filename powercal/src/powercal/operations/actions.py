"""Reusable :data:`powercal.scenario.Setup` builders.

A setup is a zero-arg callable that applies machine state and returns an optional teardown. These
are the primitives you compose (with :func:`powercal.compose`) to describe a scenario's state.
Each one here captures the prior value and restores it on teardown, so a batch leaves the machine
as it found it.
"""

from __future__ import annotations

import os
import signal
import subprocess
from typing import Callable, Optional

from .scenario import Setup, Teardown

BACKLIGHT_BASE = "/sys/class/backlight"


def _read(path: str) -> str:
    with open(path) as f:
        return f.read().strip()


def _read_int(path: str) -> int:
    return int(_read(path))


def _write(path: str, value: str) -> None:
    with open(path, "w") as f:
        f.write(value)


def sysfs_writer(path: str, value, *, restore: bool = True) -> Setup:
    """Write ``value`` to a sysfs file (e.g. backlight brightness, cpufreq), restoring the old
    value on teardown. ``restore=False`` leaves the new value in place."""

    def apply() -> Optional[Teardown]:
        old = _read(path)
        _write(path, str(value))
        if not restore:
            return None
        return lambda: _write(path, old)

    return apply


def find_backlight(base: str = BACKLIGHT_BASE) -> str:
    """Return the path of the backlight device to control.

    When several exist (e.g. ``intel_backlight`` and an ``acpi_video0`` shim) the one with the
    largest ``max_brightness`` is chosen -- that's the real hardware PWM device, not the coarse
    ACPI wrapper. Raises ``FileNotFoundError`` if there is no backlight device.
    """
    try:
        names = os.listdir(base)
    except FileNotFoundError:
        names = []
    if not names:
        raise FileNotFoundError(f"no backlight device under {base}")

    def maxb(name: str) -> int:
        try:
            return _read_int(f"{base}/{name}/max_brightness")
        except OSError:
            return -1

    return f"{base}/{max(names, key=maxb)}"


def backlight_percent(percent: float, device: Optional[str] = None, *, restore: bool = True) -> Setup:
    """Set the backlight to ``percent`` of its ``max_brightness`` (so 50 == 50%, 100 == full).

    ``max_brightness`` and the device are resolved lazily at apply-time -- nothing touches sysfs
    just to build or list the scenario -- so a name like ``brightness-50`` means 50% on whatever
    panel the run actually happens on. ``device`` defaults to :func:`find_backlight`. The previous
    raw brightness is restored on teardown unless ``restore=False``.
    """

    def apply() -> Optional[Teardown]:
        dev = device or find_backlight()
        max_raw = _read_int(f"{dev}/max_brightness")
        raw = round(percent / 100.0 * max_raw)
        raw = max(0, min(max_raw, raw))
        path = f"{dev}/brightness"
        old = _read(path)
        _write(path, str(raw))
        if not restore:
            return None
        return lambda: _write(path, old)

    return apply


def command(apply_cmd, teardown_cmd=None, *, check: bool = True) -> Setup:
    """Run a shell command (list argv) on apply, and optionally another on teardown.

    Use for state with no clean sysfs file -- e.g. ``["rfkill", "block", "wifi"]`` with
    ``["rfkill", "unblock", "wifi"]`` as teardown.
    """

    def apply() -> Optional[Teardown]:
        subprocess.run(apply_cmd, check=check)
        if teardown_cmd is None:
            return None
        return lambda: subprocess.run(teardown_cmd, check=check)

    return apply


def inhibit_sleep(
    what: str = "sleep:idle:handle-lid-switch",
    *,
    who: str = "powercal",
    why: str = "power measurement in progress",
) -> Setup:
    """Hold a systemd-logind *block* inhibitor for the batch, then release it on teardown.

    Blocks suspend/hibernate, the logind idle action, and lid-close handling -- so a long
    unattended run can't be cut short by the machine going to sleep. Implemented by spawning
    ``systemd-inhibit ... --mode=block sleep infinity`` and killing it (and its process group) on
    teardown. Belongs in a batch ``PREPARE`` (once per run), not per scenario.

    NOTE: this does *not* stop the desktop from DPMS-blanking the display -- that is driven by the
    DE's idle timer, not logind. Handle the screen separately for your environment.

    Raises ``RuntimeError`` if ``systemd-inhibit`` is unavailable (non-systemd system).
    """

    def apply() -> Teardown:
        argv = ["systemd-inhibit", f"--what={what}", f"--who={who}", f"--why={why}",
                "--mode=block", "sleep", "infinity"]
        try:
            proc = subprocess.Popen(argv, start_new_session=True)
        except FileNotFoundError as e:
            raise RuntimeError("systemd-inhibit not found; cannot block suspend/idle") from e

        def teardown() -> None:
            try:
                os.killpg(proc.pid, signal.SIGTERM)
            except (ProcessLookupError, PermissionError):
                return
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(proc.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass

        return teardown

    return apply


def action(apply_fn: Callable[[], None], teardown_fn: Optional[Callable[[], None]] = None) -> Setup:
    """Wrap arbitrary callables as a setup -- the escape hatch for anything not covered above."""

    def apply() -> Optional[Teardown]:
        apply_fn()
        return teardown_fn

    return apply


def _rfkill_soft_blocked(identifier: str) -> Optional[bool]:
    """True/False if the rfkill device is currently soft-blocked, or None if unknown."""
    try:
        out = subprocess.run(["rfkill", "list", identifier],
                             capture_output=True, text=True, check=True).stdout
    except (FileNotFoundError, subprocess.CalledProcessError):
        return None
    for line in out.splitlines():
        if "Soft blocked:" in line:
            return line.strip().endswith("yes")
    return None


def rfkill_block(*identifiers: str) -> Setup:
    """Soft-block radios via ``rfkill`` (e.g. ``rfkill_block("wifi", "bluetooth")``).

    Only the radios that were *not already blocked* are unblocked on teardown, so a batch leaves
    radio state as it found it. Typically used in a batch ``PREPARE`` (once per run), not per
    scenario -- toggling radios mid-batch perturbs power for many seconds.
    """

    def apply() -> Optional[Teardown]:
        to_restore = []
        for ident in identifiers:
            was_blocked = _rfkill_soft_blocked(ident)
            subprocess.run(["rfkill", "block", ident], check=True)
            if was_blocked is False:
                to_restore.append(ident)
        if not to_restore:
            return None

        def teardown() -> None:
            for ident in to_restore:
                subprocess.run(["rfkill", "unblock", ident], check=False)

        return teardown

    return apply
