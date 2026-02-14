import GObject from 'gi://GObject';
import St from 'gi://St';
import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import Clutter from 'gi://Clutter';

import {Extension, gettext as _} from 'resource:///org/gnome/shell/extensions/extension.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';

import {drawBatteryGraph, drawEnergyGraph} from './graphs.js';
import {setupDrag} from './dragHandler.js';

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

const TIME_RANGES = [
    {label: '15m', seconds: 900},
    {label: '1h',  seconds: 3600},
    {label: '3h',  seconds: 10800},
    {label: '6h',  seconds: 21600},
    {label: '24h', seconds: 86400},
    {label: '7d',  seconds: 604800},
];

const PowerMonitorIndicator = GObject.registerClass(
class PowerMonitorIndicator extends PanelMenu.Button {
    _init(ext) {
        super._init(0.0, _('Power Monitor'));
        this._ext = ext;
        this._settings = ext.getSettings('org.gnome.shell.extensions.power-monitor');
        this._rangeIdx = 1;
        this._customRange = null;
        this._rangeStack = [];
        this._graphData = null;
        this._sleepData = null;
        this._dragStart = null;
        this._dragEnd = null;
        this._hoverX = null;
        this._hoverSource = null;

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
            this._proxy = new PowerMonitorProxy(Gio.DBus.system, DBUS_NAME, DBUS_PATH);
        } catch (e) {
            log(`PowerMonitor: proxy error: ${e.message}`);
        }
    }

    _zoomTo(from, to) {
        this._rangeStack.push(this._customRange
            ? {from: this._customRange.from, to: this._customRange.to}
            : {preset: this._rangeIdx}
        );
        this._customRange = {from, to};
        this._timeButtons.forEach(b => { b.checked = false; });
        this._backBtn.opacity = 255; this._backBtn.reactive = true;
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
            this._rangeLabel.text = '';        } else {
            this._customRange = {from: prev.from, to: prev.to};
            this._updateRangeLabel();
        }
        const showBack = this._rangeStack.length > 0 || this._customRange !== null;
        this._backBtn.opacity = showBack ? 255 : 0;
        this._backBtn.reactive = showBack;
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

    _getTimeRange() {
        if (this._customRange)
            return {from: this._customRange.from, to: this._customRange.to, seconds: this._customRange.to - this._customRange.from};
        const r = TIME_RANGES[this._rangeIdx];
        const now = Math.floor(Date.now() / 1000);
        return {from: now - r.seconds, to: now, seconds: r.seconds};
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

        this._backBtn = new St.Button({
            label: '\u25C0',
            style_class: 'power-monitor-time-btn power-monitor-back-btn',
            opacity: 0,
            reactive: false,
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
                this._backBtn.opacity = 0; this._backBtn.reactive = false;
                this._rangeLabel.text = '';                this._refreshGraph();
            });
            this._timeButtons.push(btn);
            navBox.add_child(btn);
        }

        this._rangeLabel = new St.Label({
            text: '',
            style_class: 'power-monitor-range-label',
            x_expand: true,
            x_align: Clutter.ActorAlign.CENTER,
        });

        const navContainer = new St.BoxLayout({vertical: true, x_expand: true});
        navBox.x_expand = true;
        navContainer.add_child(navBox);
        navContainer.add_child(this._rangeLabel);

        const navItem = new PopupMenu.PopupBaseMenuItem({reactive: false});
        navItem.add_child(navContainer);
        this.menu.addMenuItem(navItem);

        // Battery level graph
        this._batteryGraphArea = new St.DrawingArea({
            style_class: 'power-monitor-graph-area',
            width: 380, height: 120, reactive: true,
        });
        this._batteryGraphArea.connect('repaint', (a) => {
            const hx = this._hoverSource === this._batteryGraphArea ? this._hoverX : null;
            drawBatteryGraph(a, this._graphData, this._sleepData, this._getTimeRange(), this._dragStart, this._dragEnd, hx);
        });
        setupDrag(this._batteryGraphArea, this);
        const batItem = new PopupMenu.PopupBaseMenuItem({reactive: false});
        batItem.add_child(this._batteryGraphArea);
        this.menu.addMenuItem(batItem);

        // Energy usage bar graph
        this._energyGraphArea = new St.DrawingArea({
            style_class: 'power-monitor-graph-area',
            width: 380, height: 120, reactive: true,
        });
        this._energyGraphArea.connect('repaint', (a) => {
            const hx = this._hoverSource === this._energyGraphArea ? this._hoverX : null;
            drawEnergyGraph(a, this._graphData, this._sleepData, this._getTimeRange(), this._dragStart, this._dragEnd, hx);
        });
        setupDrag(this._energyGraphArea, this);
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

        this._proxy.GetHistoryRemote(from, to, (result, error) => {
            if (error) return;
            try { this._graphData = JSON.parse(result[0]); } catch (e) { /* */ }
            this._batteryGraphArea.queue_repaint();
            this._energyGraphArea.queue_repaint();
        });
        this._proxy.GetSleepEventsRemote(from, to, (result, error) => {
            if (error) return;
            try { this._sleepData = JSON.parse(result[0]); } catch (e) { /* */ }
            this._batteryGraphArea.queue_repaint();
            this._energyGraphArea.queue_repaint();
        });
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
