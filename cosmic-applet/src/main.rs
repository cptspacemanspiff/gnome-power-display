// COSMIC panel applet for the power-monitor daemon.
//
// Consumes the same system D-Bus interface (org.gnome.PowerMonitor) as the
// GNOME Shell extension. The daemon needs no changes.

mod app;
mod dbus;
mod graph;

fn main() -> cosmic::iced::Result {
    cosmic::applet::run::<app::AppModel>(())
}
