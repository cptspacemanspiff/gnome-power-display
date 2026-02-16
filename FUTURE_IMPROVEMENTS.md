# Future Improvements

- Battery collection currently reads only the first `BAT*` device under `/sys/class/power_supply`; add explicit multi-battery support with deterministic aggregation for capacity, power, and status.
- Battery collection currently returns `no battery found` when no `BAT*` devices exist; consider a configurable non-fatal mode for desktop systems without batteries.
