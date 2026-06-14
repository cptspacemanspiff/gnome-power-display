"""Discover scenario definitions from a folder.

Scenario *definitions* live outside the library: point :func:`load_scenarios` at a folder and it
imports every ``*.py`` in it and collects the scenarios each module exposes, by convention:

    # scenarios/brightness.py
    from powercal import Scenario, sysfs_writer

    def build():                       # a build() callable, or...
        return [Scenario(...), ...]

    SCENARIOS = [Scenario(...)]        # ...a module-level SCENARIOS list (both are allowed)

A module may also expose ``PREPARE`` -- a single setup applied **once per batch** (before the
first scenario, torn down after the last) for shared state like radios-off or CPU pinning, as
opposed to per-scenario ``Scenario.setup``. PREPAREs from all files are composed in load order.
Use :func:`load_batch` to get both the scenarios and the composed prepare; :func:`load_scenarios`
returns just the scenarios.

This keeps scenario authoring (data + factory functions) separate from the measurement engine in
``src/powercal``. There is no global registry to mutate -- a module is collected only when loaded,
and loading is deterministic (files sorted by name, ``_``-prefixed files skipped).
"""

from __future__ import annotations

import importlib.util
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable, List, Optional, Sequence

from .scenario import Scenario, Setup, compose


@dataclass
class Batch:
    """A loaded folder: the scenarios plus the composed once-per-batch ``prepare`` (or None)."""

    scenarios: List[Scenario]
    prepare: Optional[Setup] = None


def _as_scenarios(obj) -> List[Scenario]:
    if obj is None:
        return []
    items = [obj] if isinstance(obj, Scenario) else list(obj)
    for it in items:
        if not isinstance(it, Scenario):
            raise TypeError(f"expected Scenario, got {type(it).__name__}")
    return items


def _import_file(path: Path):
    mod_name = "powercal._scenarios." + "_".join(path.with_suffix("").parts[-2:])
    spec = importlib.util.spec_from_file_location(mod_name, path)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot import scenario file {path}")
    mod = importlib.util.module_from_spec(spec)
    sys.modules[mod_name] = mod  # so dataclasses/closures resolve cleanly
    spec.loader.exec_module(mod)
    return mod


def _collect(mod) -> List[Scenario]:
    out: List[Scenario] = []
    build = getattr(mod, "build", None)
    if callable(build):
        out += _as_scenarios(build())
    out += _as_scenarios(getattr(mod, "SCENARIOS", None))
    return out


def _collect_prepare(mod) -> Optional[Setup]:
    prep = getattr(mod, "PREPARE", None)
    if prep is None:
        return None
    if not callable(prep):
        raise TypeError(f"PREPARE in {mod.__name__!r} must be a setup callable, "
                        f"got {type(prep).__name__}")
    return prep


def _iter_files(path: str, recursive: bool) -> List[Path]:
    p = Path(path)
    if p.is_file():
        return [p]
    if p.is_dir():
        globbed = p.rglob("*.py") if recursive else p.glob("*.py")
        return sorted(f for f in globbed if not f.name.startswith("_"))
    raise FileNotFoundError(path)


def load_batch(path: str, *, recursive: bool = False) -> Batch:
    """Import scenario file(s) from ``path`` and return a :class:`Batch`: the collected scenarios
    (deterministic order) and the composed once-per-batch ``prepare`` from any ``PREPARE`` modules.

    Raises ``FileNotFoundError`` if the path doesn't exist, and ``ValueError`` if two scenarios in
    different files share a name (a likely copy-paste mistake).
    """
    scenarios: List[Scenario] = []
    prepares: List[Setup] = []
    seen: dict[str, Path] = {}
    for f in _iter_files(path, recursive):
        mod = _import_file(f)
        for s in _collect(mod):
            if s.name in seen and seen[s.name] != f:
                raise ValueError(
                    f"duplicate scenario name {s.name!r} in {f} and {seen[s.name]}"
                )
            seen[s.name] = f
            scenarios.append(s)
        prep = _collect_prepare(mod)
        if prep is not None:
            prepares.append(prep)
    prepare = compose(*prepares) if prepares else None
    return Batch(scenarios=scenarios, prepare=prepare)


def load_scenarios(path: str, *, recursive: bool = False) -> List[Scenario]:
    """Import scenario file(s) from ``path`` and return just the collected :class:`Scenario` list.
    Use :func:`load_batch` if you also want the once-per-batch ``prepare``."""
    return load_batch(path, recursive=recursive).scenarios


def select(scenarios: Sequence[Scenario], names: Optional[Iterable[str]]) -> List[Scenario]:
    """Return scenarios whose name is in ``names`` (preserving the requested order). ``None`` or
    empty ``names`` returns all of them. Raises ``KeyError`` for unknown names."""
    if not names:
        return list(scenarios)
    by_name = {s.name: s for s in scenarios}
    missing = [n for n in names if n not in by_name]
    if missing:
        raise KeyError(f"unknown scenario(s): {', '.join(missing)}; "
                       f"have {', '.join(by_name)}")
    return [by_name[n] for n in names]
