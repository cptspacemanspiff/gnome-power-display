package main

import (
	"log"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	xtheme "fyne.io/x/fyne/theme"
)

func main() {
	a := app.NewWithID("org.gnome.PowerMonitorGUI")
	a.Settings().SetTheme(xtheme.AdwaitaTheme())

	client, err := newDBusClient()
	if err != nil {
		log.Fatalf("Failed to connect to D-Bus: %v", err)
	}

	w := a.NewWindow("Power Monitor")
	w.Resize(fyne.NewSize(900, 600))

	stats := newStatsBar()
	battGraph := newBatteryGraph()
	energyGr := newEnergyGraph()

	selectedRange := 3 // default 6h

	// Build overview page content
	var timeBar fyne.CanvasObject
	graphs := container.New(layout.NewGridWrapLayout(fyne.NewSize(780, 220)), battGraph, energyGr)
	overviewContent := container.NewVBox(stats.container, nil, graphs)

	rebuildTimeBar := func() {}
	rebuildTimeBar = func() {
		timeBar = newTimeRangeBar(selectedRange, func(idx int) {
			selectedRange = idx
			rebuildTimeBar()
			refreshData(client, stats, battGraph, energyGr, selectedRange)
		})
		overviewContent.Objects[1] = timeBar
		overviewContent.Refresh()
	}
	rebuildTimeBar()

	// Placeholder pages
	batteryPage := container.NewCenter(widget.NewLabel("Battery Status — Coming Soon"))
	calibrationPage := container.NewCenter(widget.NewLabel("Calibration — Coming Soon"))
	settingsPage := container.NewCenter(widget.NewLabel("Settings — Coming Soon"))

	tabs := container.NewAppTabs(
		container.NewTabItem("Overview", overviewContent),
		container.NewTabItem("Battery Status", batteryPage),
		container.NewTabItem("Calibration", calibrationPage),
		container.NewTabItem("Settings", settingsPage),
	)
	tabs.SetTabLocation(container.TabLocationLeading)

	w.SetContent(tabs)

	// Initial data load
	refreshData(client, stats, battGraph, energyGr, selectedRange)

	// Auto-refresh every 5 seconds
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			fyne.DoAndWait(func() {
				refreshData(client, stats, battGraph, energyGr, selectedRange)
			})
		}
	}()

	w.ShowAndRun()
}

func refreshData(client *dbusClient, stats *statsBar, battGraph *batteryGraph, energyGr *energyGraph, rangeIdx int) {
	now := time.Now()
	from := now.Add(-timeRanges[rangeIdx].Duration)

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
