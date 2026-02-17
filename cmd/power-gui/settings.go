package main

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	pmconfig "github.com/cptspacemanspiff/gnome-power-display/internal/config"
)

type settingsPage struct {
	container *gtk.Box

	dbPathEntry       *gtk.Entry
	stateLogPathEntry *gtk.Entry

	intervalSpin      *gtk.SpinButton
	topProcessesSpin  *gtk.SpinButton
	wallClockSpin     *gtk.SpinButton
	powerAverageSpin  *gtk.SpinButton
	retentionDaysSpin *gtk.SpinButton
	cleanupHoursSpin  *gtk.SpinButton

	statusLabel *gtk.Label
}

func newSettingsPage() *settingsPage {
	p := &settingsPage{}

	p.container = gtk.NewBox(gtk.OrientationVertical, 12)
	p.container.SetMarginStart(24)
	p.container.SetMarginEnd(24)
	p.container.SetMarginTop(24)
	p.container.SetMarginBottom(24)

	header := adw.NewPreferencesGroup()
	header.SetTitle("Daemon Configuration")
	header.SetDescription("Load and update daemon config over D-Bus")
	p.container.Append(header)

	storageGroup := adw.NewPreferencesGroup()
	storageGroup.SetTitle("Storage")
	p.dbPathEntry = gtk.NewEntry()
	p.stateLogPathEntry = gtk.NewEntry()
	storageGroup.Add(makeEntryRow("Database Path", p.dbPathEntry))
	storageGroup.Add(makeEntryRow("State Log Path", p.stateLogPathEntry))
	p.container.Append(storageGroup)

	collectionGroup := adw.NewPreferencesGroup()
	collectionGroup.SetTitle("Collection")
	p.intervalSpin = newConfigSpin(1, 3600, 1)
	p.topProcessesSpin = newConfigSpin(1, 200, 1)
	p.wallClockSpin = newConfigSpin(1, 3600, 1)
	p.powerAverageSpin = newConfigSpin(1, 3600, 1)
	collectionGroup.Add(makeSpinRow("Interval (seconds)", p.intervalSpin))
	collectionGroup.Add(makeSpinRow("Top Processes", p.topProcessesSpin))
	collectionGroup.Add(makeSpinRow("Wall Clock Jump Threshold (seconds)", p.wallClockSpin))
	collectionGroup.Add(makeSpinRow("Power Average Window (seconds)", p.powerAverageSpin))
	p.container.Append(collectionGroup)

	cleanupGroup := adw.NewPreferencesGroup()
	cleanupGroup.SetTitle("Cleanup")
	p.retentionDaysSpin = newConfigSpin(1, 3650, 1)
	p.cleanupHoursSpin = newConfigSpin(1, 720, 1)
	cleanupGroup.Add(makeSpinRow("Retention (days)", p.retentionDaysSpin))
	cleanupGroup.Add(makeSpinRow("Cleanup Interval (hours)", p.cleanupHoursSpin))
	p.container.Append(cleanupGroup)

	actions := gtk.NewBox(gtk.OrientationHorizontal, 8)
	reloadBtn := gtk.NewButtonWithLabel("Reload")
	saveBtn := gtk.NewButtonWithLabel("Save")
	saveBtn.AddCSSClass("suggested-action")
	actions.Append(reloadBtn)
	actions.Append(saveBtn)
	p.container.Append(actions)

	p.statusLabel = gtk.NewLabel("")
	p.statusLabel.SetWrap(true)
	p.statusLabel.SetXAlign(0)
	p.statusLabel.AddCSSClass("dim-label")
	p.container.Append(p.statusLabel)

	reloadBtn.ConnectClicked(func() {
		p.loadConfig()
	})

	saveBtn.ConnectClicked(func() {
		if err := p.saveConfig(); err != nil {
			p.setStatus(err.Error())
			return
		}
		p.setStatus("Saved configuration via daemon D-Bus. Restart power-monitor-daemon to apply runtime changes.")
	})

	p.loadConfig()
	return p
}

func newConfigSpin(min, max, step float64) *gtk.SpinButton {
	spin := gtk.NewSpinButtonWithRange(min, max, step)
	spin.SetDigits(0)
	spin.SetNumeric(true)
	spin.SetHAlign(gtk.AlignEnd)
	spin.SetWidthChars(6)
	return spin
}

func makeSpinRow(title string, spin *gtk.SpinButton) *adw.ActionRow {
	row := adw.NewActionRow()
	row.SetTitle(title)
	row.AddSuffix(spin)
	return row
}

func makeEntryRow(title string, entry *gtk.Entry) *adw.ActionRow {
	row := adw.NewActionRow()
	row.SetTitle(title)
	entry.SetHExpand(true)
	entry.SetWidthChars(28)
	row.AddSuffix(entry)
	return row
}

func (p *settingsPage) loadConfig() {
	cfg, err := client.GetConfig()
	if err != nil {
		cfg = pmconfig.DefaultConfig()
		p.setStatus(fmt.Sprintf("Failed to load config over D-Bus. Showing defaults: %v", err))
	} else {
		p.setStatus("Loaded configuration from daemon via D-Bus")
	}
	p.applyConfig(cfg)
}

func (p *settingsPage) applyConfig(cfg *pmconfig.Config) {
	p.dbPathEntry.SetText(cfg.Storage.DBPath)
	p.stateLogPathEntry.SetText(cfg.Storage.StateLogPath)
	p.intervalSpin.SetValue(float64(cfg.Collection.IntervalSeconds))
	p.topProcessesSpin.SetValue(float64(cfg.Collection.TopProcesses))
	p.wallClockSpin.SetValue(float64(cfg.Collection.WallClockJumpThresholdSeconds))
	p.powerAverageSpin.SetValue(float64(cfg.Collection.PowerAverageSeconds))
	p.retentionDaysSpin.SetValue(float64(cfg.Cleanup.RetentionDays))
	p.cleanupHoursSpin.SetValue(float64(cfg.Cleanup.IntervalHours))
}

func (p *settingsPage) saveConfig() error {
	cfg := &pmconfig.Config{}
	cfg.Storage.DBPath = strings.TrimSpace(p.dbPathEntry.Text())
	cfg.Storage.StateLogPath = strings.TrimSpace(p.stateLogPathEntry.Text())
	cfg.Collection.IntervalSeconds = p.intervalSpin.ValueAsInt()
	cfg.Collection.TopProcesses = p.topProcessesSpin.ValueAsInt()
	cfg.Collection.WallClockJumpThresholdSeconds = p.wallClockSpin.ValueAsInt()
	cfg.Collection.PowerAverageSeconds = p.powerAverageSpin.ValueAsInt()
	cfg.Cleanup.RetentionDays = p.retentionDaysSpin.ValueAsInt()
	cfg.Cleanup.IntervalHours = p.cleanupHoursSpin.ValueAsInt()

	sanitized, err := pmconfig.NormalizeAndValidate(cfg)
	if err != nil {
		return err
	}

	updated, err := client.UpdateConfig(sanitized)
	if err != nil {
		return err
	}
	p.applyConfig(updated)
	return nil
}

func (p *settingsPage) setStatus(msg string) {
	p.statusLabel.SetLabel(msg)
}
