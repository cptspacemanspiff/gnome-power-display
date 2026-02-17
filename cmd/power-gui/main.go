package main

import (
	"log"
	"os"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

var (
	client       *dbusClient
	stats        *statsBar
	battGraph    *batteryGraph
	energyGr     *energyGraph
	selectedRange int = 3 // default 6h
)

func main() {
	app := adw.NewApplication("org.gnome.PowerMonitorGUI", gio.ApplicationFlagsNone)
	app.ConnectActivate(func() { activate(app) })
	if code := app.Run(os.Args); code > 0 {
		os.Exit(code)
	}
}

func activate(app *adw.Application) {
	var err error
	client, err = newDBusClient()
	if err != nil {
		log.Fatalf("Failed to connect to D-Bus: %v", err)
	}

	win := adw.NewApplicationWindow(&app.Application)
	win.SetTitle("Power Monitor")
	win.SetDefaultSize(900, 600)

	loadCSS()

	// Build overview page
	stats = newStatsBar()
	battGraph = newBatteryGraph()
	energyGr = newEnergyGraph()

	battGraph.area.SetSizeRequest(780, 220)
	energyGr.area.SetSizeRequest(780, 220)

	timeBar := newTimeRangeBar(selectedRange, func(idx int) {
		selectedRange = idx
	})

	graphBox := gtk.NewBox(gtk.OrientationVertical, 8)
	graphBox.Append(battGraph.area)
	graphBox.Append(energyGr.area)

	overviewBox := gtk.NewBox(gtk.OrientationVertical, 8)
	overviewBox.SetMarginStart(12)
	overviewBox.SetMarginEnd(12)
	overviewBox.SetMarginTop(12)
	overviewBox.SetMarginBottom(12)
	overviewBox.Append(stats.container)
	overviewBox.Append(timeBar.container)
	overviewBox.Append(graphBox)

	// Placeholder pages
	batteryPage := adw.NewStatusPage()
	batteryPage.SetTitle("Battery Status")
	batteryPage.SetDescription("Coming Soon")
	batteryPage.SetIconName("battery-full-symbolic")

	calibrationPage := adw.NewStatusPage()
	calibrationPage.SetTitle("Calibration")
	calibrationPage.SetDescription("Coming Soon")
	calibrationPage.SetIconName("preferences-color-symbolic")

	settingsPage := adw.NewStatusPage()
	settingsPage.SetTitle("Settings")
	settingsPage.SetDescription("Coming Soon")
	settingsPage.SetIconName("preferences-system-symbolic")

	// Stack with pages
	stack := gtk.NewStack()
	stack.SetTransitionType(gtk.StackTransitionTypeCrossfade)
	stack.AddTitled(overviewBox, "overview", "Overview")
	stack.AddTitled(batteryPage, "battery", "Battery Status")
	stack.AddTitled(calibrationPage, "calibration", "Calibration")
	stack.AddTitled(settingsPage, "settings", "Settings")

	// Header bar with stack switcher
	switcher := gtk.NewStackSwitcher()
	switcher.SetStack(stack)

	headerBar := adw.NewHeaderBar()
	headerBar.SetTitleWidget(switcher)

	// Main layout
	mainBox := gtk.NewBox(gtk.OrientationVertical, 0)
	mainBox.Append(headerBar)
	mainBox.Append(stack)

	win.SetContent(mainBox)
	win.Show()

	// Initial data load
	refreshData()

	// Auto-refresh every 5 seconds
	glib.TimeoutSecondsAdd(5, func() bool {
		refreshData()
		return true
	})
}

func refreshData() {
	now := time.Now()
	from := now.Add(-timeRanges[selectedRange].Duration)

	current, err := client.GetCurrentStats()
	if err == nil {
		stats.Update(current)
	}

	history, err := client.GetHistory(from, now)
	if err != nil {
		return
	}

	sleep, _ := client.GetSleepEvents(from, now)

	battGraph.SetData(history.Battery, sleep, from, now)
	energyGr.SetData(history.Battery, sleep, from, now)
}
