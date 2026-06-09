// D-Bus proxy, payload structs, and async fetch helpers for
// org.gnome.PowerMonitor (system bus). Structs mirror the daemon's Go types
// in internal/collector/types.go; only fields the applet renders are kept
// (serde ignores the rest).

use serde::Deserialize;
use zbus::{Connection, proxy};

#[proxy(
    interface = "org.gnome.PowerMonitor",
    default_service = "org.gnome.PowerMonitor",
    default_path = "/org/gnome/PowerMonitor"
)]
pub trait PowerMonitor {
    /// Latest battery + backlight samples as JSON.
    fn get_current_stats(&self) -> zbus::Result<String>;
    /// Battery + backlight samples in [from, to] as JSON.
    fn get_history(&self, from_epoch: i64, to_epoch: i64) -> zbus::Result<String>;
    /// Power state events (suspend/hibernate/shutdown) in [from, to] as JSON.
    fn get_power_state_events(&self, from_epoch: i64, to_epoch: i64) -> zbus::Result<String>;
}

// ---- Current stats (panel button + popup header) ----

#[derive(Debug, Clone, Deserialize)]
pub struct CurrentStats {
    pub battery: BatterySample,
    pub backlight: BacklightSample,
}

#[derive(Debug, Clone, Deserialize)]
pub struct BatterySample {
    pub power_uw: i64,
    pub capacity_pct: i32,
    pub status: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct BacklightSample {
    pub brightness: i64,
    pub max_brightness: i64,
}

impl BatterySample {
    pub fn watts(&self) -> f64 {
        self.power_uw as f64 / 1_000_000.0
    }
}

impl BacklightSample {
    pub fn brightness_pct(&self) -> i64 {
        if self.max_brightness <= 0 {
            return 0;
        }
        ((self.brightness as f64 / self.max_brightness as f64) * 100.0).round() as i64
    }
}

// ---- History (graphs) ----

#[derive(Debug, Clone, Deserialize)]
struct HistoryReply {
    #[serde(default)]
    battery: Vec<HistSample>,
}

/// One battery sample in a history range. Fields used by both graphs.
#[derive(Debug, Clone, Deserialize)]
pub struct HistSample {
    pub timestamp: i64,
    pub capacity_pct: i32,
    pub power_uw: i64,
    pub status: String,
}

impl HistSample {
    pub fn charging(&self) -> bool {
        self.status == "Charging" || self.status == "Full"
    }
}

/// A power state transition. `kind` is "suspend", "hibernate",
/// "suspend-then-hibernate", or "shutdown".
#[derive(Debug, Clone, Deserialize)]
pub struct PowerEvent {
    pub start_time: i64,
    pub end_time: i64,
    #[serde(rename = "type")]
    pub kind: String,
}

// ---- async helpers ----

pub async fn connect() -> zbus::Result<Connection> {
    Connection::system().await
}

pub async fn fetch_current(conn: &Connection) -> Option<CurrentStats> {
    let proxy = PowerMonitorProxy::new(conn).await.ok()?;
    let json = proxy.get_current_stats().await.ok()?;
    serde_json::from_str(&json).ok()
}

pub async fn fetch_history(conn: &Connection, from: i64, to: i64) -> Option<Vec<HistSample>> {
    let proxy = PowerMonitorProxy::new(conn).await.ok()?;
    let json = proxy.get_history(from, to).await.ok()?;
    let reply: HistoryReply = serde_json::from_str(&json).ok()?;
    Some(reply.battery)
}

pub async fn fetch_events(conn: &Connection, from: i64, to: i64) -> Vec<PowerEvent> {
    let try_fetch = async {
        let proxy = PowerMonitorProxy::new(conn).await.ok()?;
        let json = proxy.get_power_state_events(from, to).await.ok()?;
        serde_json::from_str::<Vec<PowerEvent>>(&json).ok()
    };
    try_fetch.await.unwrap_or_default()
}
