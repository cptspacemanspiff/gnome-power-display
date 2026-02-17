package main

import (
	"fmt"
	"math"
	"time"

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
	"github.com/diamondburned/gotk4/pkg/pangocairo"

	"github.com/cptspacemanspiff/gnome-power-display/internal/collector"
)

// RGBA colors (0.0-1.0)
type rgba struct{ r, g, b, a float64 }

var (
	colGraphBg     = rgba{0.12, 0.12, 0.12, 0.90}
	colGrid        = rgba{1, 1, 1, 0.08}
	colAxis        = rgba{1, 1, 1, 0.25}
	colLabel       = rgba{1, 1, 1, 0.50}
	colTitle       = rgba{1, 1, 1, 0.70}
	colGreenLine   = rgba{0.30, 0.75, 0.40, 1.0}
	colGreenFill   = rgba{0.30, 0.75, 0.40, 0.25}
	colBlueLine    = rgba{0.35, 0.55, 0.90, 1.0}
	colSleepBg     = rgba{0.30, 0.35, 0.55, 0.35}
	colSleepLabel  = rgba{0.65, 0.70, 0.90, 0.60}
	colNoDataBg    = rgba{0.31, 0.31, 0.31, 0.24}
	colChargingBar = rgba{0.30, 0.75, 0.40, 0.71}
)

const (
	padLeft   = 50
	padRight  = 15
	padTop    = 30
	padBottom = 30
	gapThresh = 30 // seconds
)

func (c rgba) set(cr *cairo.Context) {
	cr.SetSourceRGBA(c.r, c.g, c.b, c.a)
}

// batteryGraph renders a battery level line chart using Cairo
type batteryGraph struct {
	area    *gtk.DrawingArea
	battery []collector.BatterySample
	sleep   []collector.PowerStateEvent
	from    time.Time
	to      time.Time
}

func newBatteryGraph() *batteryGraph {
	g := &batteryGraph{}
	g.area = gtk.NewDrawingArea()
	g.area.SetVExpand(true)
	g.area.SetHExpand(true)
	g.area.SetDrawFunc(g.draw)
	return g
}

func (g *batteryGraph) SetData(battery []collector.BatterySample, sleep []collector.PowerStateEvent, from, to time.Time) {
	g.battery = battery
	g.sleep = sleep
	g.from = from
	g.to = to
	g.area.QueueDraw()
}

func (g *batteryGraph) draw(_ *gtk.DrawingArea, cr *cairo.Context, w, h int) {
	// Background
	colGraphBg.set(cr)
	cr.Rectangle(0, 0, float64(w), float64(h))
	cr.Fill()

	if w < padLeft+padRight+10 || h < padTop+padBottom+10 {
		return
	}

	plotW := w - padLeft - padRight
	plotH := h - padTop - padBottom

	// Title
	drawLabel(cr, "Battery Level", padLeft, 8, colTitle, 11)

	// Y-axis grid (0%, 25%, 50%, 75%, 100%)
	for i := 0; i <= 4; i++ {
		pct := i * 25
		y := float64(padTop + plotH - plotH*pct/100)
		colGrid.set(cr)
		cr.MoveTo(float64(padLeft), y)
		cr.LineTo(float64(padLeft+plotW), y)
		cr.Stroke()
		drawLabel(cr, fmt.Sprintf("%d%%", pct), 5, int(y)-5, colLabel, 9)
	}

	// X-axis time labels
	drawTimeAxis(cr, g.from, g.to, padLeft, padTop+plotH, plotW, padTop, plotH)

	fromUnix := g.from.Unix()
	toUnix := g.to.Unix()
	timeSpan := float64(toUnix - fromUnix)
	if timeSpan <= 0 {
		return
	}

	// Sleep regions
	for _, ev := range g.sleep {
		x1 := float64(padLeft) + float64(ev.StartTime-fromUnix)/timeSpan*float64(plotW)
		x2 := float64(padLeft) + float64(ev.EndTime-fromUnix)/timeSpan*float64(plotW)
		x1 = math.Max(x1, float64(padLeft))
		x2 = math.Min(x2, float64(padLeft+plotW))
		colSleepBg.set(cr)
		cr.Rectangle(x1, float64(padTop), x2-x1, float64(plotH))
		cr.Fill()
		label := "Sleep"
		if ev.Type == "hibernate" {
			label = "Hibernate"
		}
		mid := (x1 + x2) / 2
		drawLabel(cr, label, int(mid)-15, padTop+plotH/2, colSleepLabel, 9)
	}

	samples := g.battery
	if len(samples) == 0 {
		return
	}

	// No-data gap hatching
	for i := 1; i < len(samples); i++ {
		dt := samples[i].Timestamp - samples[i-1].Timestamp
		if dt > gapThresh {
			x1 := float64(padLeft) + float64(samples[i-1].Timestamp-fromUnix)/timeSpan*float64(plotW)
			x2 := float64(padLeft) + float64(samples[i].Timestamp-fromUnix)/timeSpan*float64(plotW)
			x1 = math.Max(x1, float64(padLeft))
			x2 = math.Min(x2, float64(padLeft+plotW))
			drawHatched(cr, x1, float64(padTop), x2-x1, float64(plotH))
		}
	}

	// Charging indicator bar below x-axis
	for i := 0; i < len(samples); i++ {
		if samples[i].Status == "Charging" {
			x := float64(padLeft) + float64(samples[i].Timestamp-fromUnix)/timeSpan*float64(plotW)
			barW := 2.0
			if i+1 < len(samples) {
				x2 := float64(padLeft) + float64(samples[i+1].Timestamp-fromUnix)/timeSpan*float64(plotW)
				barW = x2 - x
				if barW < 1 {
					barW = 1
				}
			}
			colChargingBar.set(cr)
			cr.Rectangle(x, float64(padTop+plotH+2), barW, 4)
			cr.Fill()
		}
	}

	// Battery line with fill
	for i := 1; i < len(samples); i++ {
		dt := samples[i].Timestamp - samples[i-1].Timestamp
		if dt > gapThresh {
			continue
		}

		x1 := float64(padLeft) + float64(samples[i-1].Timestamp-fromUnix)/timeSpan*float64(plotW)
		y1 := float64(padTop+plotH) - float64(plotH)*float64(samples[i-1].CapacityPct)/100.0
		x2 := float64(padLeft) + float64(samples[i].Timestamp-fromUnix)/timeSpan*float64(plotW)
		y2 := float64(padTop+plotH) - float64(plotH)*float64(samples[i].CapacityPct)/100.0
		bottom := float64(padTop + plotH)

		// Fill area under line segment
		colGreenFill.set(cr)
		cr.MoveTo(x1, y1)
		cr.LineTo(x2, y2)
		cr.LineTo(x2, bottom)
		cr.LineTo(x1, bottom)
		cr.ClosePath()
		cr.Fill()

		// Line
		colGreenLine.set(cr)
		cr.SetLineWidth(2)
		cr.MoveTo(x1, y1)
		cr.LineTo(x2, y2)
		cr.Stroke()
	}
}

