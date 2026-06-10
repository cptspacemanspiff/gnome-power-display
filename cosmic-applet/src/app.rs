// The applet: panel watts indicator + popup with stats, two interactive
// graphs (battery level, energy usage), time-range presets, and drag-to-zoom
// with a back-stack. A port of the GNOME extension's indicator/popup.

use std::rc::Rc;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use cosmic::app::{Core, Task};
use cosmic::applet::padded_control;
use cosmic::iced::platform_specific::shell::wayland::commands::popup::{destroy_popup, get_popup};
use cosmic::iced::window::Id;
use cosmic::iced::{Length, Limits, Subscription};
use cosmic::prelude::*;
use cosmic::widget;

use crate::dbus::{self, CurrentStats, HistSample, PowerEvent};
use crate::graph::{BatteryChart, ChartData, EnergyChart, GRAPH_H, GRAPH_W, GW};

/// Time-range presets: (label, seconds). Matches the GNOME extension.
const TIME_RANGES: &[(&str, i64)] = &[
    ("15m", 900),
    ("1h", 3600),
    ("3h", 10800),
    ("6h", 21600),
    ("24h", 86400),
    ("7d", 604800),
];
const DEFAULT_RANGE: usize = 1;

const M_LEFT: f32 = 8.0; // must match graph.rs margins for pixel→time mapping

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum GraphId {
    Battery,
    Energy,
}

/// A previous view to return to when zooming out.
#[derive(Clone, Copy)]
enum RangeSel {
    Preset(usize),
    Custom(i64, i64),
}

#[derive(Clone, Copy)]
struct DragSel {
    start: f32,
    end: f32,
}

pub struct AppModel {
    core: Core,
    popup: Option<Id>,
    conn: Option<zbus::Connection>,

    current: Option<CurrentStats>,
    available: bool,

    samples: Rc<Vec<HistSample>>,
    events: Rc<Vec<PowerEvent>>,

    range_idx: usize,
    custom: Option<(i64, i64)>,
    stack: Vec<RangeSel>,

    drag: Option<DragSel>,
    hover: Option<(GraphId, f32)>,
}

#[derive(Debug, Clone)]
pub enum Message {
    TogglePopup,
    PopupClosed(Id),
    Connected(zbus::Connection),
    Reconnect,
    Disconnected,
    Tick,
    CurrentLoaded(Option<CurrentStats>),
    HistoryLoaded(Option<Vec<HistSample>>, Vec<PowerEvent>),
    SelectRange(usize),
    ZoomBack,
    GraphPress(f32),
    GraphMove(GraphId, Option<f32>),
    GraphRelease,
}

fn now_epoch() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

impl AppModel {
    /// Current (from, to, seconds) of the visible window.
    fn range(&self) -> (i64, i64, i64) {
        match self.custom {
            Some((f, t)) => (f, t, (t - f).max(1)),
            None => {
                let now = now_epoch();
                let s = TIME_RANGES[self.range_idx].1;
                (now - s, now, s)
            }
        }
    }

    fn current_sel(&self) -> RangeSel {
        match self.custom {
            Some((f, t)) => RangeSel::Custom(f, t),
            None => RangeSel::Preset(self.range_idx),
        }
    }

    fn show_back(&self) -> bool {
        !self.stack.is_empty() || self.custom.is_some()
    }

    /// Build a connect-or-fetch task for the current state.
    fn refresh(&self) -> Task<Message> {
        let Some(conn) = self.conn.clone() else {
            return cosmic::task::future(async { Message::Reconnect });
        };
        let (from, to, _) = self.range();
        let popup_open = self.popup.is_some();

        let c1 = conn.clone();
        let mut tasks =
            vec![cosmic::task::future(async move { Message::CurrentLoaded(dbus::fetch_current(&c1).await) })];
        if popup_open {
            let c2 = conn;
            tasks.push(cosmic::task::future(async move {
                let h = dbus::fetch_history(&c2, from, to).await;
                let e = dbus::fetch_events(&c2, from, to).await;
                Message::HistoryLoaded(h, e)
            }));
        }
        Task::batch(tasks)
    }

    /// Finish a drag: zoom to the selected pixel range (port of finishDrag).
    fn finish_drag(&mut self, d: DragSel) -> Task<Message> {
        let (from, _to, seconds) = self.range();
        let min = d.start.min(d.end);
        let max = d.start.max(d.end);
        if max - min < 10.0 {
            return Task::none();
        }
        let t1 = from as f64 + ((min - M_LEFT) / GW) as f64 * seconds as f64;
        let t2 = from as f64 + ((max - M_LEFT) / GW) as f64 * seconds as f64;
        let now = now_epoch();
        let cf = (t1.floor() as i64).max(from);
        let ct = (t2.ceil() as i64).min(now);
        if ct - cf < 60 {
            return Task::none();
        }
        self.stack.push(self.current_sel());
        self.custom = Some((cf, ct));
        self.refresh()
    }
}

