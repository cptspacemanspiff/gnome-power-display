"""Tests for measurement activity reporting: the CLI live line and measure_power's emit cadence."""

import io

from powercal.cli import _Activity
from powercal.measurements import measure as M


def test_activity_line_non_tty_logs_each_update():
    buf = io.StringIO()  # StringIO.isatty() is False -> log-line fallback
    act = _Activity(0.1, stream=buf)
    act(2, 1.0, None)  # before any fit
    est = type("E", (), {"power_w": 41.5, "std_w": 0.2, "std_robust_w": 0.25})()
    act(10, 5.0, est)  # with an estimate, above target
    out = buf.getvalue()
    assert "warming up" in out
    assert "41.500 W" in out
    assert "need <= 0.1 W" in out          # not yet at target
    assert out.count("\n") == 2            # one appended line per call


def test_activity_marks_target_met():
    buf = io.StringIO()
    act = _Activity(0.3, stream=buf)
    est = type("E", (), {"power_w": 41.5, "std_w": 0.1, "std_robust_w": 0.12})()
    act(40, 60.0, est)
    assert "target met" in buf.getvalue()


class _FakeClock:
    def __init__(self, poll):
        self.t = 0.0
        self.poll = poll

    def monotonic(self):
        return self.t

    def sleep(self, _s):
        self.t += self.poll


class _DummyFile:
    def __enter__(self):
        return self

    def __exit__(self, *a):
        return False

    def write(self, *a):
        pass


def test_measure_power_emits_warmup_and_fits(monkeypatch):
    poll = 0.05
    clock = _FakeClock(poll)
    tau = 0.4  # seconds per 1000-uAh tick -> a steady discharge

    def fake_read(path):
        if path.endswith("/charge_now"):
            return 1_000_000 - int(clock.t / tau) * 1000
        if path.endswith("/voltage_now"):
            return 16_500_000  # -> 16.5 V
        return 0               # CACHE_PARAM

    monkeypatch.setattr(M, "_read_int", fake_read)
    monkeypatch.setattr(M.os, "access", lambda *a: True)
    monkeypatch.setattr(M, "time", clock)
    monkeypatch.setattr(M, "open", lambda *a, **k: _DummyFile(), raising=False)

    calls = []
    est = M.measure_power(
        0.0,  # impossible target -> runs to max_secs, exercising the whole loop
        bat="/fake", poll_ms=poll * 1000,
        min_edges=3, min_secs=0.2, recompute_every=2,
        max_secs=5.0, heartbeat_s=0.5,
        progress=lambda n, t, e: calls.append((n, t, e)),
    )
    assert calls, "progress was never called"
    assert any(e is None for _, _, e in calls)      # warming-up heartbeats before first fit
    assert any(e is not None for _, _, e in calls)   # fits emit too
    # heartbeats fire during the pre-fit window even with no new edge
    assert sum(1 for _, _, e in calls if e is None) >= 2
    # P = quantum*V*3.6e-3/tau = 1000*16.5*3.6e-3/0.4 = 148.5 W
    assert est is not None and abs(est.power_w - 148.5) < 5.0


if __name__ == "__main__":
    fns = [v for k, v in sorted(globals().items()) if k.startswith("test_")]
    for fn in fns:
        try:
            fn()
        except TypeError:
            continue  # skip monkeypatch-only tests when run without pytest
        print(f"ok  {fn.__name__}")