// energyGraph renders a power usage bar chart using Cairo
type energyGraph struct {
	area    *gtk.DrawingArea
	battery []collector.BatterySample
	sleep   []collector.PowerStateEvent
	from    time.Time
	to      time.Time
}

func newEnergyGraph() *energyGraph {
	g := &energyGraph{}
	g.area = gtk.NewDrawingArea()
	g.area.SetVExpand(true)
	g.area.SetHExpand(true)
	g.area.SetDrawFunc(g.draw)
	return g
}

func (g *energyGraph) SetData(battery []collector.BatterySample, sleep []collector.PowerStateEvent, from, to time.Time) {
	g.battery = battery
	g.sleep = sleep
	g.from = from
	g.to = to
	g.area.QueueDraw()
}

func bucketDuration(d time.Duration) time.Duration {
	switch {
	case d <= 15*time.Minute:
		return 15 * time.Second
	case d <= time.Hour:
		return time.Minute
	case d <= 3*time.Hour:
		return 5 * time.Minute
	case d <= 6*time.Hour:
		return 10 * time.Minute
	case d <= 24*time.Hour:
		return 30 * time.Minute
	default:
		return time.Hour
	}
}

type powerBucket struct {
	sumPowerUW int64
	count      int
	charging   bool
	chgCount   int
}

