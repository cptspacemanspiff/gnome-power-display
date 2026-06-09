package calibration

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ChargeEdge is a single observed change in charge_now_uAh.
// It records when the change was observed and the magnitude.
type ChargeEdge struct {
	Time      time.Time
	ChargeUAH int64
	DeltaUAH  int64 // signed: negative = discharging, positive = charging
}

// ChargeSample is the result of a charge sensor sampling run.
type ChargeSample struct {
	Edges          []ChargeEdge
	StartChargeUAH int64
	EndChargeUAH   int64
	Duration       time.Duration
}

// SampleChargeEdges rapidly polls charge_now and returns only the samples
// where the value changed, over the given duration.
//
// Poll interval should be well below the expected hardware update interval
// (e.g. 10ms) so that our sampling doesn't add noise to the timing.
func SampleChargeEdges(duration, pollInterval time.Duration) (*ChargeSample, error) {
	batPath, err := findBatteryPath()
	if err != nil {
		return nil, err
	}
	chargePath := filepath.Join(batPath, "charge_now")

	// Read initial value.
	startCharge, err := readInt64File(chargePath)
	if err != nil {
		return nil, fmt.Errorf("read initial charge: %w", err)
	}

	start := time.Now()
	deadline := start.Add(duration)
	lastCharge := startCharge

	var edges []ChargeEdge
	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)

		charge, err := readInt64File(chargePath)
		if err != nil {
			// Transient read error; skip this sample.
			continue
		}
		if charge == lastCharge {
			continue
		}

		edges = append(edges, ChargeEdge{
			Time:      time.Now(),
			ChargeUAH: charge,
			DeltaUAH:  charge - lastCharge,
		})
		lastCharge = charge
	}

	return &ChargeSample{
		Edges:          edges,
		StartChargeUAH: startCharge,
		EndChargeUAH:   lastCharge,
		Duration:       time.Since(start),
	}, nil
}

// ChargeEdgeDebug holds raw charge sensor observations for offline analysis.
// All derived quantities (period, phases, residuals) are computed by the
// analysis scripts rather than here, so the script can experiment freely.
type ChargeEdgeDebug struct {
	// Edge timestamps in milliseconds from the first edge.
	EdgeTimesMs   []float64 `json:"edge_times_ms"`
	// Charge value at each edge (uAh).
	EdgeChargeUAH []int64   `json:"edge_charge_uah"`
	// Signed change in charge at each edge (uAh).
	EdgeDeltaUAH  []int64   `json:"edge_delta_uah"`
	// Charge at the very start of the sampling run (before first edge).
	StartChargeUAH int64 `json:"start_charge_uah"`
	// Charge at the end of the sampling run.
	EndChargeUAH   int64 `json:"end_charge_uah"`
	// Total sampling duration in milliseconds.
	DurationMs     int64 `json:"duration_ms"`
}

