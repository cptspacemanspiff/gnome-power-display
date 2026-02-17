package main

import (
	"fmt"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

type statsBar struct {
	powerVal   *gtk.Label
	batteryVal *gtk.Label
	statusVal  *gtk.Label
	brightVal  *gtk.Label
	container  *gtk.Box
}

func newStatsBar() *statsBar {
	s := &statsBar{}

	s.powerVal = gtk.NewLabel("-- W")
	s.powerVal.AddCSSClass("stat-value")

	s.batteryVal = gtk.NewLabel("--%")
	s.batteryVal.AddCSSClass("stat-value")

	s.statusVal = gtk.NewLabel("--")
	s.statusVal.AddCSSClass("stat-value")

	s.brightVal = gtk.NewLabel("--%")
	s.brightVal.AddCSSClass("stat-value")

	mkGroup := func(title string, val *gtk.Label) *gtk.Box {
		titleLabel := gtk.NewLabel(title)
		titleLabel.AddCSSClass("stat-title")
		box := gtk.NewBox(gtk.OrientationVertical, 2)
		box.Append(titleLabel)
		box.Append(val)
		return box
	}

	s.container = gtk.NewBox(gtk.OrientationHorizontal, 0)
	s.container.AddCSSClass("stats-bar")
	s.container.SetHomogeneous(true)
	s.container.Append(mkGroup("Power", s.powerVal))
	s.container.Append(mkGroup("Battery", s.batteryVal))
	s.container.Append(mkGroup("Status", s.statusVal))
	s.container.Append(mkGroup("Brightness", s.brightVal))

	return s
}

func (s *statsBar) Update(stats *currentStats) {
	if stats == nil {
		return
	}
	if stats.Battery != nil {
		watts := float64(stats.Battery.PowerUW) / 1e6
		s.powerVal.SetLabel(fmt.Sprintf("%.1f W", watts))
		s.batteryVal.SetLabel(fmt.Sprintf("%d%%", stats.Battery.CapacityPct))
		s.statusVal.SetLabel(stats.Battery.Status)
	}
	if stats.Backlight != nil && stats.Backlight.MaxBrightness > 0 {
		pct := float64(stats.Backlight.Brightness) * 100 / float64(stats.Backlight.MaxBrightness)
		s.brightVal.SetLabel(fmt.Sprintf("%.0f%%", pct))
	}
}
