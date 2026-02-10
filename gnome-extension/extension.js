import GObject from 'gi://GObject';
import St from 'gi://St';
import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import Clutter from 'gi://Clutter';
import Cairo from 'gi://cairo';

import {Extension, gettext as _} from 'resource:///org/gnome/shell/extensions/extension.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';

const DBUS_NAME = 'org.gnome.PowerMonitor';
const DBUS_PATH = '/org/gnome/PowerMonitor';
const DBUS_IFACE = 'org.gnome.PowerMonitor';

const PowerMonitorProxyIface = `
<node>
  <interface name="${DBUS_IFACE}">
    <method name="GetCurrentStats">
      <arg direction="out" type="s" name="json"/>
    </method>
    <method name="GetHistory">
      <arg direction="in" type="x" name="from_epoch"/>
      <arg direction="in" type="x" name="to_epoch"/>
      <arg direction="out" type="s" name="json"/>
    </method>
    <method name="GetSleepEvents">
      <arg direction="in" type="x" name="from_epoch"/>
      <arg direction="in" type="x" name="to_epoch"/>
      <arg direction="out" type="s" name="json"/>
    </method>
  </interface>
</node>`;

const PowerMonitorProxy = Gio.DBusProxy.makeProxyWrapper(PowerMonitorProxyIface);

// Bucket size adapts to visible range
function bucketSeconds(rangeSeconds) {
    if (rangeSeconds <= 600)    return 15;    // <=10m: 15s buckets
    if (rangeSeconds <= 1800)   return 30;    // <=30m: 30s buckets
    if (rangeSeconds <= 3600)   return 60;    // <=1h: 1m buckets
    if (rangeSeconds <= 10800)  return 300;   // <=3h: 5m buckets
    if (rangeSeconds <= 21600)  return 600;   // <=6h: 10m buckets
    if (rangeSeconds <= 86400)  return 900;   // <=24h: 15m buckets
    return 3600;                               // 7d: 1h buckets
}
const GAP_THRESHOLD  = 30;   // seconds — gaps longer than this break the line

const TIME_RANGES = [
    {label: '6h',  seconds: 21600},
    {label: '24h', seconds: 86400},
    {label: '7d',  seconds: 604800},
];

// Colors
const COL_BG         = [0.12, 0.12, 0.12, 0.9];
const COL_GRID       = [1, 1, 1, 0.08];
const COL_AXIS       = [1, 1, 1, 0.25];
const COL_LABEL      = [1, 1, 1, 0.5];
const COL_TITLE      = [1, 1, 1, 0.7];
const COL_SLEEP_BG   = [0.3, 0.35, 0.55, 0.35];
const COL_SLEEP_EDGE = [0.45, 0.5, 0.75, 0.5];
const COL_SLEEP_LBL  = [0.65, 0.7, 0.9, 0.6];
const COL_GREEN      = [0.30, 0.75, 0.40, 1.0];
const COL_GREEN_FILL = [0.30, 0.75, 0.40, 0.25];
const COL_GREEN_CHG  = [0.30, 0.75, 0.40, 0.45];
const COL_BLUE       = [0.35, 0.55, 0.90, 1.0];
const COL_ORANGE     = [0.95, 0.60, 0.20, 1.0];