// WriteDebug writes a ChargeEdgeDebug to a JSON file at the given path.
func WriteDebug(path string, dbg *ChargeEdgeDebug) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(dbg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// ChargeEdgeStats summarizes a set of ChargeEdge observations.
type ChargeEdgeStats struct {
	// UpdateIntervalMs is the median time between consecutive charge updates,
	// used as the estimate of the hardware update period T.
	UpdateIntervalMs int64
	// QuantizationUAH is the smallest observed absolute delta.
	QuantizationUAH int64
	// TimingStdDevMs is the standard deviation of observed edge timestamps
	// modulo the update period, representing per-sample timing uncertainty.
	TimingStdDevMs float64
	// SampleCount is the number of edges observed.
	SampleCount int
}

// ComputeEdgeStats derives update period, quantization, and timing uncertainty
// from a ChargeSample.
//
// The update period T is derived from the quantization step Q and the average
// drain/charge rate R: T = Q / R. This is more accurate than using inter-edge
// intervals directly, which are noisy due to sampling jitter and skipped updates.
//
// The timing uncertainty is computed by mapping each edge timestamp into the
// phase space [0, T) using the derived T, finding the circular mean of that
// distribution, then computing the stddev of residuals around the mean.
// This stddev represents how precisely we know when a hardware update occurred.
//
// Returns nil, nil if fewer than 3 edges were observed or the drain rate is zero.
func ComputeEdgeStats(s *ChargeSample) (*ChargeEdgeStats, *ChargeEdgeDebug) {
	edges := s.Edges
	if len(edges) < 3 {
		return nil, nil
	}

	// Smallest observed absolute delta = quantization step Q.
	var minDelta int64
	for _, e := range edges {
		d := e.DeltaUAH
		if d < 0 {
			d = -d
		}
		if d > 0 && (minDelta == 0 || d < minDelta) {
			minDelta = d
		}
	}

	// Average drain/charge rate R = total charge change / duration (uAh/ms).
	totalDeltaUAH := s.StartChargeUAH - s.EndChargeUAH // positive when discharging
	if totalDeltaUAH < 0 {
		totalDeltaUAH = -totalDeltaUAH
	}
	durationMs := s.Duration.Milliseconds()
	if totalDeltaUAH == 0 || durationMs == 0 {
		return nil, nil
	}

	// T = Q / R (ms). Use float to avoid integer truncation.
	Q := float64(minDelta)
	R := float64(totalDeltaUAH) / float64(durationMs) // uAh/ms
	Tf := Q / R                                        // ms before rounding
	T := int64(math.Round(Tf))
	if T <= 0 {
		return nil, nil
	}
	Tf = float64(T) // round-trip so phase math uses the same integer T

	// Build edge times relative to first edge (ms), then compute initial
	// phase stats using the rounded Q/R period estimate.
	epoch := edges[0].Time
	edgeTimesMs := make([]float64, len(edges))
	for i, e := range edges {
		edgeTimesMs[i] = float64(e.Time.Sub(epoch).Milliseconds())
	}
	// Refine T via cycle-count unwrapping + OLS fit.
	Trefined, _ := refinePeriod(edgeTimesMs, Tf)
	_, _, _, stdDev := computePhaseStats(edgeTimesMs, Trefined)

	// Build debug struct from raw data only.
	edgeCharges := make([]int64, len(edges))
	edgeDeltas := make([]int64, len(edges))
	for i, e := range edges {
		edgeCharges[i] = e.ChargeUAH
		edgeDeltas[i] = e.DeltaUAH
	}
	dbg := &ChargeEdgeDebug{
		EdgeTimesMs:    edgeTimesMs,
		EdgeChargeUAH:  edgeCharges,
		EdgeDeltaUAH:   edgeDeltas,
		StartChargeUAH: s.StartChargeUAH,
		EndChargeUAH:   s.EndChargeUAH,
		DurationMs:     durationMs,
	}

	return &ChargeEdgeStats{
		UpdateIntervalMs: int64(math.Round(Trefined)),
		QuantizationUAH:  minDelta,
		TimingStdDevMs:   stdDev,
		SampleCount:      len(edges),
	}, dbg
}

// refinePeriod estimates the true hardware update period from edge timestamps
// using cycle-count unwrapping and OLS. An overestimate of T is used for
// counting so that single-missed edges don't corrupt the count.
// Returns (T_refined, t0).
func refinePeriod(timesMs []float64, Tguess float64) (float64, float64) {
	const periodOverestimate = 3.0
	const maxSkip = 8
	TforCounting := Tguess * periodOverestimate

	counts := make([]float64, len(timesMs))
	var n float64
	for i := 1; i < len(timesMs); i++ {
		dt := timesMs[i] - timesMs[i-1]
		m := math.Round(dt / TforCounting)
		if m < 1 {
			m = 1
		} else if m > maxSkip {
			m = maxSkip
		}
		n += m
		counts[i] = n
	}

	// OLS: t_k = t0 + n_k * T
	N := float64(len(timesMs))
	var sumN, sumT, sumNT, sumNN float64
	for i, t := range timesMs {
		sumN += counts[i]
		sumT += t
		sumNT += counts[i] * t
		sumNN += counts[i] * counts[i]
	}
	meanN := sumN / N
	meanT := sumT / N
	denom := sumNN - N*meanN*meanN
	if denom <= 0 {
		return Tguess, timesMs[0]
	}
	T := (sumNT - N*meanN*meanT) / denom
	t0 := meanT - T*meanN
	return T, t0
}

// computePhaseStats maps timestamps into [0, T) via the circular mean, then
// returns phases, mean phase, residuals, and stddev.
func computePhaseStats(timesMs []float64, T float64) (phases []float64, meanPhase float64, residuals []float64, stdDev float64) {
	epoch := timesMs[0]
	phases = make([]float64, len(timesMs))
	for i, t := range timesMs {
		p := math.Mod(t-epoch, T)
		if p < 0 {
			p += T
		}
		phases[i] = p
	}

	var sinSum, cosSum float64
	for _, p := range phases {
		angle := 2 * math.Pi * p / T
		sinSum += math.Sin(angle)
		cosSum += math.Cos(angle)
	}
	a := math.Atan2(sinSum, cosSum)
	if a < 0 {
		a += 2 * math.Pi
	}
	meanPhase = a * T / (2 * math.Pi)

	residuals = make([]float64, len(phases))
	var sumSq float64
	for i, p := range phases {
		r := p - meanPhase
		if r > T/2 {
			r -= T
		} else if r < -T/2 {
			r += T
		}
		residuals[i] = r
		sumSq += r * r
	}
	stdDev = math.Sqrt(sumSq / float64(len(phases)))
	return
}


func findBatteryPath() (string, error) {
	matches, err := filepath.Glob("/sys/class/power_supply/BAT*")
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no battery found")
	}
	return matches[0], nil
}

func readInt64File(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
}
