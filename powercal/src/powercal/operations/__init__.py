"""Orchestration: define scenarios, set machine state, and run measurement batches."""

from .actions import (
    action,
    backlight_percent,
    command,
    find_backlight,
    inhibit_sleep,
    rfkill_block,
    sysfs_writer,
)
from .activity import VirtualMouse, keep_active
from .loader import Batch, load_batch, load_scenarios, select
from .scenario import Result, RunHandle, Runner, Scenario, Setup, Teardown, compose

__all__ = [
    "Scenario",
    "Runner",
    "Result",
    "RunHandle",
    "compose",
    "Setup",
    "Teardown",
    "sysfs_writer",
    "command",
    "action",
    "rfkill_block",
    "backlight_percent",
    "find_backlight",
    "inhibit_sleep",
    "keep_active",
    "VirtualMouse",
    "load_scenarios",
    "load_batch",
    "Batch",
    "select",
]
