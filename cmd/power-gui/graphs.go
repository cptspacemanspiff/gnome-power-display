package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	"github.com/cptspacemanspiff/gnome-power-display/internal/collector"
)

// Colors matching the GNOME extension palette
var (
	colorGraphBg     = color.NRGBA{R: 30, G: 30, B: 30, A: 230}
	colorGrid        = color.NRGBA{R: 255, G: 255, B: 255, A: 20}
	colorAxis        = color.NRGBA{R: 255, G: 255, B: 255, A: 64}
	colorLabel       = color.NRGBA{R: 255, G: 255, B: 255, A: 128}
	colorTitle       = color.NRGBA{R: 255, G: 255, B: 255, A: 179}
	colorGreenLine   = color.NRGBA{R: 77, G: 191, B: 102, A: 255}
	colorGreenFill   = color.NRGBA{R: 77, G: 191, B: 102, A: 64}
	colorBlueLine    = color.NRGBA{R: 89, G: 140, B: 230, A: 255}
	colorSleepBg     = color.NRGBA{R: 77, G: 89, B: 140, A: 89}
	colorSleepLabel  = color.NRGBA{R: 166, G: 179, B: 230, A: 153}
	colorNoDataBg    = color.NRGBA{R: 80, G: 80, B: 80, A: 60}
	colorChargingBar = color.NRGBA{R: 77, G: 191, B: 102, A: 180}
)

const (
	graphPadLeft   = 50
	graphPadRight  = 15
	graphPadTop    = 30
	graphPadBottom = 30
	gapThreshold   = 30 // seconds - gaps larger than this are "no data"
)

// batteryGraph renders a battery level line chart
type batteryGraph struct {
	widget.BaseWidget
	battery []collector.BatterySample
	sleep   []collector.SleepEvent
	from    time.Time
	to      time.Time
}

func newBatteryGraph() *batteryGraph {
	g := &batteryGraph{}
	g.ExtendBaseWidget(g)
	return g
}

func (g *batteryGraph) SetData(battery []collector.BatterySample, sleep []collector.SleepEvent, from, to time.Time) {
	g.battery = battery
	g.sleep = sleep
	g.from = from
	g.to = to
	g.Refresh()
}

func (g *batteryGraph) CreateRenderer() fyne.WidgetRenderer {
	return &batteryRenderer{graph: g}
}

func (g *batteryGraph) MinSize() fyne.Size {
	return fyne.NewSize(400, 200)
}

type batteryRenderer struct {
	graph *batteryGraph
	img   *canvas.Raster
}

func (r *batteryRenderer) Layout(size fyne.Size) {
	if r.img != nil {
		r.img.Resize(size)
	}
}

func (r *batteryRenderer) MinSize() fyne.Size {
	return r.graph.MinSize()
}

func (r *batteryRenderer) Refresh() {
	r.img = canvas.NewRaster(r.draw)
	r.img.ScaleMode = canvas.ImageScalePixels
	r.img.Resize(r.graph.Size())
}

func (r *batteryRenderer) Objects() []fyne.CanvasObject {
	if r.img == nil {
		r.img = canvas.NewRaster(r.draw)
		r.img.ScaleMode = canvas.ImageScalePixels
	}
	return []fyne.CanvasObject{r.img}
}

func (r *batteryRenderer) Destroy() {}

