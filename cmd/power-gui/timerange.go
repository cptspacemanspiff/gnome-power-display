package main

import (
	"time"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

type timeRange struct {
	Label    string
	Duration time.Duration
}

var timeRanges = []timeRange{
	{"15m", 15 * time.Minute},
	{"1h", time.Hour},
	{"3h", 3 * time.Hour},
	{"6h", 6 * time.Hour},
	{"24h", 24 * time.Hour},
	{"7d", 7 * 24 * time.Hour},
}

type timeRangeBar struct {
	container *gtk.Box
	buttons   []*gtk.ToggleButton
}

func newTimeRangeBar(selected int, onSelect func(int)) *timeRangeBar {
	bar := &timeRangeBar{}
	bar.container = gtk.NewBox(gtk.OrientationHorizontal, 4)
	bar.container.SetHAlign(gtk.AlignCenter)
	bar.container.AddCSSClass("time-range-bar")

	bar.buttons = make([]*gtk.ToggleButton, len(timeRanges))
	for i, tr := range timeRanges {
		idx := i
		btn := gtk.NewToggleButtonWithLabel(tr.Label)
		btn.SetActive(i == selected)
		btn.ConnectToggled(func() {
			if !btn.Active() {
				return
			}
			// Deactivate other buttons
			for j, other := range bar.buttons {
				if j != idx {
					other.SetActive(false)
				}
			}
			onSelect(idx)
			refreshData()
		})
		bar.buttons[i] = btn
		bar.container.Append(btn)
	}

	return bar
}
