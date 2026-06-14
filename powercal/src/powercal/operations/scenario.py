"""Declarative measurement scenarios and a serial runner.

A :class:`Scenario` describes *what state to put the machine in* (``setup``), *how long to let
the battery averaging window flush* (``settle_s``), and *how accurate the result must be*
(``target_w``). A :class:`Runner` executes a list of them **one at a time** -- the charge counter
reflects whole-system draw, so scenarios cannot overlap -- and returns a :class:`Result` per
scenario. Adding a measurement is appending one ``Scenario`` to the list.

    runner = Runner(target_w=0.1, max_secs=300)
    results = runner.run(scenarios, prepare=lock_cpu)   # blocks until all complete

For a non-blocking "kick off and wait later" flow use :meth:`Runner.start`, which runs the same
serial batch on a background thread and hands back a :class:`RunHandle` you can ``.wait()`` on.
"""

from __future__ import annotations

import threading
import time
from dataclasses import dataclass, field
from typing import Callable, List, Optional, Sequence

from ..measurements import BAT_DEFAULT, PowerEstimate, measure_power

# A setup applies machine state and returns an optional teardown to undo it.
Teardown = Callable[[], None]
Setup = Callable[[], Optional[Teardown]]

# on_event(kind, scenario, data): kind in {"prepare", "start", "settle", "progress",
# "done", "error", "teardown"}; data is a dict (e.g. {"est": ...} for "progress"/"done").
EventFn = Callable[[str, "Scenario", dict], None]


def _noop() -> Optional[Teardown]:
    return None


def compose(*setups: Setup) -> Setup:
    """Chain setups into one. Applied left-to-right; teardowns run right-to-left (LIFO)."""

    def apply() -> Teardown:
        teardowns: List[Teardown] = []
        try:
            for s in setups:
                td = s()
                if td is not None:
                    teardowns.append(td)
        except BaseException:
            # roll back what we already applied so a partial baseline doesn't leak
            for td in reversed(teardowns):
                try:
                    td()
                except Exception:
                    pass
            raise
        def teardown() -> None:
            for td in reversed(teardowns):
                td()
        return teardown

    return apply


@dataclass
class Scenario:
    """One measurement: put the machine in a state, settle, then measure its average power."""

    name: str
    setup: Setup = _noop
    settle_s: Optional[float] = None   # wait after setup; None => runner default
    target_w: Optional[float] = None   # stop when error <= this; None => runner default
    max_secs: Optional[float] = None   # hard cap on the measurement; None => runner default
    meta: dict = field(default_factory=dict)


@dataclass
class Result:
    """Outcome of one scenario. ``estimate`` is set iff it succeeded."""

    scenario: Scenario
    estimate: Optional[PowerEstimate] = None
    error: Optional[BaseException] = None

    @property
    def ok(self) -> bool:
        return self.error is None and self.estimate is not None


def _pick(override, default):
    return default if override is None else override


class Runner:
    """Runs scenarios serially, applying a once-per-batch ``prepare`` around the whole run.

    ``measure`` and ``sleep`` are injectable so the runner can be tested without root or a
    battery. The default ``measure`` is :func:`powercal.measure_power`.
    """

    def __init__(
        self,
        *,
        target_w: float = 0.1,
        max_secs: float = 300.0,
        settle_s: float = 0.0,
        sigma: float = 2.0,
        bat: str = BAT_DEFAULT,
        poll_ms: float = 5.0,
        on_event: Optional[EventFn] = None,
        measure=measure_power,
        sleep=time.sleep,
    ) -> None:
        self.target_w = target_w
        self.max_secs = max_secs
        self.settle_s = settle_s
        self.sigma = sigma
        self.bat = bat
        self.poll_ms = poll_ms
        self.on_event = on_event
        self._measure = measure
        self._sleep = sleep

    def _emit(self, kind: str, scenario: Scenario, **data) -> None:
        if self.on_event is not None:
            self.on_event(kind, scenario, data)

    def run(
        self,
        scenarios: Sequence[Scenario],
        *,
        prepare: Optional[Setup] = None,
    ) -> List[Result]:
        """Execute every scenario in order; return one :class:`Result` each.

        A scenario that raises is captured in its ``Result.error`` and the batch continues. The
        ``prepare`` setup (e.g. CPU pinning) is applied once before the first scenario and its
        teardown runs once after the last, even on error.
        """
        results: List[Result] = []
        session_td: Optional[Teardown] = None
        if prepare is not None:
            self._emit("prepare", scenarios[0] if scenarios else Scenario("<prepare>"))
            session_td = prepare()
        try:
            for s in scenarios:
                results.append(self._run_one(s))
        finally:
            if session_td is not None:
                session_td()
        return results

    def _run_one(self, s: Scenario) -> Result:
        self._emit("start", s)
        td: Teardown = _noop  # type: ignore[assignment]
        try:
            td = s.setup() or _noop
            settle = _pick(s.settle_s, self.settle_s)
            if settle:
                self._emit("settle", s, seconds=settle)
                self._sleep(settle)
            est = self._measure(
                _pick(s.target_w, self.target_w),
                bat=self.bat,
                poll_ms=self.poll_ms,
                max_secs=_pick(s.max_secs, self.max_secs),
                sigma=self.sigma,
                progress=lambda n, t, e: self._emit("progress", s, n_edges=n, elapsed=t, est=e),
            )
            self._emit("done", s, est=est)
            return Result(s, estimate=est)
        except Exception as e:  # keep the batch going; record the failure
            self._emit("error", s, error=e)
            return Result(s, error=e)
        finally:
            td()
            self._emit("teardown", s)

    def start(
        self,
        scenarios: Sequence[Scenario],
        *,
        prepare: Optional[Setup] = None,
    ) -> "RunHandle":
        """Run the batch on a background thread; return a handle to wait on.

        Still serial -- this only frees the calling thread (e.g. a UI). Do not start two runs
        against the same battery at once.
        """
        handle = RunHandle()

        def target() -> None:
            try:
                handle._results = self.run(scenarios, prepare=prepare)
            except BaseException as e:  # pragma: no cover - defensive
                handle._error = e
            finally:
                handle._done.set()

        handle._thread = threading.Thread(target=target, name="powercal-runner", daemon=True)
        handle._thread.start()
        return handle


class RunHandle:
    """Handle for a background :meth:`Runner.start` run."""

    def __init__(self) -> None:
        self._done = threading.Event()
        self._thread: Optional[threading.Thread] = None
        self._results: List[Result] = []
        self._error: Optional[BaseException] = None

    @property
    def done(self) -> bool:
        return self._done.is_set()

    def wait(self, timeout: Optional[float] = None) -> List[Result]:
        """Block until the batch finishes (or ``timeout``); return the results.

        Re-raises any exception that escaped the runner itself (per-scenario errors are captured
        in their ``Result`` and do not raise here). Raises ``TimeoutError`` if it doesn't finish.
        """
        if not self._done.wait(timeout):
            raise TimeoutError("measurement batch still running")
        if self._error is not None:
            raise self._error
        return self._results