func (r *batteryRenderer) draw(w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	fillRect(img, 0, 0, w, h, colorGraphBg)

	if w < graphPadLeft+graphPadRight+10 || h < graphPadTop+graphPadBottom+10 {
		return img
	}

	plotW := w - graphPadLeft - graphPadRight
	plotH := h - graphPadTop - graphPadBottom

	// Title
	drawText(img, "Battery Level", graphPadLeft, 5, colorTitle)

	// Y-axis grid (0%, 25%, 50%, 75%, 100%)
	for i := 0; i <= 4; i++ {
		pct := i * 25
		y := graphPadTop + plotH - (plotH * pct / 100)
		drawHLine(img, graphPadLeft, y, plotW, colorGrid)
		drawText(img, fmt.Sprintf("%d%%", pct), 5, y-5, colorLabel)
	}

	// X-axis time labels
	drawTimeAxis(img, r.graph.from, r.graph.to, graphPadLeft, graphPadTop+plotH, plotW, colorLabel, colorGrid, graphPadTop, plotH)

	fromUnix := r.graph.from.Unix()
	toUnix := r.graph.to.Unix()
	timeSpan := float64(toUnix - fromUnix)
	if timeSpan <= 0 {
		return img
	}

	// Sleep regions
	for _, ev := range r.graph.sleep {
		x1 := graphPadLeft + int(float64(ev.SleepTime-fromUnix)/timeSpan*float64(plotW))
		x2 := graphPadLeft + int(float64(ev.WakeTime-fromUnix)/timeSpan*float64(plotW))
		x1 = clamp(x1, graphPadLeft, graphPadLeft+plotW)
		x2 = clamp(x2, graphPadLeft, graphPadLeft+plotW)
		fillRect(img, x1, graphPadTop, x2-x1, plotH, colorSleepBg)
		label := "Sleep"
		if ev.Type == "hibernate" {
			label = "Hibernate"
		}
		mid := (x1 + x2) / 2
		drawText(img, label, mid-15, graphPadTop+plotH/2, colorSleepLabel)
	}

	// No-data gaps and battery line
	samples := r.graph.battery
	if len(samples) == 0 {
		return img
	}

	// Detect no-data gaps and draw hatched regions
	for i := 1; i < len(samples); i++ {
		dt := samples[i].Timestamp - samples[i-1].Timestamp
		if dt > gapThreshold {
			x1 := graphPadLeft + int(float64(samples[i-1].Timestamp-fromUnix)/timeSpan*float64(plotW))
			x2 := graphPadLeft + int(float64(samples[i].Timestamp-fromUnix)/timeSpan*float64(plotW))
			x1 = clamp(x1, graphPadLeft, graphPadLeft+plotW)
			x2 = clamp(x2, graphPadLeft, graphPadLeft+plotW)
			fillHatched(img, x1, graphPadTop, x2-x1, plotH, colorNoDataBg)
		}
	}

	// Draw charging indicator bar below x-axis
	for i := 0; i < len(samples); i++ {
		if samples[i].Status == "Charging" {
			x := graphPadLeft + int(float64(samples[i].Timestamp-fromUnix)/timeSpan*float64(plotW))
			barW := 2
			if i+1 < len(samples) {
				x2 := graphPadLeft + int(float64(samples[i+1].Timestamp-fromUnix)/timeSpan*float64(plotW))
				barW = x2 - x
				if barW < 1 {
					barW = 1
				}
			}
			fillRect(img, x, graphPadTop+plotH+2, barW, 4, colorChargingBar)
		}
	}

	// Battery line with fill
	for i := 1; i < len(samples); i++ {
		dt := samples[i].Timestamp - samples[i-1].Timestamp
		if dt > gapThreshold {
			continue // break line at gaps
		}

		x1 := graphPadLeft + int(float64(samples[i-1].Timestamp-fromUnix)/timeSpan*float64(plotW))
		y1 := graphPadTop + plotH - (plotH * samples[i-1].CapacityPct / 100)
		x2 := graphPadLeft + int(float64(samples[i].Timestamp-fromUnix)/timeSpan*float64(plotW))
		y2 := graphPadTop + plotH - (plotH * samples[i].CapacityPct / 100)

		// Fill area under line
		for x := x1; x <= x2; x++ {
			if x < graphPadLeft || x >= graphPadLeft+plotW {
				continue
			}
			t := 0.0
			if x2 != x1 {
				t = float64(x-x1) / float64(x2-x1)
			}
			yy := y1 + int(t*float64(y2-y1))
			bottom := graphPadTop + plotH
			for y := yy; y < bottom; y++ {
				img.SetNRGBA(x, y, colorGreenFill)
			}
		}

		// Draw line
		drawLine(img, x1, y1, x2, y2, colorGreenLine)
	}

	return img
}

