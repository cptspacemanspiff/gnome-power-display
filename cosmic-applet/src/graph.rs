// Battery-level and energy-usage charts, drawn with iced's canvas.
//
// This is a port of the GNOME extension's graphUtils.js + graphs.js (Cairo)
// to cosmic::widget::canvas. The two charts share drawing/aggregation helpers
// and emit mouse interactions (hover + drag-to-zoom) as app Messages.

use std::rc::Rc;

use chrono::{Datelike, TimeZone, Timelike};
use cosmic::iced::alignment::Vertical;
use cosmic::iced::{Color, Pixels, Point, Rectangle, Size, mouse};
use cosmic::widget::canvas::{Action, Event, Frame, Geometry, LineDash, Program, Stroke, Style, Text, path};

use crate::app::{GraphId, Message};
use crate::dbus::{HistSample, PowerEvent};

// ---- Geometry ----

pub const GRAPH_W: f32 = 420.0;
pub const GRAPH_H: f32 = 120.0;
const M_TOP: f32 = 18.0;
const M_RIGHT: f32 = 32.0;
const M_BOTTOM: f32 = 16.0;
const M_LEFT: f32 = 8.0;
/// Plot-area width (inside margins).
pub const GW: f32 = GRAPH_W - M_LEFT - M_RIGHT;
const GH: f32 = GRAPH_H - M_TOP - M_BOTTOM;

/// Gaps longer than this (seconds) break the line / mark a no-data region.
const GAP_THRESHOLD: i64 = 30;

// ---- Colors (match graphUtils.js) ----

const COL_BG: Color = Color::from_rgba(0.12, 0.12, 0.12, 0.9);
const COL_GRID: Color = Color::from_rgba(1.0, 1.0, 1.0, 0.08);
const COL_AXIS: Color = Color::from_rgba(1.0, 1.0, 1.0, 0.25);
const COL_LABEL: Color = Color::from_rgba(1.0, 1.0, 1.0, 0.5);
const COL_TITLE: Color = Color::from_rgba(1.0, 1.0, 1.0, 0.7);
const COL_SLEEP_BG: Color = Color::from_rgba(0.3, 0.35, 0.55, 0.35);
const COL_SLEEP_EDGE: Color = Color::from_rgba(0.45, 0.5, 0.75, 0.5);
const COL_SLEEP_LBL: Color = Color::from_rgba(0.65, 0.7, 0.9, 0.6);
const COL_SHUTDOWN_BG: Color = Color::from_rgba(0.55, 0.3, 0.3, 0.35);
const COL_SHUTDOWN_EDGE: Color = Color::from_rgba(0.75, 0.45, 0.45, 0.5);
const COL_SHUTDOWN_LBL: Color = Color::from_rgba(0.9, 0.65, 0.65, 0.6);
const COL_GREEN: Color = Color::from_rgba(0.30, 0.75, 0.40, 1.0);
const COL_GREEN_FILL: Color = Color::from_rgba(0.30, 0.75, 0.40, 0.25);
const COL_GREEN_CHG: Color = Color::from_rgba(0.30, 0.75, 0.40, 0.45);
const COL_BLUE: Color = Color::from_rgba(0.35, 0.55, 0.90, 1.0);

// ---- Shared chart inputs ----

/// Data + view state shared by both charts, built fresh each `view`.
#[derive(Clone)]
pub struct ChartData {
    pub samples: Rc<Vec<HistSample>>,
    pub events: Rc<Vec<PowerEvent>>,
    pub from: i64,
    pub seconds: i64,
    /// Active drag selection in pixels (start, end), shown on both charts.
    pub drag: Option<(f32, f32)>,
}

pub struct BatteryChart {
    pub data: ChartData,
    /// Hover x in pixels, only set on the chart the cursor is over.
    pub hover: Option<f32>,
}

pub struct EnergyChart {
    pub data: ChartData,
    pub hover: Option<f32>,
}

