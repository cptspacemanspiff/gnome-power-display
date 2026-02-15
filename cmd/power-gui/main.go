package main

import (
	"log"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
)

func main() {
	a := app.NewWithID("org.gnome.PowerMonitorGUI")
	a.Settings().SetTheme(theme.DarkTheme())

	client, err := newDBusClient()
	if err != nil {
		log.Fatalf("Failed to connect to D-Bus: %v", err)
	}

	w := a.NewWindow("Power Monitor")
	w.Resize(fyne.NewSize(800, 600))

	stats := newStatsBar()
	battGraph := newBatteryGraph()
	energyGr := newEnergyGraph()

	selectedRange := 3 // default 6h

	var timeBar fyne.CanvasObject
	var rebuildTimeBar func()
	rebuildTimeBar = func() {
		timeBar = newTimeRangeBar(selectedRange, func(idx int) {
			selectedRange = idx
			rebuildTimeBar()
			refreshData(client, stats, battGraph, energyGr, selectedRange)
			w.SetContent(buildLayout(stats, timeBar, battGraph, energyGr))
		})
		w.SetContent(buildLayout(stats, timeBar, battGraph, energyGr))
	}
	rebuildTimeBar()

	// Initial data load
	refreshData(client, stats, battGraph, energyGr, selectedRange)

	// Auto-refresh every 5 seconds
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			refreshData(client, stats, battGraph, energyGr, selectedRange)
		}
	}()

	w.ShowAndRun()
}

func buildLayout(stats *statsBar, timeBar fyne.CanvasObject, battGraph *batteryGraph, energyGr *energyGraph) fyne.CanvasObject {
	graphs := container.New(layout.NewGridWrapLayout(fyne.NewSize(780, 220)), battGraph, energyGr)
	return container.NewVBox(
		stats.container,
		timeBar,
		graphs,
	)
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
