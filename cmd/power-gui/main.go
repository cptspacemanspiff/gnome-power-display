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
	client        *dbusClient
	stats         *statsBar
	battGraph     *batteryGraph
	energyGr      *energyGraph
	selectedRange int = 3 // default 6h
)

type sidebarEntry struct {
	id       string
	title    string
	iconName string
}

var sidebarEntries = []sidebarEntry{
	{"overview", "Overview", "utilities-system-monitor-symbolic"},
	{"battery", "Battery Status", "battery-full-symbolic"},
	{"calibration", "Calibration", "preferences-color-symbolic"},
	{"settings", "Settings", "preferences-system-symbolic"},
}

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

	// Content stack
	stack := gtk.NewStack()
	stack.SetTransitionType(gtk.StackTransitionTypeCrossfade)
	stack.SetHExpand(true)
	stack.SetVExpand(true)

	// Build overview page
	stats = newStatsBar()
	battGraph = newBatteryGraph()
	energyGr = newEnergyGraph()

	battGraph.area.SetSizeRequest(600, 220)
	energyGr.area.SetSizeRequest(600, 220)

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

	stack.AddNamed(overviewBox, "overview")

	// Battery health page
	batteryPage := newBatteryHealthPage()
	stack.AddNamed(batteryPage.container, "battery")

	calibrationPage := adw.NewStatusPage()
	calibrationPage.SetTitle("Calibration")
	calibrationPage.SetDescription("Coming Soon")
	calibrationPage.SetIconName("preferences-color-symbolic")
	stack.AddNamed(calibrationPage, "calibration")

	settingsPage := adw.NewStatusPage()
	settingsPage.SetTitle("Settings")
	settingsPage.SetDescription("Coming Soon")
	settingsPage.SetIconName("preferences-system-symbolic")
	stack.AddNamed(settingsPage, "settings")

	// Sidebar
	sidebar := gtk.NewListBox()
	sidebar.SetSelectionMode(gtk.SelectionBrowse)
	sidebar.AddCSSClass("navigation-sidebar")

	for _, entry := range sidebarEntries {
		row := newSidebarRow(entry.iconName, entry.title)
		sidebar.Append(row)
	}

	contentTitle := gtk.NewLabel("")
	contentTitle.AddCSSClass("heading")

	sidebar.ConnectRowSelected(func(row *gtk.ListBoxRow) {
		if row == nil {
			return
		}
		idx := row.Index()
		if idx >= 0 && idx < len(sidebarEntries) {
			contentTitle.SetLabel(sidebarEntries[idx].title)
			stack.SetVisibleChildName(sidebarEntries[idx].id)
		}
	})

	// Select first row
	if first := sidebar.RowAtIndex(0); first != nil {
		sidebar.SelectRow(first)
	}

	sidebarScroll := gtk.NewScrolledWindow()
	sidebarScroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	sidebarScroll.SetChild(sidebar)
	sidebarScroll.SetSizeRequest(250, -1)
	sidebarScroll.SetVExpand(true)

	leftTitle := gtk.NewLabel("Power Monitor")
	leftTitle.AddCSSClass("heading")
	leftTitle.SetXAlign(0)

	leftHeader := gtk.NewBox(gtk.OrientationHorizontal, 0)
	leftHeader.AddCSSClass("left-pane-header")
	leftHeader.SetMarginStart(12)
	leftHeader.SetMarginEnd(12)
	leftHeader.SetMarginTop(8)
	leftHeader.SetMarginBottom(8)
	leftHeader.Append(leftTitle)

	leftPane := gtk.NewBox(gtk.OrientationVertical, 0)
	leftPane.SetVExpand(true)
	leftPane.Append(leftHeader)
	leftPane.Append(sidebarScroll)

	rightHeader := adw.NewHeaderBar()
	rightHeader.SetTitleWidget(contentTitle)

	// Horizontal split: sidebar | content
	splitBox := gtk.NewBox(gtk.OrientationHorizontal, 0)
	splitBox.Append(leftPane)

	separator := gtk.NewSeparator(gtk.OrientationVertical)
	separator.AddCSSClass("sidebar-separator")
	splitBox.Append(separator)

	contentScroll := gtk.NewScrolledWindow()
	contentScroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	contentScroll.SetChild(stack)
	contentScroll.SetHExpand(true)
	contentScroll.SetVExpand(true)

	rightPane := gtk.NewBox(gtk.OrientationVertical, 0)
	rightPane.SetHExpand(true)
	rightPane.Append(rightHeader)
	rightPane.Append(contentScroll)

	splitBox.Append(rightPane)

	win.SetContent(splitBox)
	win.Show()

	// Initial data load
	refreshData()

	// Auto-refresh every 5 seconds
	glib.TimeoutSecondsAdd(5, func() bool {
		refreshData()
		return true
	})
}

func newSidebarRow(iconName, label string) *gtk.Box {
	icon := gtk.NewImageFromIconName(iconName)

	text := gtk.NewLabel(label)
	text.SetXAlign(0)
	text.SetHExpand(true)

	row := gtk.NewBox(gtk.OrientationHorizontal, 10)
	row.Append(icon)
	row.Append(text)
	return row
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

	sleep, _ := client.GetPowerStateEvents(from, now)

	battGraph.SetData(history.Battery, sleep, from, now)
	energyGr.SetData(history.Battery, sleep, from, now)
}