/// Per-canvas interaction state: whether the left button is held (dragging).
#[derive(Default)]
pub struct ChartState {
    pressed: bool,
}

// ---- Coordinate mapping ----

fn x_of(t: i64, from: i64, seconds: i64) -> f32 {
    M_LEFT + ((t - from) as f32 / seconds.max(1) as f32) * GW
}

fn y_of_pct(pct: f32) -> f32 {
    M_TOP + GH - (pct / 100.0) * GH
}

fn local(epoch: i64) -> chrono::DateTime<chrono::Local> {
    chrono::Local
        .timestamp_opt(epoch, 0)
        .single()
        .unwrap_or_else(|| chrono::Local.timestamp_opt(0, 0).unwrap())
}

// ---- Aggregation helpers (port of graphUtils.js) ----

fn bucket_seconds(range: i64) -> i64 {
    match range {
        r if r <= 600 => 15,
        r if r <= 1800 => 30,
        r if r <= 3600 => 60,
        r if r <= 10800 => 300,
        r if r <= 21600 => 600,
        r if r <= 86400 => 900,
        _ => 3600,
    }
}

fn overlaps_sleep(events: &[PowerEvent], start: i64, end: i64) -> bool {
    events
        .iter()
        .any(|e| start < e.end_time && end > e.start_time)
}

struct Gap {
    start: i64,
    end: i64,
}

fn no_data_gaps(samples: &[HistSample], from: i64, events: &[PowerEvent]) -> Vec<Gap> {
    let mut gaps = Vec::new();
    if samples.is_empty() {
        return gaps;
    }
    if samples[0].timestamp - from > GAP_THRESHOLD
        && !overlaps_sleep(events, from, samples[0].timestamp)
    {
        gaps.push(Gap {
            start: from,
            end: samples[0].timestamp,
        });
    }
    for w in samples.windows(2) {
        let dt = w[1].timestamp - w[0].timestamp;
        if dt > GAP_THRESHOLD && !overlaps_sleep(events, w[0].timestamp, w[1].timestamp) {
            gaps.push(Gap {
                start: w[0].timestamp,
                end: w[1].timestamp,
            });
        }
    }
    gaps
}

/// Split samples into contiguous segments, breaking at gaps > GAP_THRESHOLD.
fn segments(samples: &[HistSample]) -> Vec<&[HistSample]> {
    let mut out = Vec::new();
    if samples.is_empty() {
        return out;
    }
    let mut seg_start = 0;
    for i in 1..samples.len() {
        if samples[i].timestamp - samples[i - 1].timestamp > GAP_THRESHOLD {
            out.push(&samples[seg_start..i]);
            seg_start = i;
        }
    }
    out.push(&samples[seg_start..]);
    out
}

struct Bucket {
    start: i64,
    sum_power: i128,
    count: u32,
    charging: bool,
}

fn bucketize(samples: &[HistSample], from: i64, to: i64, bucket_sec: i64) -> Vec<Bucket> {
    let n = (((to - from) as f64) / bucket_sec as f64).ceil().max(0.0) as usize;
    let mut buckets: Vec<Bucket> = (0..n)
        .map(|i| Bucket {
            start: from + i as i64 * bucket_sec,
            sum_power: 0,
            count: 0,
            charging: false,
        })
        .collect();
    for s in samples {
        let idx = ((s.timestamp - from) as f64 / bucket_sec as f64).floor() as i64;
        if idx >= 0 && (idx as usize) < buckets.len() {
            let b = &mut buckets[idx as usize];
            b.sum_power += s.power_uw as i128;
            b.count += 1;
            if s.charging() {
                b.charging = true;
            }
        }
    }
    buckets
}