const PowerMonitorIndicator = GObject.registerClass(
class PowerMonitorIndicator extends PanelMenu.Button {
    _init(ext) {
        super._init(0.0, _('Power Monitor'));
        this._ext = ext;
        this._settings = ext.getSettings('org.gnome.shell.extensions.power-monitor');
        this._rangeIdx = 1; // default 24h
        this._customRange = null; // {from, to} when zoomed
        this._rangeStack = [];    // previous ranges for back navigation
        this._graphData = null;
        this._sleepData = null;
        this._dragStart = null;   // drag selection state
        this._dragEnd = null;

        this._label = new St.Label({
            text: '-- W',
            y_align: Clutter.ActorAlign.CENTER,
            style_class: 'power-monitor-panel-button',
        });
        this.add_child(this._label);

        this._buildPopup();

        this._proxy = null;
        this._createProxy();

        this._startTimer();
        this._settingsChangedId = this._settings.connect('changed::refresh-interval', () => {
            this._startTimer();
        });
        this._refresh();
    }

    _startTimer() {
        if (this._timerId) {
            GLib.source_remove(this._timerId);
            this._timerId = null;
        }
        const interval = this._settings.get_int('refresh-interval');
        this._timerId = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, interval, () => {
            this._refresh();
            return GLib.SOURCE_CONTINUE;
        });
    }

    _createProxy() {
        try {
            this._proxy = new PowerMonitorProxy(Gio.DBus.session, DBUS_NAME, DBUS_PATH);
        } catch (e) {
            log(`PowerMonitor: proxy error: ${e.message}`);
        }
    }

    _setupDrag(area) {
        area.connect('button-press-event', (actor, event) => {
            if (event.get_button() !== 1) return Clutter.EVENT_PROPAGATE;
            const [x] = event.get_coords();
            const [ax] = actor.get_transformed_position();
            this._dragStart = x - ax;
            this._dragEnd = null;
            return Clutter.EVENT_STOP;
        });
        area.connect('motion-event', (actor, event) => {
            if (this._dragStart === null) return Clutter.EVENT_PROPAGATE;
            const [x] = event.get_coords();
            const [ax] = actor.get_transformed_position();
            this._dragEnd = x - ax;
            // Repaint both graphs to show selection overlay
            this._batteryGraphArea.queue_repaint();
            this._energyGraphArea.queue_repaint();
            return Clutter.EVENT_STOP;
        });
        area.connect('button-release-event', (actor, event) => {
            if (this._dragStart === null) return Clutter.EVENT_PROPAGATE;
            const [x] = event.get_coords();
            const [ax] = actor.get_transformed_position();
            this._dragEnd = x - ax;
            this._finishDrag(actor);
            return Clutter.EVENT_STOP;
        });
        area.connect('leave-event', () => {
            if (this._dragStart !== null && this._dragEnd !== null)
                this._finishDrag(area);
            else {
                this._dragStart = null;
                this._dragEnd = null;
            }
            return Clutter.EVENT_PROPAGATE;
        });
    }

    _finishDrag(area) {
        const start = this._dragStart;
        const end = this._dragEnd;
        this._dragStart = null;
        this._dragEnd = null;

        if (start === null || end === null) return;
        const minPx = Math.min(start, end);
        const maxPx = Math.max(start, end);
        if (maxPx - minPx < 10) return; // too small, ignore

        // Convert pixel positions to timestamps using widget allocation width
        const margin = {top: 18, right: 32, bottom: 16, left: 8};
        const width = area.get_width();
        const gw = width - margin.left - margin.right;
        const {from, seconds} = this._getTimeRange();

        const t1 = from + ((minPx - margin.left) / gw) * seconds;
        const t2 = from + ((maxPx - margin.left) / gw) * seconds;
        const now = Math.floor(Date.now() / 1000);
        const clampFrom = Math.max(from, Math.floor(t1));
        const clampTo = Math.min(now, Math.ceil(t2));

        if (clampTo - clampFrom < 60) return; // minimum 1 minute zoom

        this._zoomTo(clampFrom, clampTo);
    }

    _zoomTo(from, to) {
        // Push current range to stack for back navigation
        this._rangeStack.push(this._customRange
            ? {from: this._customRange.from, to: this._customRange.to}
            : {preset: this._rangeIdx}
        );
        this._customRange = {from, to};
        this._timeButtons.forEach(b => { b.checked = false; });
        this._backBtn.visible = true;
        this._updateRangeLabel();
        this._refreshGraph();
    }

    _zoomOut() {
        if (this._rangeStack.length === 0) return;
        const prev = this._rangeStack.pop();
        if (prev.preset !== undefined) {
            this._customRange = null;
            this._rangeIdx = prev.preset;
            this._timeButtons.forEach((b, j) => { b.checked = (j === prev.preset); });
            this._rangeLabel.text = '';
        } else {
            this._customRange = {from: prev.from, to: prev.to};
            this._updateRangeLabel();
        }
        this._backBtn.visible = this._rangeStack.length > 0 || this._customRange !== null;
        this._refreshGraph();
    }

    _updateRangeLabel() {
        if (!this._customRange) { this._rangeLabel.text = ''; return; }
        const fmt = (epoch) => {
            const d = new Date(epoch * 1000);
            const h = d.getHours();
            const m = d.getMinutes().toString().padStart(2, '0');
            const mon = d.toLocaleDateString(undefined, {month: 'short', day: 'numeric'});
            return `${mon} ${h}:${m}`;
        };
        this._rangeLabel.text = `${fmt(this._customRange.from)} - ${fmt(this._customRange.to)}`;
    }

    // Draw selection overlay during drag
    _drawSelectionOverlay(cr, margin, gw, gh) {
        if (this._dragStart === null || this._dragEnd === null) return;
        const x1 = Math.max(margin.left, Math.min(this._dragStart, this._dragEnd));
        const x2 = Math.min(margin.left + gw, Math.max(this._dragStart, this._dragEnd));
        if (x2 <= x1) return;
        // Dim outside selection
        cr.setSourceRGBA(0, 0, 0, 0.4);
        cr.rectangle(margin.left, margin.top, x1 - margin.left, gh);
        cr.fill();
        cr.rectangle(x2, margin.top, margin.left + gw - x2, gh);
        cr.fill();
        // Selection highlight border
        cr.setSourceRGBA(1, 1, 1, 0.5);
        cr.setLineWidth(1);
        cr.rectangle(x1, margin.top, x2 - x1, gh);
        cr.stroke();
    }

    _buildPopup() {
        // Stats row
        this._statsBox = new St.BoxLayout({vertical: false, style_class: 'power-monitor-stats'});
        this._powerLabel = new St.Label({text: '-- W', style_class: 'power-monitor-stat-big'});
        this._batteryLabel = new St.Label({text: '--%', style_class: 'power-monitor-stat-big'});
        this._statusLabel = new St.Label({text: '--', style_class: 'power-monitor-stat-status'});
        const leftStats = new St.BoxLayout({vertical: true, x_expand: true});
        leftStats.add_child(this._powerLabel);
        leftStats.add_child(this._statusLabel);
        const rightStats = new St.BoxLayout({vertical: true});
        rightStats.add_child(this._batteryLabel);
        this._brightnessLabel = new St.Label({text: '', style_class: 'power-monitor-stat-status'});
        rightStats.add_child(this._brightnessLabel);
        this._statsBox.add_child(leftStats);
        this._statsBox.add_child(rightStats);
        const statsItem = new PopupMenu.PopupBaseMenuItem({reactive: false});
        statsItem.add_child(this._statsBox);
        this.menu.addMenuItem(statsItem);

        // Time range selector row
        const navBox = new St.BoxLayout({style_class: 'power-monitor-time-selector'});

        // Back button (hidden when not zoomed)
        this._backBtn = new St.Button({
            label: '\u25C0',
            style_class: 'power-monitor-time-btn power-monitor-back-btn',
            visible: false,
        });
        this._backBtn.connect('clicked', () => this._zoomOut());
        navBox.add_child(this._backBtn);

        this._timeButtons = [];
        for (let i = 0; i < TIME_RANGES.length; i++) {
            const idx = i;
            const btn = new St.Button({
                label: TIME_RANGES[i].label,
                style_class: 'power-monitor-time-btn',
                toggle_mode: true,
                checked: i === this._rangeIdx,
            });
            btn.connect('clicked', () => {
                this._timeButtons.forEach((b, j) => { b.checked = (j === idx); });
                this._rangeIdx = idx;
                this._customRange = null;
                this._rangeStack = [];
                this._backBtn.visible = false;
                this._rangeLabel.text = '';
                this._refreshGraph();
            });
            this._timeButtons.push(btn);
            navBox.add_child(btn);
        }

        // Range label (shows zoomed time range)
        this._rangeLabel = new St.Label({
            text: '',
            style_class: 'power-monitor-range-label',
            x_expand: true,
            x_align: Clutter.ActorAlign.END,
        });
        navBox.add_child(this._rangeLabel);

        const navItem = new PopupMenu.PopupBaseMenuItem({reactive: false});
        navItem.add_child(navBox);
        this.menu.addMenuItem(navItem);

        // Battery level graph
        this._batteryGraphArea = new St.DrawingArea({
            style_class: 'power-monitor-graph-area',
            width: 380,
            height: 120,
            reactive: true,
        });
        this._batteryGraphArea.connect('repaint', (a) => this._drawBatteryGraph(a));
        this._setupDrag(this._batteryGraphArea);
        const batItem = new PopupMenu.PopupBaseMenuItem({reactive: false});
        batItem.add_child(this._batteryGraphArea);
        this.menu.addMenuItem(batItem);

        // Energy usage bar graph
        this._energyGraphArea = new St.DrawingArea({
            style_class: 'power-monitor-graph-area',
            width: 380,
            height: 120,
            reactive: true,
        });
        this._energyGraphArea.connect('repaint', (a) => this._drawEnergyGraph(a));
        this._setupDrag(this._energyGraphArea);
        const engItem = new PopupMenu.PopupBaseMenuItem({reactive: false});
        engItem.add_child(this._energyGraphArea);
        this.menu.addMenuItem(engItem);
    }

    _refresh() {
        if (!this._proxy) return;

        this._proxy.GetCurrentStatsRemote((result, error) => {
            if (error) { this._label.text = '?? W'; return; }
            try {
                const data = JSON.parse(result[0]);
                const bat = data.battery;
                const bl = data.backlight;
                if (bat) {
                    const watts = (bat.power_uw / 1e6).toFixed(1);
                    this._label.text = `${watts} W`;
                    this._powerLabel.text = `${watts} W`;
                    this._batteryLabel.text = `${bat.capacity_pct}%`;
                    this._statusLabel.text = bat.status;
                }
                if (bl) {
                    const pct = Math.round(bl.brightness / bl.max_brightness * 100);
                    this._brightnessLabel.text = `Brightness ${pct}%`;
                }
            } catch (e) {
                log(`PowerMonitor: parse error: ${e.message}`);
            }
        });
        this._refreshGraph();
    }

    _refreshGraph() {
        if (!this._proxy) return;
        const {from, to} = this._getTimeRange();
        const now = to;

        this._proxy.GetHistoryRemote(from, now, (result, error) => {
            if (error) return;
            try { this._graphData = JSON.parse(result[0]); } catch (e) { /* */ }
            this._batteryGraphArea.queue_repaint();
            this._energyGraphArea.queue_repaint();
        });
        this._proxy.GetSleepEventsRemote(from, now, (result, error) => {
            if (error) return;
            try { this._sleepData = JSON.parse(result[0]); } catch (e) { /* */ }
            this._batteryGraphArea.queue_repaint();
            this._energyGraphArea.queue_repaint();
        });
    }

    _getTimeRange() {
        if (this._customRange)
            return {from: this._customRange.from, to: this._customRange.to, seconds: this._customRange.to - this._customRange.from};
        const r = TIME_RANGES[this._rangeIdx];
        const now = Math.floor(Date.now() / 1000);
        return {from: now - r.seconds, to: now, seconds: r.seconds};
    }

    _bucketize(samples, from, to, bucketSec) {
        const buckets = [];
        const nBuckets = Math.ceil((to - from) / bucketSec);
        for (let i = 0; i < nBuckets; i++) {
            buckets.push({start: from + i * bucketSec, sumPower: 0, count: 0, charging: false});
        }
        for (const s of samples) {
            const idx = Math.floor((s.timestamp - from) / bucketSec);
            if (idx >= 0 && idx < nBuckets) {
                buckets[idx].sumPower += s.power_uw;
                buckets[idx].count++;
                if (s.status === 'Charging' || s.status === 'Full')
                    buckets[idx].charging = true;
            }
        }
        return buckets;
    }

    _isSleeping(timestamp) {
        if (!this._sleepData) return false;
        for (const evt of this._sleepData) {
            if (timestamp >= evt.sleep_time && timestamp <= evt.wake_time)
                return true;
        }
        return false;
    }

    _drawBackground(cr, width, height) {
        cr.setSourceRGBA(...COL_BG);
        cr.rectangle(0, 0, width, height);
        cr.fill();
    }

    _drawSleepRegions(cr, margin, gw, gh, from, rangeSeconds) {
        if (!this._sleepData || this._sleepData.length === 0) return;
        for (const evt of this._sleepData) {
            const x1 = Math.max(margin.left, margin.left + ((evt.sleep_time - from) / rangeSeconds) * gw);
            const x2 = Math.min(margin.left + gw, margin.left + ((evt.wake_time - from) / rangeSeconds) * gw);
            if (x2 <= x1) continue;
            cr.setSourceRGBA(...COL_SLEEP_BG);
            cr.rectangle(x1, margin.top, x2 - x1, gh);
            cr.fill();
            // Edge lines
            cr.setSourceRGBA(...COL_SLEEP_EDGE);
            cr.setLineWidth(0.5);
            for (const xv of [x1, x2]) {
                cr.moveTo(xv, margin.top);
                cr.lineTo(xv, margin.top + gh);
                cr.stroke();
            }
            // Label
            if (x2 - x1 > 28) {
                cr.setSourceRGBA(...COL_SLEEP_LBL);
                cr.setFontSize(7);
                const label = evt.type === 'hibernate' ? 'Hibernate' : 'Sleep';
                const lx = x1 + (x2 - x1) / 2 - (label.length * 2.5);
                cr.moveTo(lx, margin.top + gh / 2 + 3);
                cr.showText(label);
            }
        }
    }

    // Find gaps in sample data and draw hatched "no data" regions
    _drawNoDataRegions(cr, margin, gw, gh, from, rangeSeconds, samples) {
        if (!samples || samples.length === 0) return;
        const gaps = [];
        // Gap at the start if first sample is late
        if (samples[0].timestamp - from > GAP_THRESHOLD)
            gaps.push({start: from, end: samples[0].timestamp});
        for (let i = 1; i < samples.length; i++) {
            const dt = samples[i].timestamp - samples[i - 1].timestamp;
            if (dt > GAP_THRESHOLD && !this._isSleeping(samples[i - 1].timestamp))
                gaps.push({start: samples[i - 1].timestamp, end: samples[i].timestamp});
        }
        for (const gap of gaps) {
            const x1 = Math.max(margin.left, margin.left + ((gap.start - from) / rangeSeconds) * gw);
            const x2 = Math.min(margin.left + gw, margin.left + ((gap.end - from) / rangeSeconds) * gw);
            if (x2 - x1 < 2) continue;
            // Subtle diagonal hatch pattern
            cr.save();
            cr.rectangle(x1, margin.top, x2 - x1, gh);
            cr.clip();
            cr.setSourceRGBA(1, 1, 1, 0.04);
            cr.rectangle(x1, margin.top, x2 - x1, gh);
            cr.fill();
            cr.setSourceRGBA(1, 1, 1, 0.06);
            cr.setLineWidth(0.5);
            const step = 6;
            for (let x = x1 - gh; x < x2 + gh; x += step) {
                cr.moveTo(x, margin.top + gh);
                cr.lineTo(x + gh, margin.top);
                cr.stroke();
            }
            cr.restore();
            // "No data" label if wide enough
            if (x2 - x1 > 36) {
                cr.setSourceRGBA(1, 1, 1, 0.3);
                cr.setFontSize(7);
                cr.moveTo(x1 + (x2 - x1) / 2 - 14, margin.top + gh / 2 + 3);
                cr.showText('No data');
            }
        }
    }

    // Split samples into contiguous segments (breaking at gaps)
    _segmentSamples(samples) {
        if (!samples || samples.length === 0) return [];
        const segments = [];
        let seg = [samples[0]];
        for (let i = 1; i < samples.length; i++) {
            if (samples[i].timestamp - samples[i - 1].timestamp > GAP_THRESHOLD) {
                segments.push(seg);
                seg = [];
            }
            seg.push(samples[i]);
        }
        if (seg.length > 0) segments.push(seg);
        return segments;
    }

    _drawTimeAxis(cr, margin, gw, gh, from, rangeSeconds) {
        let stepSeconds, fmt;
        if (rangeSeconds <= 3600) {           // <=1h: label every 10 min
            stepSeconds = 600;
            fmt = (t) => {
                const d = new Date(t * 1000);
                return `${d.getHours()}:${d.getMinutes().toString().padStart(2, '0')}`;
            };
        } else if (rangeSeconds <= 10800) {   // <=3h: label every 30 min
            stepSeconds = 1800;
            fmt = (t) => {
                const d = new Date(t * 1000);
                return `${d.getHours()}:${d.getMinutes().toString().padStart(2, '0')}`;
            };
        } else if (rangeSeconds <= 21600) {   // <=6h: label every hour
            stepSeconds = 3600;
            fmt = (t) => { const d = new Date(t * 1000); return `${d.getHours()}:00`; };
        } else if (rangeSeconds <= 86400) {   // <=24h: label every 3h
            stepSeconds = 10800;
            fmt = (t) => {
                const d = new Date(t * 1000);
                const h = d.getHours();
                if (h === 0) return '12 AM';
                if (h === 12) return '12 PM';
                return h < 12 ? `${h}` : `${h - 12}`;
            };
        } else {                              // 7d: label every day
            stepSeconds = 86400;
            fmt = (t) => {
                const d = new Date(t * 1000);
                return ['S','M','T','W','T','F','S'][d.getDay()];
            };
        }

        cr.setSourceRGBA(...COL_LABEL);
        cr.setFontSize(8);
        // Align to step boundary
        let t = Math.ceil(from / stepSeconds) * stepSeconds;
        while (t < from + rangeSeconds) {
            const x = margin.left + ((t - from) / rangeSeconds) * gw;
            // Grid line
            cr.setSourceRGBA(...COL_GRID);
            cr.setLineWidth(0.5);
            cr.moveTo(x, margin.top);
            cr.lineTo(x, margin.top + gh);
            cr.stroke();
            // Label
            cr.setSourceRGBA(...COL_LABEL);
            cr.moveTo(x - 8, margin.top + gh + 12);
            cr.showText(fmt(t));
            t += stepSeconds;
        }
    }

    _drawBatteryGraph(area) {
        const cr = area.get_context();
        const [width, height] = area.get_surface_size();
        const margin = {top: 18, right: 32, bottom: 16, left: 8};
        const gw = width - margin.left - margin.right;
        const gh = height - margin.top - margin.bottom;
        const {from, to, seconds} = this._getTimeRange();

        this._drawBackground(cr, width, height);

        // Title
        cr.setSourceRGBA(...COL_TITLE);
        cr.setFontSize(10);
        cr.moveTo(margin.left + 2, 13);
        cr.showText('Battery Level');

        // Y-axis labels on right
        cr.setSourceRGBA(...COL_LABEL);
        cr.setFontSize(8);
        cr.moveTo(margin.left + gw + 4, margin.top + 8);
        cr.showText('100%');
        cr.moveTo(margin.left + gw + 4, margin.top + gh / 2 + 3);
        cr.showText('50%');
        cr.moveTo(margin.left + gw + 4, margin.top + gh);
        cr.showText('0%');

        // 50% grid line
        cr.setSourceRGBA(...COL_GRID);
        cr.setLineWidth(0.5);
        cr.moveTo(margin.left, margin.top + gh / 2);
        cr.lineTo(margin.left + gw, margin.top + gh / 2);
        cr.stroke();

        this._drawTimeAxis(cr, margin, gw, gh, from, seconds);
        this._drawSleepRegions(cr, margin, gw, gh, from, seconds);

        const samples = this._graphData?.battery;
        if (samples && samples.length > 0) {
            this._drawNoDataRegions(cr, margin, gw, gh, from, seconds, samples);
            const segments = this._segmentSamples(samples);

            // Charging indicator bar below axis
            const barY = margin.top + gh + 1;
            const barH = 4;
            for (const seg of segments) {
                for (let i = 1; i < seg.length; i++) {
                    if (seg[i - 1].status === 'Charging' || seg[i - 1].status === 'Full') {
                        const x1 = margin.left + ((seg[i - 1].timestamp - from) / seconds) * gw;
                        const x2 = margin.left + ((seg[i].timestamp - from) / seconds) * gw;
                        cr.setSourceRGBA(...COL_GREEN_CHG);
                        cr.rectangle(x1, barY, x2 - x1, barH);
                        cr.fill();
                    }
                }
            }

            // Draw each segment as separate filled area + line
            for (const seg of segments) {
                if (seg.length < 2) continue;
                const toX = (s) => margin.left + ((s.timestamp - from) / seconds) * gw;
                const toY = (s) => margin.top + gh - (s.capacity_pct / 100) * gh;

                // Filled area
                cr.moveTo(toX(seg[0]), margin.top + gh);
                for (const s of seg) cr.lineTo(toX(s), toY(s));
                cr.lineTo(toX(seg[seg.length - 1]), margin.top + gh);
                cr.closePath();
                cr.setSourceRGBA(...COL_GREEN_FILL);
                cr.fill();

                // Line
                cr.setSourceRGBA(...COL_GREEN);
                cr.setLineWidth(1.5);
                cr.moveTo(toX(seg[0]), toY(seg[0]));
                for (let i = 1; i < seg.length; i++) cr.lineTo(toX(seg[i]), toY(seg[i]));
                cr.stroke();
            }
        } else {
            cr.setSourceRGBA(1, 1, 1, 0.4);
            cr.moveTo(width / 2 - 20, margin.top + gh / 2);
            cr.showText('No data');
        }

        // Bottom axis line
        cr.setSourceRGBA(...COL_AXIS);
        cr.setLineWidth(0.5);
        cr.moveTo(margin.left, margin.top + gh);
        cr.lineTo(margin.left + gw, margin.top + gh);
        cr.stroke();

        this._drawSelectionOverlay(cr, margin, gw, gh);
        cr.$dispose();
    }

    _drawEnergyGraph(area) {
        const cr = area.get_context();
        const [width, height] = area.get_surface_size();
        const margin = {top: 18, right: 32, bottom: 16, left: 8};
        const gw = width - margin.left - margin.right;
        const gh = height - margin.top - margin.bottom;
        const {from, to, seconds} = this._getTimeRange();

        this._drawBackground(cr, width, height);

        // Title
        cr.setSourceRGBA(...COL_TITLE);
        cr.setFontSize(10);
        cr.moveTo(margin.left + 2, 13);
        cr.showText('Energy Usage');

        const samples = this._graphData?.battery;
        if (!samples || samples.length === 0) {
            cr.setSourceRGBA(1, 1, 1, 0.4);
            cr.moveTo(width / 2 - 20, margin.top + gh / 2);
            cr.showText('No data');
            cr.$dispose();
            return;
        }

        const buckets = this._bucketize(samples, from, to, bucketSeconds(seconds));
        const nBuckets = buckets.length;

        // Find max avg power for Y scale
        let maxAvg = 0;
        for (const b of buckets) {
            if (b.count > 0) {
                const avg = b.sumPower / b.count;
                maxAvg = Math.max(maxAvg, avg);
            }
        }
        if (maxAvg === 0) maxAvg = 1;
        // Round up to nice value in watts
        const maxW = Math.ceil(maxAvg / 1e6 / 5) * 5;
        const maxScale = maxW * 1e6;

        // Y-axis labels on right
        cr.setSourceRGBA(...COL_LABEL);
        cr.setFontSize(8);
        cr.moveTo(margin.left + gw + 4, margin.top + 8);
        cr.showText(`${maxW}W`);
        cr.moveTo(margin.left + gw + 4, margin.top + gh / 2 + 3);
        cr.showText(`${Math.round(maxW / 2)}W`);
        cr.moveTo(margin.left + gw + 4, margin.top + gh);
        cr.showText('0W');

        // Mid grid line
        cr.setSourceRGBA(...COL_GRID);
        cr.setLineWidth(0.5);
        cr.moveTo(margin.left, margin.top + gh / 2);
        cr.lineTo(margin.left + gw, margin.top + gh / 2);
        cr.stroke();

        this._drawTimeAxis(cr, margin, gw, gh, from, seconds);
        this._drawSleepRegions(cr, margin, gw, gh, from, seconds);
        this._drawNoDataRegions(cr, margin, gw, gh, from, seconds, samples);

        // Draw bars
        const slotWidth = gw / nBuckets;
        const maxBarWidth = 12;
        const barWidth = Math.min(slotWidth * 0.75, maxBarWidth);
        for (let i = 0; i < nBuckets; i++) {
            const b = buckets[i];
            if (b.count === 0) continue;
            const avg = b.sumPower / b.count;
            const barH = (avg / maxScale) * gh;
            const x = margin.left + (i + 0.5) * slotWidth - barWidth / 2;
            const y = margin.top + gh - barH;

            if (b.charging)
                cr.setSourceRGBA(...COL_GREEN);
            else
                cr.setSourceRGBA(...COL_BLUE);
            cr.rectangle(x, y, barWidth, barH);
            cr.fill();
        }

        // Bottom axis
        cr.setSourceRGBA(...COL_AXIS);
        cr.setLineWidth(0.5);
        cr.moveTo(margin.left, margin.top + gh);
        cr.lineTo(margin.left + gw, margin.top + gh);
        cr.stroke();

        this._drawSelectionOverlay(cr, margin, gw, gh);
        cr.$dispose();
    }

    destroy() {
        if (this._settingsChangedId) {
            this._settings.disconnect(this._settingsChangedId);
            this._settingsChangedId = null;
        }
        if (this._timerId) {
            GLib.source_remove(this._timerId);
            this._timerId = null;
        }
        super.destroy();
    }
});

export default class PowerMonitorExtension extends Extension {
    enable() {
        this._indicator = new PowerMonitorIndicator(this);
        Main.panel.addToStatusArea(this.uuid, this._indicator);
    }

    disable() {
        this._indicator?.destroy();
        this._indicator = null;
    }
}
