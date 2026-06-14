"""Tests for folder-based scenario discovery."""

from pathlib import Path

import pytest

from powercal import Scenario, load_batch, load_scenarios, select

REPO_SCENARIOS = Path(__file__).resolve().parent.parent / "scenarios"


def test_loads_shipped_scenarios_folder():
    scenarios = load_scenarios(str(REPO_SCENARIOS))
    names = {s.name for s in scenarios}
    assert "idle-baseline" in names          # SCENARIOS-list convention
    assert any(n.startswith("brightness-") for n in names)  # build() convention
    assert all(isinstance(s, Scenario) for s in scenarios)


def test_build_and_scenarios_list_both_collected(tmp_path):
    (tmp_path / "a.py").write_text(
        "from powercal import Scenario\n"
        "def build():\n"
        "    return [Scenario('built-1'), Scenario('built-2')]\n"
    )
    (tmp_path / "b.py").write_text(
        "from powercal import Scenario\n"
        "SCENARIOS = [Scenario('listed')]\n"
    )
    (tmp_path / "_skip.py").write_text("raise RuntimeError('should not be imported')\n")
    names = [s.name for s in load_scenarios(str(tmp_path))]
    assert names == ["built-1", "built-2", "listed"]   # sorted by file, _-prefixed skipped


def test_duplicate_name_across_files_raises(tmp_path):
    (tmp_path / "x.py").write_text(
        "from powercal import Scenario\nSCENARIOS = [Scenario('dup')]\n"
    )
    (tmp_path / "y.py").write_text(
        "from powercal import Scenario\nSCENARIOS = [Scenario('dup')]\n"
    )
    with pytest.raises(ValueError, match="duplicate scenario name"):
        load_scenarios(str(tmp_path))


def test_select_filters_and_orders():
    scenarios = [Scenario("a"), Scenario("b"), Scenario("c")]
    assert [s.name for s in select(scenarios, ["c", "a"])] == ["c", "a"]
    assert [s.name for s in select(scenarios, None)] == ["a", "b", "c"]
    with pytest.raises(KeyError):
        select(scenarios, ["nope"])


def test_missing_path_raises():
    with pytest.raises(FileNotFoundError):
        load_scenarios("/does/not/exist")


def test_prepare_collected_and_composed(tmp_path):
    marker = tmp_path / "applied"
    (tmp_path / "baseline.py").write_text(
        "from powercal import action\n"
        "import pathlib\n"
        f"_P = pathlib.Path({str(marker)!r})\n"
        "PREPARE = action(lambda: _P.write_text('on'), lambda: _P.unlink())\n"
    )
    (tmp_path / "s.py").write_text(
        "from powercal import Scenario\nSCENARIOS = [Scenario('x')]\n"
    )
    batch = load_batch(str(tmp_path))
    assert [s.name for s in batch.scenarios] == ["x"]   # PREPARE file contributes no scenarios
    assert batch.prepare is not None

    teardown = batch.prepare()                          # apply the shared baseline
    assert marker.exists()
    teardown()                                          # ...and undo it
    assert not marker.exists()


def test_no_prepare_means_none(tmp_path):
    (tmp_path / "s.py").write_text(
        "from powercal import Scenario\nSCENARIOS = [Scenario('x')]\n"
    )
    assert load_batch(str(tmp_path)).prepare is None
