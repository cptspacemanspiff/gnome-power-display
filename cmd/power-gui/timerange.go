package main

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
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

func newTimeRangeBar(selected int, onSelect func(int)) fyne.CanvasObject {
	buttons := make([]fyne.CanvasObject, len(timeRanges))
	for i, tr := range timeRanges {
		idx := i
		btn := widget.NewButton(tr.Label, func() {
			onSelect(idx)
		})
		if i == selected {
			btn.Importance = widget.HighImportance
		}
		buttons[idx] = btn
	}
	row := container.New(layout.NewHBoxLayout(), buttons...)
	bg := canvas.NewRectangle(color.NRGBA{R: 30, G: 30, B: 30, A: 230})
	return container.NewStack(bg, container.NewPadded(row))
}
