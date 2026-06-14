"""Tests for the universal keep-awake jiggle (injected fake mouse -- no real /dev/uinput)."""

import threading
import time

import pytest

from powercal import keep_active


class _FakeMouse:
    def __init__(self, device=None):
        self.jiggles = 0
        self.closed = False
        self._lock = threading.Lock()

    def jiggle(self):
        with self._lock:
            self.jiggles += 1

    def close(self):
        self.closed = True


def test_keep_active_jiggles_then_stops_and_closes():
    made = {}
    td = keep_active(interval_s=0.01,
                     mouse_factory=lambda device: made.setdefault("m", _FakeMouse()))()
    time.sleep(0.05)            # let the loop run a few iterations
    td()                        # stop + close
    m = made["m"]
    assert m.jiggles >= 1       # jiggled immediately and on the interval
    assert m.closed             # device released on teardown


def test_keep_active_unavailable_device_raises():
    def boom(device):
        raise PermissionError(device)
    with pytest.raises(RuntimeError, match="input synthesis"):
        keep_active(mouse_factory=boom)()


def test_keep_active_jiggles_at_least_once_even_if_torn_down_fast():
    made = {}
    td = keep_active(interval_s=100.0,   # long interval: the immediate first jiggle is what counts
                     mouse_factory=lambda device: made.setdefault("m", _FakeMouse()))()
    td()
    assert made["m"].jiggles >= 1
    assert made["m"].closed