fn nearest_sample(samples: &[HistSample], ts: i64) -> Option<&HistSample> {
    if samples.is_empty() {
        return None;
    }
    let idx = samples.partition_point(|s| s.timestamp < ts);
    if idx == 0 {
        return samples.first();
    }
    if idx >= samples.len() {
        return samples.last();
    }
    let prev = &samples[idx - 1];
    let cur = &samples[idx];
    if (prev.timestamp - ts).abs() < (cur.timestamp - ts).abs() {
        Some(prev)
    } else {
        Some(cur)
    }
}

// ---- Drawing helpers ----

fn text(frame: &mut Frame, content: impl Into<String>, x: f32, y: f32, color: Color, size: f32) {
    frame.fill_text(Text {
        content: content.into(),
        position: Point::new(x, y),
        color,
        size: Pixels(size),
        align_y: Vertical::Bottom,
        ..Default::default()
    });
}

fn line(frame: &mut Frame, x1: f32, y1: f32, x2: f32, y2: f32, color: Color, width: f32) {
    let mut b = path::Builder::new();
    b.move_to(Point::new(x1, y1));
    b.line_to(Point::new(x2, y2));
    frame.stroke(
        &b.build(),
        Stroke {
            style: Style::Solid(color),
            width,
            ..Default::default()
        },
    );
}

fn rect(frame: &mut Frame, x: f32, y: f32, w: f32, h: f32, color: Color) {
    frame.fill_rectangle(Point::new(x, y), Size::new(w, h), color);
}

fn draw_background(frame: &mut Frame) {
    rect(frame, 0.0, 0.0, GRAPH_W, GRAPH_H, COL_BG);
}

fn draw_title(frame: &mut Frame, title: &str) {
    text(frame, title, M_LEFT + 2.0, 13.0, COL_TITLE, 10.0);
}

fn draw_mid_grid(frame: &mut Frame) {
    line(frame, M_LEFT, M_TOP + GH / 2.0, M_LEFT + GW, M_TOP + GH / 2.0, COL_GRID, 0.5);
}

fn draw_bottom_axis(frame: &mut Frame) {
    line(frame, M_LEFT, M_TOP + GH, M_LEFT + GW, M_TOP + GH, COL_AXIS, 0.5);
}

fn draw_y_labels(frame: &mut Frame, labels: &[(&str, f32)]) {
    for (s, yfrac) in labels {
        let bump = if *yfrac == 0.0 {
            8.0
        } else if *yfrac == 1.0 {
            0.0
        } else {
            3.0
        };
        let y = M_TOP + yfrac * GH + bump;
        text(frame, *s, M_LEFT + GW + 4.0, y, COL_LABEL, 8.0);
    }
}

fn draw_no_data_message(frame: &mut Frame) {
    text(frame, "No data", GRAPH_W / 2.0 - 20.0, M_TOP + GH / 2.0, Color::from_rgba(1.0, 1.0, 1.0, 0.4), 9.0);
}

fn draw_time_axis(frame: &mut Frame, from: i64, range: i64) {
    let (step, fmt): (i64, fn(i64) -> String) = if range <= 3600 {
        (600, |t| {
            let d = local(t);
            format!("{}:{:02}", d.hour(), d.minute())
        })
    } else if range <= 10800 {
        (1800, |t| {
            let d = local(t);
            format!("{}:{:02}", d.hour(), d.minute())
        })
    } else if range <= 21600 {
        (3600, |t| format!("{}:00", local(t).hour()))
    } else if range <= 86400 {
        (10800, |t| {
            let h = local(t).hour();
            if h == 0 {
                "12 AM".into()
            } else if h == 12 {
                "12 PM".into()
            } else if h < 12 {
                format!("{h}")
            } else {
                format!("{}", h - 12)
            }
        })
    } else {
        (86400, |t| {
            const D: [&str; 7] = ["S", "M", "T", "W", "T", "F", "S"];
            D[local(t).weekday().num_days_from_sunday() as usize].into()
        })
    };

    let mut t = ((from as f64 / step as f64).ceil() as i64) * step;
    while t < from + range {
        let x = x_of(t, from, range);
        line(frame, x, M_TOP, x, M_TOP + GH, COL_GRID, 0.5);
        text(frame, fmt(t), x - 8.0, M_TOP + GH + 12.0, COL_LABEL, 8.0);
        t += step;
    }
}

