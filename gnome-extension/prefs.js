import Adw from 'gi://Adw';
import Gio from 'gi://Gio';
import Gtk from 'gi://Gtk';

import {ExtensionPreferences, gettext as _} from 'resource:///org/gnome/Shell/Extensions/js/extensions/prefs.js';

export default class PowerMonitorPreferences extends ExtensionPreferences {
    fillPreferencesWindow(window) {
        const settings = this.getSettings('org.gnome.shell.extensions.power-monitor');

        const page = new Adw.PreferencesPage({
            title: _('General'),
            icon_name: 'preferences-system-symbolic',
        });
        window.add(page);

        const group = new Adw.PreferencesGroup({
            title: _('Display Settings'),
        });
        page.add(group);

        // Refresh interval
        const refreshRow = new Adw.SpinRow({
            title: _('Refresh Interval'),
            subtitle: _('Seconds between panel updates'),
            adjustment: new Gtk.Adjustment({
                lower: 1,
                upper: 60,
                step_increment: 1,
                value: settings.get_int('refresh-interval'),
            }),
        });
        refreshRow.connect('notify::value', () => {
            settings.set_int('refresh-interval', refreshRow.value);
        });
        group.add(refreshRow);
    }
}
