"""Keep the session non-idle by emitting tiny synthetic mouse motion via a uinput virtual device.

cosmic-idle (and GNOME/KDE) drive screen-off, lock, and idle-suspend from the compositor's *input*
idle timer (Wayland ext-idle-notify ``get_input_idle_notification``), which by design ignores idle
inhibitors -- so D-Bus/ScreenSaver inhibits and config tweaks don't stop it. The only reliable way
to keep the display and lock awake is genuine input. This creates a virtual mouse via
``/dev/uinput`` and jiggles it (+1/-1 px) on an interval, resetting that timer on any desktop (and
the console). It also keeps the machine off idle-suspend.

Needs write access to ``/dev/uinput``: root, or membership in the ``input`` group.
"""

from __future__ import annotations

import fcntl
import os
import struct
import threading
from typing import Callable, Optional

from .scenario import Setup, Teardown

UINPUT = "/dev/uinput"


# ioctl encoding (asm-generic _IOC): dir<<30 | size<<16 | type<<8 | nr
def _IOC(d: int, t: str, nr: int, size: int) -> int:
    return (d << 30) | (size << 16) | (ord(t) << 8) | nr


_UI_SET_EVBIT = _IOC(1, "U", 100, 4)
_UI_SET_KEYBIT = _IOC(1, "U", 101, 4)
_UI_SET_RELBIT = _IOC(1, "U", 102, 4)
_UI_DEV_SETUP = _IOC(1, "U", 3, 92)   # sizeof(struct uinput_setup)
_UI_DEV_CREATE = _IOC(0, "U", 1, 0)
_UI_DEV_DESTROY = _IOC(0, "U", 2, 0)

_EV_SYN, _EV_KEY, _EV_REL = 0x00, 0x01, 0x02
_REL_X = 0x00
_SYN_REPORT = 0x00
_BTN_LEFT = 0x110


class VirtualMouse:
    """A throwaway uinput pointer device. Construct, :meth:`jiggle`, then :meth:`close`."""

    def __init__(self, device: str = UINPUT) -> None:
        self._fd = os.open(device, os.O_WRONLY | os.O_NONBLOCK)
        for ioc, bit in [(_UI_SET_EVBIT, _EV_KEY), (_UI_SET_KEYBIT, _BTN_LEFT),
                         (_UI_SET_EVBIT, _EV_REL), (_UI_SET_RELBIT, _REL_X)]:
            fcntl.ioctl(self._fd, ioc, bit)
        # struct uinput_setup: input_id{bustype,vendor,product,version} + name[80] + ff_effects_max
        setup = struct.pack("HHHH80sI", 0x03, 0x1234, 0x5678, 1, b"powercal-keepawake", 0)
        fcntl.ioctl(self._fd, _UI_DEV_SETUP, setup)
        fcntl.ioctl(self._fd, _UI_DEV_CREATE)

    def _emit(self, etype: int, code: int, value: int) -> None:
        os.write(self._fd, struct.pack("llHHi", 0, 0, etype, code, value))

    def jiggle(self) -> None:
        """Move +1px then -1px so the cursor lands where it started but input is registered."""
        self._emit(_EV_REL, _REL_X, 1)
        self._emit(_EV_SYN, _SYN_REPORT, 0)
        self._emit(_EV_REL, _REL_X, -1)
        self._emit(_EV_SYN, _SYN_REPORT, 0)

    def close(self) -> None:
        try:
            fcntl.ioctl(self._fd, _UI_DEV_DESTROY)
        finally:
            os.close(self._fd)


def keep_active(
    interval_s: float = 30.0,
    *,
    device: str = UINPUT,
    mouse_factory: Optional[Callable[[str], "VirtualMouse"]] = None,
) -> Setup:
    """Setup that jiggles a virtual mouse every ``interval_s`` seconds for the batch.

    Keep ``interval_s`` comfortably below the desktop's shortest idle timeout (screen-off / lock).
    The jiggle is a sub-millisecond, net-zero cursor move -- negligible against measurement noise.
    Belongs in a batch ``PREPARE`` (once per run). Raises ``RuntimeError`` if ``/dev/uinput`` can't
    be opened. ``mouse_factory`` is injectable for testing.
    """

    def apply() -> Teardown:
        try:
            mouse = (mouse_factory or VirtualMouse)(device)
        except (PermissionError, FileNotFoundError, OSError) as e:
            raise RuntimeError(f"cannot open {device} for input synthesis ({e}); "
                               "run as root or join the 'input' group") from e
        stop = threading.Event()

        def loop() -> None:
            while True:
                try:
                    mouse.jiggle()
                except OSError:
                    return
                if stop.wait(interval_s):
                    return

        thread = threading.Thread(target=loop, name="powercal-keepactive", daemon=True)
        thread.start()

        def teardown() -> None:
            stop.set()
            thread.join(timeout=2.0)
            mouse.close()

        return teardown

    return apply
