# AGENTS.md

Guidance for autonomous coding agents working in `gnome-power-display`.

## Project Snapshot

- Primary stack: Go + Bazel (`rules_go`, `gazelle`)
- Components:
  - `cmd/power-monitor-daemon`: system daemon (D-Bus + SQLite + collectors)
  - `cmd/power-calibrate`: root CLI for display power calibration
  - `cmd/power-gui`: Go/Fyne GUI
  - `gnome-extension/`: GNOME Shell extension (JS/CSS/schemas)
  - `internal/*`: collector, storage, dbus, config, calibration internals
- Build metadata lives in `MODULE.bazel` and package `BUILD.bazel` files.
- CI runs Bazel tests/build in a devcontainer (`.github/workflows/ci.yml`).

## Priority Instruction Files

- Read `CLAUDE.md` first for architecture, runtime behavior, and known pitfalls.
- No Cursor rules were found at the time this file was written:
  - `.cursorrules` is absent
  - `.cursor/rules/` is absent
- No Copilot instruction file was found:
  - `.github/copilot-instructions.md` is absent
- If any of the above files are added later, treat them as high-priority constraints.

## Build Commands

- Build core binaries:
  - `bazel build //cmd/power-monitor-daemon //cmd/power-calibrate`
- Build GUI binary:
  - `bazel build //cmd/power-gui`
- Build everything:
  - `bazel build //...`
- Run daemon:
  - `bazel run //cmd/power-monitor-daemon`
  - `bazel run //cmd/power-monitor-daemon -- -verbose`
  - `bazel run //cmd/power-monitor-daemon -- -log=battery,process`
- Run calibration binary after build (requires root):
  - `sudo bazel-bin/cmd/power-calibrate/power-calibrate_/power-calibrate`
- Regenerate BUILD metadata after package/dependency moves:
  - `bazel run //:gazelle`

## Test Commands (Including Single-Test Usage)

- Run all tests:
  - `bazel test //...`
- Current Bazel test target(s):
  - `//internal/config:config_test`
- Run one test target:
  - `bazel test //internal/config:config_test`
- Run one test function by name:
  - `bazel test //internal/config:config_test --test_filter='TestLoad_ValidationErrors'`
- Run one subtest case:
  - `bazel test //internal/config:config_test --test_filter='TestLoad_ValidationErrors/interval_seconds_must_be_positive'`
- Show full logs during test execution:
  - `bazel test //internal/config:config_test --test_output=all`
- Force a rerun without cached test results:
  - `bazel test //internal/config:config_test --nocache_test_results`

## Lint / Format / Static Analysis

- There is no dedicated lint target checked into this repo (no golangci-lint or eslint config).
- Effective quality gates are:
  - `bazel test //...`
  - `bazel build //cmd/power-monitor-daemon //cmd/power-calibrate //cmd/power-gui`
- Go formatting expectations:
  - Keep code `gofmt`-clean.
  - Follow gofumpt-style formatting (repo VS Code config enables gofumpt).
- Keep imports and whitespace canonical; avoid hand-styled formatting.

## GNOME Extension Workflow

- Install/symlink extension once:
  - `./gnome-extension/install.sh install`
- Run nested shell for iterative testing:
  - `./gnome-extension/install.sh nested`
- Recompile schemas after schema edits:
  - `./gnome-extension/install.sh schemas`
- Tail shell logs:
  - `./gnome-extension/install.sh log`
- Uninstall extension:
  - `./gnome-extension/install.sh uninstall`

## Go Code Style Guidelines

### Imports

- Group imports in this order:
  1. standard library
  2. third-party modules
  3. local module imports (`github.com/cptspacemanspiff/gnome-power-display/...`)
- Separate groups with one blank line.
- Alias imports only when clarity improves (example: `dbussvc`).

### Formatting and File Structure

- Keep files focused by domain concern (`battery.go`, `sleep.go`, `cleanup.go`, etc.).
- Prefer small helpers for sysfs parsing and repetitive transformations.
- Avoid unrelated refactors while fixing a scoped issue.

### Types and Units

- Use explicit unit suffixes in identifiers:
  - `VoltageUV`, `CurrentUA`, `PowerUW`, `FreqKHz`, `Timestamp`
- Use `int64` for epoch timestamps and micro-unit numeric values.
- Keep serialized field names stable (JSON/TOML snake_case tags).

### Naming Conventions

- Exported identifiers: `PascalCase`.
- Unexported identifiers: `camelCase`.
- Prefer descriptive names over abbreviations, except widely accepted ones (`CPU`, `PID`, `DBus`).
- Keep constants explicit and stable for service contracts (`BusName`, `IfaceName`, `ObjPath`).

### Error Handling

- Return errors instead of silently ignoring them.
- Wrap errors with context using `%w`.
- Validate inputs at boundaries (config load, D-Bus range arguments, CLI flags).
- In D-Bus methods, convert failures to D-Bus errors via `godbus.MakeFailedError`.
- In transactions, rollback on failure and commit once on success.
- Use `defer` for cleanup of closers/resources (`rows.Close`, DB/file handles).

### Concurrency and Runtime Behavior

- Keep ticker/select loops simple and resilient to transient read failures.
- Preserve wake-triggered state-log re-read behavior.
- Preserve wall-clock jump detection behavior (`time.Now().Round(0)` usage).
- Buffered/non-blocking channels are intentional in wake signaling; keep semantics.

## SQL and Storage Guidelines

- Always use placeholders for values (`?`).
- If SQL identifiers are assembled via `fmt.Sprintf`, identifiers must come only from compile-time constants and be documented as such.
- Keep schema/index changes backward compatible.
- Preserve SQLite WAL mode and single writer connection model unless there is a strong reason to change.

## GNOME Extension (JS) Style Guidelines

- Use GNOME ESM import patterns:
  - `gi://...`
  - `resource:///...`
  - local relative modules (`./graphs.js`)
- Match existing style:
  - 4-space indentation
  - semicolons
  - underscore-prefixed internal methods/fields (`_refreshGraph`, `_timerId`)
- Keep drawing logic in `graphs.js` and `graphUtils.js`; keep orchestration in `extension.js`.
- Use early returns for guards and keep repaint paths efficient.

## Agent Behavior in This Repository

- Make minimal, targeted changes.
- Do not break D-Bus interface names, object path, or method signatures without explicit instruction.
- Do not change serialized JSON field names consumed by the extension unless coordinated.
- Update docs when behavior/config/commands change (`README.md`, `CLAUDE.md`, and extension docs when relevant).
- If you add tests, register them in the relevant `BUILD.bazel` using `go_test`.

## Validation Checklist Before Finishing

- Build changed targets successfully.
- Run affected tests (or `bazel test //...` for broad changes).
- Confirm no accidental contract break in:
  - D-Bus methods/signatures
  - JSON payload structure returned by daemon
  - TOML keys/defaults in config
- For extension UI/drawing changes, smoke test in nested shell when possible.

## Known Pitfalls to Respect

- Battery `power_now` has a long averaging window; avoid quick-read assumptions in calibration.
- Sleep/hibernate reconstruction relies on state-log ingestion plus wake-triggered refresh.
- Battery fallback power computation must avoid overflow by dividing before multiply.
- Process cmdline cache pruning must follow tracked PIDs to avoid memory growth.
- On hybrid Intel CPU pinning, min/max write order matters.