fn draw_sleep_regions(frame: &mut Frame, from: i64, range: i64, events: &[PowerEvent], bucket_sec: Option<i64>) {
    for e in events {
        let (mut s, mut end) = (e.start_time, e.end_time);
        if let Some(b) = bucket_sec {
            s = ((s - from) as f64 / b as f64).floor() as i64 * b + from;
            end = ((end - from) as f64 / b as f64).ceil() as i64 * b + from;
        }
        let x1 = x_of(s, from, range).max(M_LEFT);
        let x2 = x_of(end, from, range).min(M_LEFT + GW);
        if x2 <= x1 {
            continue;
        }
        let shutdown = e.kind == "shutdown";
        rect(frame, x1, M_TOP, x2 - x1, GH, if shutdown { COL_SHUTDOWN_BG } else { COL_SLEEP_BG });
        let edge = if shutdown { COL_SHUTDOWN_EDGE } else { COL_SLEEP_EDGE };
        line(frame, x1, M_TOP, x1, M_TOP + GH, edge, 0.5);
        line(frame, x2, M_TOP, x2, M_TOP + GH, edge, 0.5);
        if x2 - x1 > 28.0 {
            let label = match e.kind.as_str() {
                "hibernate" => "Hibernate",
                "suspend-then-hibernate" => "S2H",
                "shutdown" => "Shutdown",
                _ => "Sleep",
            };
            let lbl_col = if shutdown { COL_SHUTDOWN_LBL } else { COL_SLEEP_LBL };
            let lx = x1 + (x2 - x1) / 2.0 - (label.len() as f32 * 2.5);
            text(frame, label, lx, M_TOP + GH / 2.0 + 3.0, lbl_col, 7.0);
        }
    }
}

fn draw_no_data_regions(frame: &mut Frame, from: i64, range: i64, samples: &[HistSample], events: &[PowerEvent]) {
    for gap in no_data_gaps(samples, from, events) {
        let x1 = x_of(gap.start, from, range).max(M_LEFT);
        let x2 = x_of(gap.end, from, range).min(M_LEFT + GW);
        if x2 - x1 < 2.0 {
            continue;
        }
        let region = Rectangle {
            x: x1,
            y: M_TOP,
            width: x2 - x1,
            height: GH,
        };
        frame.with_clip(region, |f| {
            rect(f, x1, M_TOP, x2 - x1, GH, Color::from_rgba(1.0, 1.0, 1.0, 0.04));
            let hatch = Color::from_rgba(1.0, 1.0, 1.0, 0.06);
            let mut x = x1 - GH;
            while x < x2 + GH {
                line(f, x, M_TOP + GH, x + GH, M_TOP, hatch, 0.5);
                x += 6.0;
            }
        });
        if x2 - x1 > 36.0 {
            text(frame, "No data", x1 + (x2 - x1) / 2.0 - 14.0, M_TOP + GH / 2.0 + 3.0, Color::from_rgba(1.0, 1.0, 1.0, 0.3), 7.0);
        }
    }
}

fn draw_hover_line(frame: &mut Frame, hx: f32) {
    if hx < M_LEFT || hx > M_LEFT + GW {
        return;
    }
    let mut b = path::Builder::new();
    b.move_to(Point::new(hx, M_TOP));
    b.line_to(Point::new(hx, M_TOP + GH));
    frame.stroke(
        &b.build(),
        Stroke {
            style: Style::Solid(Color::from_rgba(1.0, 1.0, 1.0, 0.5)),
            width: 0.5,
            line_dash: LineDash {
                segments: &[3.0, 3.0],
                offset: 0,
            },
            ..Default::default()
        },
    );
}