// energyGraph renders a power usage bar chart
type energyGraph struct {
	widget.BaseWidget
	battery []collector.BatterySample
	sleep   []collector.SleepEvent
	from    time.Time
	to      time.Time
}

func newEnergyGraph() *energyGraph {
	g := &energyGraph{}
	g.ExtendBaseWidget(g)
	return g
}

func (g *energyGraph) SetData(battery []collector.BatterySample, sleep []collector.SleepEvent, from, to time.Time) {
	g.battery = battery
	g.sleep = sleep
	g.from = from
	g.to = to
	g.Refresh()
}

func (g *energyGraph) CreateRenderer() fyne.WidgetRenderer {
	return &energyRenderer{graph: g}
}

func (g *energyGraph) MinSize() fyne.Size {
	return fyne.NewSize(400, 200)
}

type energyRenderer struct {
	graph *energyGraph
	img   *canvas.Raster
}

func (r *energyRenderer) Layout(size fyne.Size) {
	if r.img != nil {
		r.img.Resize(size)
	}
}

func (r *energyRenderer) MinSize() fyne.Size {
	return r.graph.MinSize()
}

func (r *energyRenderer) Refresh() {
	r.img = canvas.NewRaster(r.draw)
	r.img.ScaleMode = canvas.ImageScalePixels
	r.img.Resize(r.graph.Size())
}

func (r *energyRenderer) Objects() []fyne.CanvasObject {
	if r.img == nil {
		r.img = canvas.NewRaster(r.draw)
		r.img.ScaleMode = canvas.ImageScalePixels
	}
	return []fyne.CanvasObject{r.img}
}

func (r *energyRenderer) Destroy() {}

// bucketDuration returns an appropriate bucket size for the given time range
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
	charging   bool // majority charging
	chgCount   int
}

func (r *energyRenderer) draw(w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	fillRect(img, 0, 0, w, h, colorGraphBg)

	if w < graphPadLeft+graphPadRight+10 || h < graphPadTop+graphPadBottom+10 {
		return img
	}

	plotW := w - graphPadLeft - graphPadRight
	plotH := h - graphPadTop - graphPadBottom

	drawText(img, "Energy Usage", graphPadLeft, 5, colorTitle)

	fromUnix := r.graph.from.Unix()
	toUnix := r.graph.to.Unix()
	timeSpan := float64(toUnix - fromUnix)
	if timeSpan <= 0 {
		return img
	}

	// X-axis time labels
	drawTimeAxis(img, r.graph.from, r.graph.to, graphPadLeft, graphPadTop+plotH, plotW, colorLabel, colorGrid, graphPadTop, plotH)

	// Sleep regions
	for _, ev := range r.graph.sleep {
		x1 := graphPadLeft + int(float64(ev.SleepTime-fromUnix)/timeSpan*float64(plotW))
		x2 := graphPadLeft + int(float64(ev.WakeTime-fromUnix)/timeSpan*float64(plotW))
		x1 = clamp(x1, graphPadLeft, graphPadLeft+plotW)
		x2 = clamp(x2, graphPadLeft, graphPadLeft+plotW)
		fillRect(img, x1, graphPadTop, x2-x1, plotH, colorSleepBg)
		label := "Sleep"
		if ev.Type == "hibernate" {
			label = "Hibernate"
		}
		mid := (x1 + x2) / 2
		drawText(img, label, mid-15, graphPadTop+plotH/2, colorSleepLabel)
	}

	samples := r.graph.battery
	if len(samples) == 0 {
		return img
	}

	// Bucket samples
	bucketDur := bucketDuration(r.graph.to.Sub(r.graph.from))
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

	// Find max power for Y-axis scaling
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
	// Round up to nice number
	maxPowerW = math.Ceil(maxPowerW/5) * 5
	if maxPowerW < 5 {
		maxPowerW = 5
	}

	// Y-axis grid
	numYLines := 4
	for i := 0; i <= numYLines; i++ {
		val := maxPowerW * float64(i) / float64(numYLines)
		y := graphPadTop + plotH - int(float64(plotH)*float64(i)/float64(numYLines))
		drawHLine(img, graphPadLeft, y, plotW, colorGrid)
		drawText(img, fmt.Sprintf("%.0fW", val), 5, y-5, colorLabel)
	}

	// Draw bars
	barW := plotW / numBuckets
	if barW < 1 {
		barW = 1
	}
	gap := 1
	if barW <= 2 {
		gap = 0
	}

	for i, b := range buckets {
		if b.count == 0 {
			continue
		}
		avgW := float64(b.sumPowerUW) / float64(b.count) / 1e6
		barH := int(float64(plotH) * avgW / maxPowerW)
		x := graphPadLeft + i*plotW/numBuckets + gap
		y := graphPadTop + plotH - barH

		c := colorBlueLine
		if b.charging {
			c = colorGreenLine
		}
		fillRect(img, x, y, barW-gap*2, barH, c)
	}

	return img
}