impl cosmic::Application for AppModel {
    type Executor = cosmic::executor::Default;
    type Flags = ();
    type Message = Message;

    const APP_ID: &'static str = "io.github.cptspacemanspiff.PowerMonitor.Cosmic";

    fn core(&self) -> &Core {
        &self.core
    }

    fn core_mut(&mut self) -> &mut Core {
        &mut self.core
    }

    fn init(core: Core, _flags: ()) -> (Self, Task<Message>) {
        let model = AppModel {
            core,
            popup: None,
            conn: None,
            current: None,
            available: false,
            samples: Rc::new(Vec::new()),
            events: Rc::new(Vec::new()),
            range_idx: DEFAULT_RANGE,
            custom: None,
            stack: Vec::new(),
            drag: None,
            hover: None,
        };
        let connect = cosmic::task::future(async {
            match dbus::connect().await {
                Ok(c) => Message::Connected(c),
                Err(_) => Message::Disconnected,
            }
        });
        (model, connect)
    }

    fn on_close_requested(&self, id: Id) -> Option<Message> {
        Some(Message::PopupClosed(id))
    }

    fn subscription(&self) -> Subscription<Message> {
        cosmic::iced::time::every(Duration::from_secs(5)).map(|_| Message::Tick)
    }

    fn update(&mut self, message: Message) -> Task<Message> {
        match message {
            Message::Tick => return self.refresh(),
            Message::Reconnect => {
                return cosmic::task::future(async {
                    match dbus::connect().await {
                        Ok(c) => Message::Connected(c),
                        Err(_) => Message::Disconnected,
                    }
                });
            }
            Message::Connected(c) => {
                self.conn = Some(c);
                return self.refresh();
            }
            Message::Disconnected => {
                self.conn = None;
                self.available = false;
                self.current = None;
            }
            Message::CurrentLoaded(stats) => match stats {
                Some(s) => {
                    self.current = Some(s);
                    self.available = true;
                }
                None => {
                    // Call failed — drop the connection so the next tick reconnects.
                    self.available = false;
                    self.current = None;
                    self.conn = None;
                }
            },
            Message::HistoryLoaded(h, e) => {
                if let Some(s) = h {
                    self.samples = Rc::new(s);
                }
                self.events = Rc::new(e);
            }
            Message::SelectRange(i) => {
                self.range_idx = i;
                self.custom = None;
                self.stack.clear();
                return self.refresh();
            }
            Message::ZoomBack => {
                if let Some(prev) = self.stack.pop() {
                    match prev {
                        RangeSel::Preset(i) => {
                            self.custom = None;
                            self.range_idx = i;
                        }
                        RangeSel::Custom(f, t) => self.custom = Some((f, t)),
                    }
                    return self.refresh();
                }
            }
            Message::GraphPress(x) => {
                self.drag = Some(DragSel { start: x, end: x });
                self.hover = None;
            }
            Message::GraphMove(id, x) => match x {
                Some(x) => {
                    if let Some(d) = self.drag.as_mut() {
                        d.end = x;
                    } else {
                        self.hover = Some((id, x));
                    }
                }
                None => {
                    if self.drag.is_none() {
                        self.hover = None;
                    }
                }
            },
            Message::GraphRelease => {
                if let Some(d) = self.drag.take() {
                    return self.finish_drag(d);
                }
            }
            Message::PopupClosed(id) => {
                if self.popup.as_ref() == Some(&id) {
                    self.popup = None;
                    self.drag = None;
                    self.hover = None;
                }
            }
            Message::TogglePopup => {
                if let Some(id) = self.popup.take() {
                    return destroy_popup(id);
                }
                let new_id = Id::unique();
                self.popup = Some(new_id);
                let mut settings = self.core.applet.get_popup_settings(
                    self.core.main_window_id().unwrap(),
                    new_id,
                    None,
                    None,
                    None,
                );
                settings.positioner.size_limits = Limits::NONE
                    .max_width(620.0)
                    .min_width(560.0)
                    .min_height(200.0)
                    .max_height(900.0);
                return Task::batch([get_popup(settings), self.refresh()]);
            }
        }
        Task::none()
    }