fn draw_hover_tooltip(frame: &mut Frame, hx: f32, lines: &[String]) {
    let right_half = hx > M_LEFT + GW / 2.0;
    let padding = 6.0;
    let max_w = lines
        .iter()
        .map(|l| l.len() as f32 * 5.5)
        .fold(0.0_f32, f32::max);
    let box_w = max_w + padding * 2.0;
    let box_h = lines.len() as f32 * 13.0 + padding * 2.0 - 4.0;
    let box_x = if right_half { hx - box_w - 8.0 } else { hx + 8.0 };
    let box_y = M_TOP + 4.0;

    rect(frame, box_x, box_y, box_w, box_h, Color::from_rgba(0.1, 0.1, 0.1, 0.85));
    // border
    let mut b = path::Builder::new();
    b.rectangle(Point::new(box_x, box_y), Size::new(box_w, box_h));
    frame.stroke(
        &b.build(),
        Stroke {
            style: Style::Solid(Color::from_rgba(1.0, 1.0, 1.0, 0.2)),
            width: 0.5,
            ..Default::default()
        },
    );
    for (i, l) in lines.iter().enumerate() {
        text(
            frame,
            l.clone(),
            box_x + padding,
            box_y + padding + 9.0 + i as f32 * 13.0,
            Color::from_rgba(1.0, 1.0, 1.0, 0.9),
            9.0,
        );
    }
}

fn draw_selection_overlay(frame: &mut Frame, drag: Option<(f32, f32)>) {
    let Some((a, b)) = drag else { return };
    let x1 = a.min(b).max(M_LEFT);
    let x2 = a.max(b).min(M_LEFT + GW);
    if x2 <= x1 {
        return;
    }
    let dim = Color::from_rgba(0.0, 0.0, 0.0, 0.4);
    rect(frame, M_LEFT, M_TOP, x1 - M_LEFT, GH, dim);
    rect(frame, x2, M_TOP, M_LEFT + GW - x2, GH, dim);
    let mut p = path::Builder::new();
    p.rectangle(Point::new(x1, M_TOP), Size::new(x2 - x1, GH));
    frame.stroke(
        &p.build(),
        Stroke {
            style: Style::Solid(Color::from_rgba(1.0, 1.0, 1.0, 0.5)),
            width: 1.0,
            ..Default::default()
        },
    );
}

// ---- Mouse handling (shared by both charts) ----

fn handle_event(
    id: GraphId,
    state: &mut ChartState,
    event: &Event,
    bounds: Rectangle,
    cursor: mouse::Cursor,
) -> Option<Action<Message>> {
    use mouse::{Button, Event as Me};
    let Event::Mouse(me) = event else { return None };
    match me {
        Me::ButtonPressed(Button::Left) => cursor.position_in(bounds).map(|p| {
            state.pressed = true;
            Action::publish(Message::GraphPress(p.x)).and_capture()
        }),
        Me::ButtonReleased(Button::Left) => {
            if state.pressed {
                state.pressed = false;
                Some(Action::publish(Message::GraphRelease).and_capture())
            } else {
                None
            }
        }
        Me::CursorMoved { .. } => {
            if state.pressed {
                // Track even outside bounds so the selection can extend.
                cursor
                    .position()
                    .map(|p| Action::publish(Message::GraphMove(id, Some(p.x - bounds.x))).and_capture())
            } else {
                let x = cursor.position_in(bounds).map(|p| p.x);
                Some(Action::publish(Message::GraphMove(id, x)))
            }
        }
        Me::CursorLeft if !state.pressed => Some(Action::publish(Message::GraphMove(id, None))),
        _ => None,
    }
}

// ---- Battery level chart ----

impl Program<Message, cosmic::Theme> for BatteryChart {
    type State = ChartState;

    fn update(&self, state: &mut ChartState, event: &Event, bounds: Rectangle, cursor: mouse::Cursor) -> Option<Action<Message>> {
        handle_event(GraphId::Battery, state, event, bounds, cursor)
    }