func (g *energyGraph) draw(_ *gtk.DrawingArea, cr *cairo.Context, w, h int) {
	colGraphBg.set(cr)
	cr.Rectangle(0, 0, float64(w), float64(h))
	cr.Fill()

	if w < padLeft+padRight+10 || h < padTop+padBottom+10 {
		return
	}

	plotW := w - padLeft - padRight
	plotH := h - padTop - padBottom

	drawLabel(cr, "Energy Usage", padLeft, 8, colTitle, 11)

	fromUnix := g.from.Unix()
	toUnix := g.to.Unix()
	timeSpan := float64(toUnix - fromUnix)
	if timeSpan <= 0 {
		return
	}

	drawTimeAxis(cr, g.from, g.to, padLeft, padTop+plotH, plotW, padTop, plotH)

	// Sleep regions
	for _, ev := range g.sleep {
		x1 := float64(padLeft) + float64(ev.StartTime-fromUnix)/timeSpan*float64(plotW)
		x2 := float64(padLeft) + float64(ev.EndTime-fromUnix)/timeSpan*float64(plotW)
		x1 = math.Max(x1, float64(padLeft))
		x2 = math.Min(x2, float64(padLeft+plotW))
		colSleepBg.set(cr)
		cr.Rectangle(x1, float64(padTop), x2-x1, float64(plotH))
		cr.Fill()
		label := "Sleep"
		if ev.Type == "hibernate" {
			label = "Hibernate"
		}
		mid := (x1 + x2) / 2
		drawLabel(cr, label, int(mid)-15, padTop+plotH/2, colSleepLabel, 9)
	}

	samples := g.battery
	if len(samples) == 0 {
		return
	}

	// Bucket samples
	bucketDur := bucketDuration(g.to.Sub(g.from))
	bucketSecs := int64(bucketDur.Seconds())
	numBuckets := int((toUnix - fromUnix) / bucketSecs)
	if numBuckets < 1 {
		numBuckets = 1
	}

	buckets := make([]powerBucket, numBuckets)
	for _, s := range samples {
		idx := int((s.Timestamp - fromUnix) / bucketSecs)
		if idx < 0 || idx >= numBuckets {
			continue
		}
		buckets[idx].sumPowerUW += s.PowerUW
		buckets[idx].count++
		if s.Status == "Charging" {
			buckets[idx].chgCount++
		}
	}

	var maxPowerW float64
	for i := range buckets {
		if buckets[i].count > 0 {
			buckets[i].charging = buckets[i].chgCount > buckets[i].count/2
			avg := float64(buckets[i].sumPowerUW) / float64(buckets[i].count) / 1e6
			if avg > maxPowerW {
				maxPowerW = avg
			}
		}
	}
	if maxPowerW <= 0 {
		maxPowerW = 10
	}
	maxPowerW = math.Ceil(maxPowerW/5) * 5
	if maxPowerW < 5 {
		maxPowerW = 5
	}

	// Y-axis grid
	numYLines := 4
	for i := 0; i <= numYLines; i++ {
		val := maxPowerW * float64(i) / float64(numYLines)
		y := float64(padTop+plotH) - float64(plotH)*float64(i)/float64(numYLines)
		colGrid.set(cr)
		cr.MoveTo(float64(padLeft), y)
		cr.LineTo(float64(padLeft+plotW), y)
		cr.Stroke()
		drawLabel(cr, fmt.Sprintf("%.0fW", val), 5, int(y)-5, colLabel, 9)
	}

	// Draw bars
	barW := float64(plotW) / float64(numBuckets)
	gap := 1.0
	if barW <= 2 {
		gap = 0
	}

	for i, b := range buckets {
		if b.count == 0 {
			continue
		}
		avgW := float64(b.sumPowerUW) / float64(b.count) / 1e6
		barH := float64(plotH) * avgW / maxPowerW
		x := float64(padLeft) + float64(i)*float64(plotW)/float64(numBuckets) + gap
		y := float64(padTop+plotH) - barH

		if b.charging {
			colGreenLine.set(cr)
		} else {
			colBlueLine.set(cr)
		}
		cr.Rectangle(x, y, barW-gap*2, barH)
		cr.Fill()
	}
}

// Drawing helpers

func drawLabel(cr *cairo.Context, text string, x, y int, col rgba, fontSize int) {
	col.set(cr)
	layout := pangocairo.CreateLayout(cr)
	fd := pango.NewFontDescription()
	fd.SetFamily("Sans")
	fd.SetSize(fontSize * pango.SCALE)
	layout.SetFontDescription(fd)
	layout.SetText(text)
	cr.MoveTo(float64(x), float64(y))
	pangocairo.ShowLayout(cr, layout)
}

func drawTimeAxis(cr *cairo.Context, from, to time.Time, x, y, w, plotTop, plotH int) {
	dur := to.Sub(from)
	var step time.Duration
	var format string
	switch {
	case dur <= 30*time.Minute:
		step = 5 * time.Minute
		format = "15:04"
	case dur <= 2*time.Hour:
		step = 15 * time.Minute
		format = "15:04"
	case dur <= 8*time.Hour:
		step = time.Hour
		format = "15:04"
	case dur <= 2*24*time.Hour:
		step = 3 * time.Hour
		format = "15:04"
	default:
		step = 24 * time.Hour
		format = "Jan 2"
	}

	t := from.Truncate(step).Add(step)
	for t.Before(to) {
		frac := float64(t.Unix()-from.Unix()) / float64(to.Unix()-from.Unix())
		px := float64(x) + frac*float64(w)

		// Vertical grid line
		colGrid.set(cr)
		cr.SetLineWidth(1)
		cr.MoveTo(px, float64(plotTop))
		cr.LineTo(px, float64(plotTop+plotH))
		cr.Stroke()

		drawLabel(cr, t.Format(format), int(px)-15, y+5, colLabel, 8)
		t = t.Add(step)
	}
}

func drawHatched(cr *cairo.Context, x, y, w, h float64) {
	cr.Save()
	cr.Rectangle(x, y, w, h)
	cr.Clip()

	colNoDataBg.set(cr)
	cr.SetLineWidth(1)
	spacing := 8.0
	for offset := -h; offset < w+h; offset += spacing {
		cr.MoveTo(x+offset, y+h)
		cr.LineTo(x+offset+h, y)
		cr.Stroke()
	}
	cr.Restore()
}