// Drawing helpers

func fillRect(img *image.NRGBA, x, y, w, h int, c color.NRGBA) {
	bounds := img.Bounds()
	for dy := 0; dy < h; dy++ {
		for dx := 0; dx < w; dx++ {
			px, py := x+dx, y+dy
			if px >= bounds.Min.X && px < bounds.Max.X && py >= bounds.Min.Y && py < bounds.Max.Y {
				img.SetNRGBA(px, py, c)
			}
		}
	}
}

func fillHatched(img *image.NRGBA, x, y, w, h int, c color.NRGBA) {
	bounds := img.Bounds()
	for dy := 0; dy < h; dy++ {
		for dx := 0; dx < w; dx++ {
			if (dx+dy)%8 < 2 {
				px, py := x+dx, y+dy
				if px >= bounds.Min.X && px < bounds.Max.X && py >= bounds.Min.Y && py < bounds.Max.Y {
					img.SetNRGBA(px, py, c)
				}
			}
		}
	}
}

func drawHLine(img *image.NRGBA, x, y, w int, c color.NRGBA) {
	for dx := 0; dx < w; dx++ {
		px := x + dx
		if px >= img.Bounds().Min.X && px < img.Bounds().Max.X && y >= img.Bounds().Min.Y && y < img.Bounds().Max.Y {
			img.SetNRGBA(px, y, c)
		}
	}
}

func drawLine(img *image.NRGBA, x1, y1, x2, y2 int, c color.NRGBA) {
	dx := abs(x2 - x1)
	dy := abs(y2 - y1)
	sx, sy := 1, 1
	if x1 > x2 {
		sx = -1
	}
	if y1 > y2 {
		sy = -1
	}
	err := dx - dy
	for {
		if x1 >= img.Bounds().Min.X && x1 < img.Bounds().Max.X && y1 >= img.Bounds().Min.Y && y1 < img.Bounds().Max.Y {
			img.SetNRGBA(x1, y1, c)
			// Thicken line
			if y1+1 < img.Bounds().Max.Y {
				img.SetNRGBA(x1, y1+1, c)
			}
		}
		if x1 == x2 && y1 == y2 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x1 += sx
		}
		if e2 < dx {
			err += dx
			y1 += sy
		}
	}
}

func drawText(img *image.NRGBA, text string, x, y int, c color.NRGBA) {
	// Simple 5x7 pixel font for basic ASCII
	// For production, use a real font renderer; this is a minimal bitmap approach
	cx := x
	for _, ch := range text {
		glyph := getGlyph(ch)
		for row := 0; row < 7; row++ {
			for col := 0; col < 5; col++ {
				if glyph[row]&(1<<(4-col)) != 0 {
					px, py := cx+col, y+row
					if px >= 0 && px < img.Bounds().Max.X && py >= 0 && py < img.Bounds().Max.Y {
						img.SetNRGBA(px, py, c)
					}
				}
			}
		}
		cx += 6
	}
}