    fn draw(&self, _state: &ChartState, renderer: &cosmic::Renderer, _theme: &cosmic::Theme, bounds: Rectangle, _cursor: mouse::Cursor) -> Vec<Geometry> {
        let mut frame = Frame::new(renderer, bounds.size());
        let d = &self.data;
        let (from, seconds) = (d.from, d.seconds);

        draw_background(&mut frame);
        draw_title(&mut frame, "Battery Level");
        draw_y_labels(&mut frame, &[("100%", 0.0), ("50%", 0.5), ("0%", 1.0)]);
        draw_mid_grid(&mut frame);
        draw_time_axis(&mut frame, from, seconds);
        draw_sleep_regions(&mut frame, from, seconds, &d.events, None);

        let samples = d.samples.as_slice();
        if !samples.is_empty() {
            draw_no_data_regions(&mut frame, from, seconds, samples, &d.events);
            let segs = segments(samples);

            // Charging indicator bar just below the axis.
            let bar_y = M_TOP + GH + 1.0;
            for seg in &segs {
                for w in seg.windows(2) {
                    if w[0].charging() && !overlaps_sleep(&d.events, w[0].timestamp, w[1].timestamp) {
                        let x1 = x_of(w[0].timestamp, from, seconds);
                        let x2 = x_of(w[1].timestamp, from, seconds);
                        rect(&mut frame, x1, bar_y, x2 - x1, 4.0, COL_GREEN_CHG);
                    }
                }
            }

            // Filled area + line per segment.
            for seg in &segs {
                if seg.len() < 2 {
                    continue;
                }
                let mut area = path::Builder::new();
                area.move_to(Point::new(x_of(seg[0].timestamp, from, seconds), M_TOP + GH));
                for s in *seg {
                    area.line_to(Point::new(x_of(s.timestamp, from, seconds), y_of_pct(s.capacity_pct as f32)));
                }
                area.line_to(Point::new(x_of(seg[seg.len() - 1].timestamp, from, seconds), M_TOP + GH));
                area.close();
                frame.fill(&area.build(), COL_GREEN_FILL);

                let mut ln = path::Builder::new();
                ln.move_to(Point::new(x_of(seg[0].timestamp, from, seconds), y_of_pct(seg[0].capacity_pct as f32)));
                for s in &seg[1..] {
                    ln.line_to(Point::new(x_of(s.timestamp, from, seconds), y_of_pct(s.capacity_pct as f32)));
                }
                frame.stroke(
                    &ln.build(),
                    Stroke {
                        style: Style::Solid(COL_GREEN),
                        width: 1.5,
                        ..Default::default()
                    },
                );
            }

            // Hover marker + tooltip.
            if let Some(hx) = self.hover {
                if d.drag.is_none() {
                    draw_hover_line(&mut frame, hx);
                    let ts = from + (((hx - M_LEFT) / GW) * seconds as f32) as i64;
                    if let Some(n) = nearest_sample(samples, ts) {
                        let sx = x_of(n.timestamp, from, seconds);
                        let sy = y_of_pct(n.capacity_pct as f32);
                        let mut dot = path::Builder::new();
                        dot.rectangle(Point::new(sx - 2.0, sy - 2.0), Size::new(4.0, 4.0));
                        frame.fill(&dot.build(), COL_GREEN);
                        let dt = local(n.timestamp);
                        draw_hover_tooltip(
                            &mut frame,
                            hx,
                            &[
                                format!("{}%  {:.1} W", n.capacity_pct, n.power_uw as f64 / 1e6),
                                format!("{}:{:02}", dt.hour(), dt.minute()),
                            ],
                        );
                    }
                }
            }
        } else {
            draw_no_data_message(&mut frame);
        }

        draw_bottom_axis(&mut frame);
        draw_selection_overlay(&mut frame, d.drag);
        vec![frame.into_geometry()]
    }
}

// ---- Energy usage bar chart ----

