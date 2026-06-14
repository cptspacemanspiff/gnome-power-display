"""Tests for setup builders that don't need root: backlight scaling and device discovery."""

import signal

import pytest

from powercal import backlight_percent, find_backlight, inhibit_sleep
from powercal.operations import actions as A


def _make_device(tmp_path, name, max_raw, cur):
    dev = tmp_path / name
    dev.mkdir()
    (dev / "max_brightness").write_text(f"{max_raw}\n")
    (dev / "brightness").write_text(f"{cur}\n")
    return dev


def test_backlight_percent_scales_to_max_and_restores(tmp_path):
    dev = _make_device(tmp_path, "intel_backlight", max_raw=1000, cur=123)
    teardown = backlight_percent(50, device=str(dev))()
    assert (dev / "brightness").read_text().strip() == "500"   # 50% of 1000
    teardown()
    assert (dev / "brightness").read_text().strip() == "123"   # restored


def test_backlight_percent_endpoints(tmp_path):
    dev = _make_device(tmp_path, "bl", max_raw=937, cur=400)
    backlight_percent(0, device=str(dev), restore=False)()
    assert (dev / "brightness").read_text().strip() == "0"
    backlight_percent(100, device=str(dev), restore=False)()
    assert (dev / "brightness").read_text().strip() == "937"


def test_find_backlight_prefers_largest_max(tmp_path):
    _make_device(tmp_path, "acpi_video0", max_raw=10, cur=5)
    _make_device(tmp_path, "intel_backlight", max_raw=937, cur=400)
    assert find_backlight(base=str(tmp_path)).endswith("/intel_backlight")


def test_find_backlight_none(tmp_path):
    with pytest.raises(FileNotFoundError):
        find_backlight(base=str(tmp_path / "empty"))


class _FakeProc:
    pid = 4321

    def wait(self, timeout=None):
        return 0


def test_inhibit_sleep_spawns_block_inhibitor_and_releases(monkeypatch):
    rec = {}
    monkeypatch.setattr(A.subprocess, "Popen",
                        lambda argv, **kw: rec.update(argv=argv, kw=kw) or _FakeProc())
    killed = []
    monkeypatch.setattr(A.os, "killpg", lambda pid, sig: killed.append((pid, sig)))

    teardown = inhibit_sleep()()                       # apply
    assert rec["argv"][0] == "systemd-inhibit"
    assert "--mode=block" in rec["argv"]
    assert any(a.startswith("--what=") and "sleep" in a for a in rec["argv"])
    assert rec["kw"].get("start_new_session") is True  # own process group, so killpg works

    teardown()                                         # release
    assert killed == [(4321, signal.SIGTERM)]


def test_inhibit_sleep_raises_without_systemd(monkeypatch):
    def boom(*a, **k):
        raise FileNotFoundError("systemd-inhibit")
    monkeypatch.setattr(A.subprocess, "Popen", boom)
    with pytest.raises(RuntimeError, match="systemd-inhibit not found"):
        inhibit_sleep()()
