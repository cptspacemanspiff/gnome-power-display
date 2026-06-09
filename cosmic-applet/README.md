# COSMIC Power Monitor Applet

A native [COSMIC](https://github.com/pop-os/cosmic-epoch) panel applet that
displays live power draw in the panel and battery / charge-status / brightness
in a popup.

It is a pure frontend: it reads everything from the existing **power-monitor
daemon** over the system D-Bus interface `org.gnome.PowerMonitor`
(`GetCurrentStats`). The daemon requires no changes, and this applet is the
COSMIC counterpart to the GNOME Shell extension in `../gnome-extension`.

## Requirements

- A running COSMIC desktop session (for actually showing the applet).
- The `power-monitor-daemon` running on the system bus.
- Rust (edition 2024; tested with 1.94). `libcosmic` is pulled from git, so the
  first build downloads and compiles it — expect several minutes.
- Optional: [`just`](https://github.com/casey/just) for the install recipes
  (`cargo install just`). Without it, use the raw `cargo`/`install` commands.

## Install / uninstall

Use `install.sh` (no root, no `just` required). A user-local install touches
exactly three files in standard XDG dirs, and `uninstall` removes precisely
those — nothing is scattered around:

```bash
./install.sh install      # cargo build --release + copy 3 files to ~/.local
./install.sh list         # show what is installed
./install.sh uninstall    # remove the 3 files
./install.sh clean        # cargo clean (drops ./target build artifacts)
```

Installed files:
- `~/.local/bin/cosmic-power-monitor`
- `~/.local/share/applications/com.github.nlong.CosmicPowerMonitor.desktop`
- `~/.local/share/metainfo/com.github.nlong.CosmicPowerMonitor.metainfo.xml`

Make sure `~/.local/bin` is on your `PATH` so the panel can exec the binary.

(A `justfile` with the same recipes is also provided if you prefer `just`.)

## Add it to the panel

After installing, the panel discovers the applet via its `.desktop` file
(`X-CosmicApplet=true`). Open **COSMIC Settings → Desktop → Panel** (or Dock),
choose **Add applet**, and select **Power Monitor**. If it doesn't appear, log
out/in or restart `cosmic-panel` so it rescans the applications directories.

## How it works

- `src/dbus.rs` — `zbus` typed proxy for `org.gnome.PowerMonitor` plus serde
  structs mirroring the daemon's JSON (`internal/collector/types.go`).
- `src/subscription.rs` — background worker polling `GetCurrentStats` every 5s
  (matching the daemon's collection cadence), resilient to the daemon being
  down or restarting.
- `src/app.rs` — the `cosmic::Application`: panel button (`view`) and popup
  (`view_window`).

## Not yet implemented

History graphs (battery level / energy usage), time-range presets, and zoom
that the GNOME extension has. The daemon already exposes the data
(`GetHistory`, `GetPowerStateEvents`) — those would be drawn with
`cosmic::widget::canvas`.