impl Program<Message, cosmic::Theme> for EnergyChart {
    type State = ChartState;

    fn update(&self, state: &mut ChartState, event: &Event, bounds: Rectangle, cursor: mouse::Cursor) -> Option<Action<Message>> {
        handle_event(GraphId::Energy, state, event, bounds, cursor)
    }

    fn draw(&self, _state: &ChartState, renderer: &cosmic::Renderer, _theme: &cosmic::Theme, bounds: Rectangle, _cursor: mouse::Cursor) -> Vec<Geometry> {
        let mut frame = Frame::new(renderer, bounds.size());
        let d = &self.data;
        let (from, seconds) = (d.from, d.seconds);
        let to = from + seconds;

        draw_background(&mut frame);
        draw_title(&mut frame, "Energy Usage");

        let samples = d.samples.as_slice();
        if samples.is_empty() {
            draw_no_data_message(&mut frame);
            return vec![frame.into_geometry()];
        }

        let b_sec = bucket_seconds(seconds);
        let buckets = bucketize(samples, from, to, b_sec);
        let n = buckets.len().max(1);

        // Y scale: round max average power up to a 5 W multiple.
        let max_avg = buckets
            .iter()
            .filter(|b| b.count > 0)
            .map(|b| b.sum_power as f64 / b.count as f64)
            .fold(0.0_f64, f64::max)
            .max(1.0);
        let max_w = ((max_avg / 1e6 / 5.0).ceil() * 5.0).max(5.0);
        let max_scale = max_w * 1e6;

        draw_y_labels(
            &mut frame,
            &[
                (&format!("{}W", max_w as i64), 0.0),
                (&format!("{}W", (max_w / 2.0).round() as i64), 0.5),
                ("0W", 1.0),
            ],
        );
        draw_mid_grid(&mut frame);
        draw_time_axis(&mut frame, from, seconds);
        draw_no_data_regions(&mut frame, from, seconds, samples, &d.events);
        draw_sleep_regions(&mut frame, from, seconds, &d.events, Some(b_sec));

        let gaps = no_data_gaps(samples, from, &d.events);
        let slot = GW / n as f32;
        let bar_w = (slot * 0.75).min(12.0);
        for (i, b) in buckets.iter().enumerate() {
            if b.count == 0 {
                continue;
            }
            let b_end = b.start + b_sec;
            if overlaps_sleep(&d.events, b.start, b_end) {
                continue;
            }
            if gaps.iter().any(|g| b.start < g.end && b_end > g.start) {
                continue;
            }
            let avg = b.sum_power as f64 / b.count as f64;
            let bar_h = (avg / max_scale) as f32 * GH;
            let x = M_LEFT + (i as f32 + 0.5) * slot - bar_w / 2.0;
            let y = M_TOP + GH - bar_h;
            rect(&mut frame, x, y, bar_w, bar_h, if b.charging { COL_GREEN } else { COL_BLUE });
        }

        // Hover tooltip over the bucket under the cursor.
        if let Some(hx) = self.hover {
            if d.drag.is_none() {
                draw_hover_line(&mut frame, hx);
                let idx = ((hx - M_LEFT) / (GW / n as f32)).floor() as i64;
                if idx >= 0 && (idx as usize) < buckets.len() {
                    let b = &buckets[idx as usize];
                    if b.count > 0 {
                        let avg = b.sum_power as f64 / b.count as f64 / 1e6;
                        let s = local(b.start);
                        let e = local(b.start + b_sec);
                        draw_hover_tooltip(
                            &mut frame,
                            hx,
                            &[
                                format!("{avg:.1} W avg"),
                                format!("{}:{:02} – {}:{:02}", s.hour(), s.minute(), e.hour(), e.minute()),
                            ],
                        );
                    }
                }
            }
        }

        draw_bottom_axis(&mut frame);
        draw_selection_overlay(&mut frame, d.drag);
        vec![frame.into_geometry()]
    }
}