func drawTimeAxis(img *image.NRGBA, from, to time.Time, x, y, w int, labelColor, gridColor color.NRGBA, plotTop, plotH int) {
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

	// Align to step boundary
	t := from.Truncate(step).Add(step)
	for t.Before(to) {
		frac := float64(t.Unix()-from.Unix()) / float64(to.Unix()-from.Unix())
		px := x + int(frac*float64(w))
		// Vertical grid line
		for dy := 0; dy < plotH; dy++ {
			py := plotTop + dy
			if px >= 0 && px < img.Bounds().Max.X && py >= 0 && py < img.Bounds().Max.Y {
				img.SetNRGBA(px, py, gridColor)
			}
		}
		drawText(img, t.Format(format), px-15, y+5, labelColor)
		t = t.Add(step)
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Minimal 5x7 bitmap font for digits, letters, and common symbols
func getGlyph(ch rune) [7]byte {
	switch ch {
	case '0':
		return [7]byte{0x0E, 0x11, 0x13, 0x15, 0x19, 0x11, 0x0E}
	case '1':
		return [7]byte{0x04, 0x0C, 0x04, 0x04, 0x04, 0x04, 0x0E}
	case '2':
		return [7]byte{0x0E, 0x11, 0x01, 0x06, 0x08, 0x10, 0x1F}
	case '3':
		return [7]byte{0x0E, 0x11, 0x01, 0x06, 0x01, 0x11, 0x0E}
	case '4':
		return [7]byte{0x02, 0x06, 0x0A, 0x12, 0x1F, 0x02, 0x02}
	case '5':
		return [7]byte{0x1F, 0x10, 0x1E, 0x01, 0x01, 0x11, 0x0E}
	case '6':
		return [7]byte{0x06, 0x08, 0x10, 0x1E, 0x11, 0x11, 0x0E}
	case '7':
		return [7]byte{0x1F, 0x01, 0x02, 0x04, 0x08, 0x08, 0x08}
	case '8':
		return [7]byte{0x0E, 0x11, 0x11, 0x0E, 0x11, 0x11, 0x0E}
	case '9':
		return [7]byte{0x0E, 0x11, 0x11, 0x0F, 0x01, 0x02, 0x0C}
	case '%':
		return [7]byte{0x18, 0x19, 0x02, 0x04, 0x08, 0x13, 0x03}
	case '.':
		return [7]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x0C, 0x0C}
	case ':':
		return [7]byte{0x00, 0x0C, 0x0C, 0x00, 0x0C, 0x0C, 0x00}
	case ' ':
		return [7]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	case '-':
		return [7]byte{0x00, 0x00, 0x00, 0x0E, 0x00, 0x00, 0x00}
	case 'W':
		return [7]byte{0x11, 0x11, 0x11, 0x15, 0x15, 0x1B, 0x11}
	case 'a':
		return [7]byte{0x00, 0x00, 0x0E, 0x01, 0x0F, 0x11, 0x0F}
	case 'b':
		return [7]byte{0x10, 0x10, 0x1E, 0x11, 0x11, 0x11, 0x1E}
	case 'c':
		return [7]byte{0x00, 0x00, 0x0E, 0x11, 0x10, 0x11, 0x0E}
	case 'd':
		return [7]byte{0x01, 0x01, 0x0F, 0x11, 0x11, 0x11, 0x0F}
	case 'e':
		return [7]byte{0x00, 0x00, 0x0E, 0x11, 0x1F, 0x10, 0x0E}
	case 'f':
		return [7]byte{0x06, 0x09, 0x08, 0x1C, 0x08, 0x08, 0x08}
	case 'g':
		return [7]byte{0x00, 0x00, 0x0F, 0x11, 0x0F, 0x01, 0x0E}
	case 'h':
		return [7]byte{0x10, 0x10, 0x1E, 0x11, 0x11, 0x11, 0x11}
	case 'i':
		return [7]byte{0x04, 0x00, 0x0C, 0x04, 0x04, 0x04, 0x0E}
	case 'l':
		return [7]byte{0x0C, 0x04, 0x04, 0x04, 0x04, 0x04, 0x0E}
	case 'n':
		return [7]byte{0x00, 0x00, 0x16, 0x19, 0x11, 0x11, 0x11}
	case 'o':
		return [7]byte{0x00, 0x00, 0x0E, 0x11, 0x11, 0x11, 0x0E}
	case 'p':
		return [7]byte{0x00, 0x00, 0x1E, 0x11, 0x1E, 0x10, 0x10}
	case 'r':
		return [7]byte{0x00, 0x00, 0x16, 0x19, 0x10, 0x10, 0x10}
	case 's':
		return [7]byte{0x00, 0x00, 0x0F, 0x10, 0x0E, 0x01, 0x1E}
	case 't':
		return [7]byte{0x08, 0x08, 0x1C, 0x08, 0x08, 0x09, 0x06}
	case 'u':
		return [7]byte{0x00, 0x00, 0x11, 0x11, 0x11, 0x13, 0x0D}
	case 'y':
		return [7]byte{0x00, 0x00, 0x11, 0x11, 0x0F, 0x01, 0x0E}
	case 'A':
		return [7]byte{0x0E, 0x11, 0x11, 0x1F, 0x11, 0x11, 0x11}
	case 'B':
		return [7]byte{0x1E, 0x11, 0x11, 0x1E, 0x11, 0x11, 0x1E}
	case 'C':
		return [7]byte{0x0E, 0x11, 0x10, 0x10, 0x10, 0x11, 0x0E}
	case 'D':
		return [7]byte{0x1C, 0x12, 0x11, 0x11, 0x11, 0x12, 0x1C}
	case 'E':
		return [7]byte{0x1F, 0x10, 0x10, 0x1E, 0x10, 0x10, 0x1F}
	case 'F':
		return [7]byte{0x1F, 0x10, 0x10, 0x1E, 0x10, 0x10, 0x10}
	case 'H':
		return [7]byte{0x11, 0x11, 0x11, 0x1F, 0x11, 0x11, 0x11}
	case 'J':
		return [7]byte{0x07, 0x02, 0x02, 0x02, 0x12, 0x12, 0x0C}
	case 'L':
		return [7]byte{0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x1F}
	case 'M':
		return [7]byte{0x11, 0x1B, 0x15, 0x11, 0x11, 0x11, 0x11}
	case 'N':
		return [7]byte{0x11, 0x11, 0x19, 0x15, 0x13, 0x11, 0x11}
	case 'O':
		return [7]byte{0x0E, 0x11, 0x11, 0x11, 0x11, 0x11, 0x0E}
	case 'P':
		return [7]byte{0x1E, 0x11, 0x11, 0x1E, 0x10, 0x10, 0x10}
	case 'R':
		return [7]byte{0x1E, 0x11, 0x11, 0x1E, 0x14, 0x12, 0x11}
	case 'S':
		return [7]byte{0x0E, 0x11, 0x10, 0x0E, 0x01, 0x11, 0x0E}
	case 'T':
		return [7]byte{0x1F, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04}
	case 'U':
		return [7]byte{0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x0E}
	case 'V':
		return [7]byte{0x11, 0x11, 0x11, 0x11, 0x0A, 0x0A, 0x04}
	case 'v':
		return [7]byte{0x00, 0x00, 0x11, 0x11, 0x11, 0x0A, 0x04}
	case 'w':
		return [7]byte{0x00, 0x00, 0x11, 0x11, 0x15, 0x15, 0x0A}
	case 'm':
		return [7]byte{0x00, 0x00, 0x1A, 0x15, 0x15, 0x11, 0x11}
	case 'j':
		return [7]byte{0x02, 0x00, 0x06, 0x02, 0x02, 0x12, 0x0C}
	case 'k':
		return [7]byte{0x10, 0x10, 0x12, 0x14, 0x18, 0x14, 0x12}
	default:
		return [7]byte{0x0E, 0x0E, 0x0E, 0x0E, 0x0E, 0x0E, 0x0E} // block for unknown
	}
}
