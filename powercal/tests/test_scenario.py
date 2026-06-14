"""Runner/scenario tests with an injected fake measurer (no root, no battery)."""

from powercal import Runner, Scenario, action, compose


def _fake_estimate(power_w):
    # Runner only stores the estimate, so a stand-in object is enough here.
    return type("E", (), {"power_w": power_w})()


def _recording_measure(log):
    def measure(target, *, bat, poll_ms, max_secs, sigma, progress):
        log.append(("measure", target, max_secs))
        return _fake_estimate(target)
    return measure


def test_runs_in_order_with_setup_and_teardown():
    log = []
    def mk(name):
        return action(lambda: log.append(f"setup:{name}"),
                      lambda: log.append(f"teardown:{name}"))
    scenarios = [Scenario("a", setup=mk("a")), Scenario("b", setup=mk("b"))]
    runner = Runner(target_w=0.1, settle_s=0, measure=_recording_measure(log),
                    sleep=lambda s: log.append(("sleep", s)))
    results = runner.run(scenarios)
    assert [r.scenario.name for r in results] == ["a", "b"]
    assert all(r.ok for r in results)
    # each scenario: setup then measure then teardown, in order
    assert log == [
        "setup:a", ("measure", 0.1, 300.0), "teardown:a",
        "setup:b", ("measure", 0.1, 300.0), "teardown:b",
    ]


def test_per_scenario_overrides_and_settle():
    log = []
    s = Scenario("x", settle_s=90, target_w=0.05, max_secs=120)
    runner = Runner(target_w=0.1, settle_s=0, measure=_recording_measure(log),
                    sleep=lambda sec: log.append(("sleep", sec)))
    runner.run([s])
    assert ("sleep", 90) in log
    assert ("measure", 0.05, 120) in log     # scenario overrides runner defaults


def test_error_is_captured_and_batch_continues():
    log = []
    def boom():
        raise RuntimeError("setup failed")
    def measure(target, **kw):
        return _fake_estimate(target)
    scenarios = [Scenario("bad", setup=action(boom)), Scenario("good")]
    runner = Runner(measure=measure, sleep=lambda s: None)
    results = runner.run(scenarios)
    assert not results[0].ok and isinstance(results[0].error, RuntimeError)
    assert results[1].ok                      # batch kept going after the failure


def test_prepare_teardown_runs_once_around_batch():
    log = []
    prep = action(lambda: log.append("prep"), lambda: log.append("prep-down"))
    runner = Runner(measure=lambda t, **kw: _fake_estimate(t), sleep=lambda s: None)
    runner.run([Scenario("a"), Scenario("b")], prepare=prep)
    assert log[0] == "prep" and log[-1] == "prep-down"
    assert log.count("prep") == 1 and log.count("prep-down") == 1


def test_compose_teardown_is_lifo():
    log = []
    setup = compose(
        action(lambda: log.append("apply:1"), lambda: log.append("down:1")),
        action(lambda: log.append("apply:2"), lambda: log.append("down:2")),
    )
    teardown = setup()
    teardown()
    assert log == ["apply:1", "apply:2", "down:2", "down:1"]


def test_background_start_and_wait():
    runner = Runner(measure=lambda t, **kw: _fake_estimate(t), sleep=lambda s: None)
    handle = runner.start([Scenario("a"), Scenario("b")])
    results = handle.wait(timeout=5)
    assert handle.done
    assert [r.scenario.name for r in results] == ["a", "b"]


if __name__ == "__main__":
    fns = [v for k, v in sorted(globals().items()) if k.startswith("test_")]
    for fn in fns:
        fn()
        print(f"ok  {fn.__name__}")
    print(f"\n{len(fns)} tests passed")
