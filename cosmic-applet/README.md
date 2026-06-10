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
- Optional: [`just`](https://github.com/casey/just) for the build recipes
  (`cargo install just`).

## Install

The supported install path is the **system package**, built and installed from the repo
root along with the daemon it depends on:

```bash
# from the repo root
./scripts/install-packages.sh install      # builds + installs all packages
./scripts/install-packages.sh uninstall    # removes them
```

The COSMIC package installs three files:
- `/usr/bin/power-monitor-cosmic`
- `/usr/share/applications/io.github.cptspacemanspiff.PowerMonitor.Cosmic.desktop`
- `/usr/share/metainfo/io.github.cptspacemanspiff.PowerMonitor.Cosmic.metainfo.xml`

For local iteration, `./install.sh build` (or `just build-release`) compiles the binary
and `./install.sh restart` restarts `cosmic-panel` to reload a running applet.

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