    fn view(&self) -> Element<'_, Message> {
        let label = match &self.current {
            Some(s) if self.available => format!("{:.1} W", s.battery.watts()),
            _ => "?? W".to_string(),
        };
        let button = self
            .core
            .applet
            .text(label)
            // Keep the watts on one line. Without this the label wraps and the
            // " W" drops to a clipped second line, showing only the top of the
            // "W" as three dots under the number.
            .wrapping(cosmic::iced::widget::text::Wrapping::None)
            .apply(widget::button::custom)
            .class(cosmic::theme::Button::AppletIcon)
            .on_press(Message::TogglePopup);
        // Wrap in autosize_window so the panel surface sizes to the text width.
        // The framework calls view() raw (no auto-wrapping), so without this the
        // button is pinned to an icon-square width and the "W" gets clipped.
        self.core.applet.autosize_window(button).into()
    }

    fn view_window(&self, _id: Id) -> Element<'_, Message> {
        let (from, _to, seconds) = self.range();

        let data = ChartData {
            samples: self.samples.clone(),
            events: self.events.clone(),
            from,
            seconds,
            drag: self.drag.map(|d| (d.start, d.end)),
        };
        let hover_for = |id: GraphId| match self.hover {
            Some((hid, x)) if hid == id => Some(x),
            _ => None,
        };

        // Stats header.
        let header: Element<'_, Message> = match &self.current {
            Some(s) if self.available => {
                let left = widget::Column::new()
                    .width(Length::Fill)
                    .push(widget::text::title3(format!("{:.1} W", s.battery.watts())))
                    .push(widget::text::body(s.battery.status.clone()));
                let right = widget::Column::new()
                    .align_x(cosmic::iced::Alignment::End)
                    .push(widget::text::title3(format!("{}%", s.battery.capacity_pct)))
                    .push(widget::text::body(format!("Brightness {}%", s.backlight.brightness_pct())));
                widget::Row::new()
                    .push(left)
                    .push(right)
                    .width(Length::Fill)
                    .into()
            }
            _ => widget::text::body("Daemon unavailable. Is power-monitor-daemon running?").into(),
        };

        // Nav row: back button + range presets. A flex_row wraps the buttons
        // onto additional lines when they don't all fit the popup width,
        // instead of overflowing the window and clipping the rightmost presets
        // (e.g. "7d", and "24h" too once the back button appears).
        let mut nav_items: Vec<Element<'_, Message>> = Vec::new();
        if self.show_back() {
            nav_items.push(
                widget::button::text("◀")
                    .on_press(Message::ZoomBack)
                    .class(cosmic::theme::Button::Standard)
                    .into(),
            );
        }
        for (i, (label, _)) in TIME_RANGES.iter().enumerate() {
            let selected = self.custom.is_none() && self.range_idx == i;
            nav_items.push(
                widget::button::text(*label)
                    .on_press(Message::SelectRange(i))
                    .class(if selected {
                        cosmic::theme::Button::Suggested
                    } else {
                        cosmic::theme::Button::Standard
                    })
                    .into(),
            );
        }
        let nav = widget::flex_row(nav_items).spacing(4).width(Length::Fill);

        let battery = widget::canvas(BatteryChart {
            data: data.clone(),
            hover: hover_for(GraphId::Battery),
        })
        .width(Length::Fixed(GRAPH_W))
        .height(Length::Fixed(GRAPH_H));

        let energy = widget::canvas(EnergyChart {
            data,
            hover: hover_for(GraphId::Energy),
        })
        .width(Length::Fixed(GRAPH_W))
        .height(Length::Fixed(GRAPH_H));

        let mut content = widget::Column::new()
            .spacing(8)
            .push(header)
            .push(widget::divider::horizontal::default())
            .push(nav);

        // Custom-range label when zoomed.
        if let Some((f, t)) = self.custom {
            content = content.push(widget::text::caption(format!(
                "{} – {}",
                fmt_range(f),
                fmt_range(t)
            )));
        }

        content = content.push(battery).push(energy);

        // popup_container() autosizes content but hard-caps width at 360px,
        // which clipped the graphs and forced the nav buttons to wrap. Override
        // its limits so the popup can size up to our wider graph (GRAPH_W).
        self.core
            .applet
            .popup_container(padded_control(content))
            .limits(
                Limits::NONE
                    .min_width(360.0)
                    .max_width(620.0)
                    .min_height(1.0)
                    .max_height(1000.0),
            )
            .into()
    }

    fn style(&self) -> Option<cosmic::iced::theme::Style> {
        Some(cosmic::applet::style())
    }
}

fn fmt_range(epoch: i64) -> String {
    use chrono::{Datelike, TimeZone, Timelike};
    let d = chrono::Local
        .timestamp_opt(epoch, 0)
        .single()
        .unwrap_or_else(|| chrono::Local.timestamp_opt(0, 0).unwrap());
    const MON: [&str; 12] = [
        "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
    ];
    format!(
        "{} {} {}:{:02}",
        MON[(d.month0()) as usize],
        d.day(),
        d.hour(),
        d.minute()
    )
}
