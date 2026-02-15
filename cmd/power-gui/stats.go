package main

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
)

var (
	colorGreenAccent = color.NRGBA{R: 77, G: 191, B: 102, A: 255}
	colorWhiteLabel  = color.NRGBA{R: 200, G: 200, B: 200, A: 255}
)

type statsBar struct {
	powerLabel   *canvas.Text
	batteryLabel *canvas.Text
	statusLabel  *canvas.Text
	brightLabel  *canvas.Text
	container    fyne.CanvasObject
}

func newStatsBar() *statsBar {
	s := &statsBar{
		powerLabel:   newStatText("-- W"),
		batteryLabel: newStatText("--%"),
		statusLabel:  newStatText("--"),
		brightLabel:  newStatText("--%"),
	}

	powerTitle := newLabelText("Power")
	batteryTitle := newLabelText("Battery")
	statusTitle := newLabelText("Status")
	brightTitle := newLabelText("Brightness")

	bg := canvas.NewRectangle(accentBgColor())

	row := container.New(layout.NewHBoxLayout(),
		container.NewVBox(powerTitle, s.powerLabel),
		layout.NewSpacer(),
		container.NewVBox(batteryTitle, s.batteryLabel),
		layout.NewSpacer(),
		container.NewVBox(statusTitle, s.statusLabel),
		layout.NewSpacer(),
		container.NewVBox(brightTitle, s.brightLabel),
	)

	s.container = container.NewStack(bg, container.NewPadded(row))
	return s
}

func (s *statsBar) Update(stats *currentStats) {
	if stats == nil {
		return
	}
	if stats.Battery != nil {
		watts := float64(stats.Battery.PowerUW) / 1e6
		s.powerLabel.Text = fmt.Sprintf("%.1f W", watts)
		s.batteryLabel.Text = fmt.Sprintf("%d%%", stats.Battery.CapacityPct)
		s.statusLabel.Text = stats.Battery.Status
	}
	if stats.Backlight != nil && stats.Backlight.MaxBrightness > 0 {
		pct := float64(stats.Backlight.Brightness) * 100 / float64(stats.Backlight.MaxBrightness)
		s.brightLabel.Text = fmt.Sprintf("%.0f%%", pct)
	}
	s.powerLabel.Refresh()
	s.batteryLabel.Refresh()
	s.statusLabel.Refresh()
	s.brightLabel.Refresh()
}

func newStatText(text string) *canvas.Text {
	t := canvas.NewText(text, colorGreenAccent)
	t.TextSize = 18
	t.TextStyle = fyne.TextStyle{Bold: true}
	return t
}

func newLabelText(text string) *canvas.Text {
	t := canvas.NewText(text, colorWhiteLabel)
	t.TextSize = 12
	return t
}
