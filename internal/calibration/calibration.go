package calibration

import (
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cptspacemanspiff/gnome-power-display/internal/collector"
)

// PowerReading is a timestamped power measurement.
type PowerReading struct {
	Timestamp time.Time
	PowerUW   int64
}

// CalibrationResult holds the output of a calibration run.
type CalibrationResult struct {
	UpdateIntervalMs int64              `json:"update_interval_ms"`
	LatencyMs        int64              `json:"latency_ms"`
	StaleCycles      int                `json:"stale_cycles"`
	BaselinePowerUW  int64              `json:"baseline_power_uw"`
	Samples          []BrightnessSample `json:"samples"`
	CPUFrequencyKHz  int64              `json:"cpu_frequency_khz"`
	CalibratedAt     string             `json:"calibrated_at"`
}

// BrightnessSample holds power at a given brightness level.
type BrightnessSample struct {
	BrightnessPct int   `json:"brightness_pct"`
	AvgPowerUW    int64 `json:"avg_power_uw"`
}

// PinCPU disables turbo boost and locks all CPU cores to base frequency.
// Returns a restore function that undoes the changes.
func PinCPU() (restore func(), err error) {
	var restoreFns []func()

	// Disable turbo boost (intel_pstate).
	turboPath := "/sys/devices/system/cpu/intel_pstate/no_turbo"
	if origTurbo, err := readSysFile(turboPath); err == nil {
		if err := os.WriteFile(turboPath, []byte("1"), 0644); err != nil {
			return nil, fmt.Errorf("disable turbo: %w", err)
		}
		restoreFns = append(restoreFns, func() {
			os.WriteFile(turboPath, []byte(origTurbo), 0644)
		})
	}

	// Find all CPU cores.
	cpus, err := filepath.Glob("/sys/devices/system/cpu/cpu[0-9]*/cpufreq")
	if err != nil || len(cpus) == 0 {
		return nil, fmt.Errorf("no cpufreq directories found")
	}

	for _, cpufreqDir := range cpus {
		cpuName := filepath.Base(filepath.Dir(cpufreqDir))

		// Read base frequency.
		baseFreq, err := readSysFile(filepath.Join(cpufreqDir, "base_frequency"))
		if err != nil {
			// Fallback: use cpuinfo_min_freq.
			baseFreq, err = readSysFile(filepath.Join(cpufreqDir, "cpuinfo_min_freq"))
			if err != nil {
				log.Printf("  cpu-pin: %s: no base_frequency or cpuinfo_min_freq, skipping", cpuName)
				continue
			}
		}

		curMin, _ := readSysFile(filepath.Join(cpufreqDir, "scaling_min_freq"))
		curMax, _ := readSysFile(filepath.Join(cpufreqDir, "scaling_max_freq"))
		curGov, _ := readSysFile(filepath.Join(cpufreqDir, "scaling_governor"))
		log.Printf("  cpu-pin: %s: base=%s kHz  current min=%s max=%s gov=%s", cpuName, baseFreq, curMin, curMax, curGov)

		// Save and set governor.
		govPath := filepath.Join(cpufreqDir, "scaling_governor")
		if err := os.WriteFile(govPath, []byte("powersave"), 0644); err == nil {
			origGovCopy := curGov
			govPathCopy := govPath
			restoreFns = append(restoreFns, func() {
				os.WriteFile(govPathCopy, []byte(origGovCopy), 0644)
			})
		}

		// Order matters: if target < current min, lower min first.
		// If target > current max, raise max first.
		minPath := filepath.Join(cpufreqDir, "scaling_min_freq")
		maxPath := filepath.Join(cpufreqDir, "scaling_max_freq")
		origMin := curMin
		origMax := curMax

		// Lower min first (so max can go below old min).
		if err := os.WriteFile(minPath, []byte(baseFreq), 0644); err != nil {
			log.Printf("  cpu-pin: %s: set min=%s failed: %v", cpuName, baseFreq, err)
		}
		// Then set max.
		if err := os.WriteFile(maxPath, []byte(baseFreq), 0644); err != nil {
			log.Printf("  cpu-pin: %s: set max=%s failed: %v", cpuName, baseFreq, err)
		}
		// Re-set min in case it needed max lowered first.
		if err := os.WriteFile(minPath, []byte(baseFreq), 0644); err != nil {
			log.Printf("  cpu-pin: %s: set min=%s (retry) failed: %v", cpuName, baseFreq, err)
		}

		// Verify.
		actualFreq, _ := readSysFile(filepath.Join(cpufreqDir, "scaling_cur_freq"))
		log.Printf("  cpu-pin: %s: locked to %s kHz (actual: %s kHz)", cpuName, baseFreq, actualFreq)

		// Restore closures — restore max first, then min (reverse of lock order).
		origMaxCopy := origMax
		maxPathCopy := maxPath
		origMinCopy := origMin
		minPathCopy := minPath
		restoreFns = append(restoreFns, func() {
			os.WriteFile(maxPathCopy, []byte(origMaxCopy), 0644)
			os.WriteFile(minPathCopy, []byte(origMinCopy), 0644)
		})
	}

	restore = func() {
		// Restore in reverse order.
		for i := len(restoreFns) - 1; i >= 0; i-- {
			restoreFns[i]()
		}
	}
	return restore, nil
}

// GetCPUFrequency returns the current scaling frequency of cpu0 in kHz.
func GetCPUFrequency() (int64, error) {
	s, err := readSysFile("/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq")
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(s, 10, 64)
}

// SetBrightness sets the backlight brightness as a percentage (0-100).
func SetBrightness(pct int) error {
	blDir, err := findBacklightDir()
	if err != nil {
		return err
	}
	maxStr, err := readSysFile(filepath.Join(blDir, "max_brightness"))
	if err != nil {
		return fmt.Errorf("read max_brightness: %w", err)
	}
	max, err := strconv.ParseInt(maxStr, 10, 64)
	if err != nil {
		return err
	}
	target := max * int64(pct) / 100
	return os.WriteFile(filepath.Join(blDir, "brightness"), []byte(strconv.FormatInt(target, 10)), 0644)
}

// GetBrightness returns the current and max brightness values.
func GetBrightness() (current, max int64, err error) {
	blDir, err := findBacklightDir()
	if err != nil {
		return 0, 0, err
	}
	curStr, err := readSysFile(filepath.Join(blDir, "brightness"))
	if err != nil {
		return 0, 0, err
	}
	maxStr, err := readSysFile(filepath.Join(blDir, "max_brightness"))
	if err != nil {
		return 0, 0, err
	}
	current, _ = strconv.ParseInt(curStr, 10, 64)
	max, _ = strconv.ParseInt(maxStr, 10, 64)
	return current, max, nil
}

// SamplePower collects power readings for the given duration at the given interval.
func SamplePower(duration, interval time.Duration) ([]PowerReading, error) {
	var readings []PowerReading
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		sample, err := collector.CollectBattery()
		if err != nil {
			return nil, fmt.Errorf("collect battery: %w", err)
		}
		readings = append(readings, PowerReading{
			Timestamp: time.Now(),
			PowerUW:   sample.PowerUW,
		})
		time.Sleep(interval)
	}
	return readings, nil
}

// AvgPower computes the average power from a slice of readings.
func AvgPower(readings []PowerReading) int64 {
	if len(readings) == 0 {
		return 0
	}
	var total int64
	for _, r := range readings {
		total += r.PowerUW
	}
	return total / int64(len(readings))
}

// UpdateIntervalStats holds the result of measuring battery update intervals.
type UpdateIntervalStats struct {
	Median time.Duration
	Min    time.Duration
	Max    time.Duration
	All    []time.Duration
}

// MeasureUpdateInterval determines how often the battery firmware/kernel updates
// the power reading in sysfs. It rapidly polls the power value and measures the
// time between value changes.
func MeasureUpdateInterval() (UpdateIntervalStats, error) {
	// Poll rapidly for up to 30 seconds, looking for value transitions.
	var transitions []time.Time
	var lastValue int64
	first := true
	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		sample, err := collector.CollectBattery()
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if first {
			lastValue = sample.PowerUW
			first = false
		} else if sample.PowerUW != lastValue {
			transitions = append(transitions, time.Now())
			lastValue = sample.PowerUW
			// We need at least a few transitions to get a reliable interval.
			if len(transitions) >= 6 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	if len(transitions) < 3 {
		return UpdateIntervalStats{}, fmt.Errorf("only detected %d value changes in 30s, need at least 3", len(transitions))
	}

	// Compute intervals between consecutive transitions.
	var intervals []time.Duration
	for i := 1; i < len(transitions); i++ {
		intervals = append(intervals, transitions[i].Sub(transitions[i-1]))
	}

	// Simple sort for median/min/max.
	for i := range intervals {
		for j := i + 1; j < len(intervals); j++ {
			if intervals[j] < intervals[i] {
				intervals[i], intervals[j] = intervals[j], intervals[i]
			}
		}
	}

	return UpdateIntervalStats{
		Median: intervals[len(intervals)/2],
		Min:    intervals[0],
		Max:    intervals[len(intervals)-1],
		All:    intervals,
	}, nil
}

// MeasureLatency measures the number of battery update cycles between a brightness
// step change and when the power reading actually reflects it. This captures any
// internal averaging the battery controller may do. The updateInterval should come
// from MeasureUpdateInterval. Returns the latency as a duration and the number of
// stale update cycles observed.
func MeasureLatency(updateInterval time.Duration) (latency time.Duration, staleCycles int, err error) {
	// Set brightness to 0% and wait for readings to stabilize.
	if err := SetBrightness(0); err != nil {
		return 0, 0, fmt.Errorf("set brightness 0%%: %w", err)
	}
	log.Println("  latency: waiting for baseline to stabilize...")

	// Poll until the rolling stddev drops, indicating the averaging window
	// has flushed and readings reflect the current state.
	baselineReadings, err := WaitForStable(updateInterval)
	if err != nil {
		return 0, 0, fmt.Errorf("baseline stabilize: %w", err)
	}
	baselineAvg := AvgPower(baselineReadings)
	if baselineAvg == 0 {
		return 0, 0, fmt.Errorf("baseline power is zero")
	}
	baselineStdDev := stdDev(baselineReadings, baselineAvg)
	// Threshold: 3 standard deviations above baseline. This is much more
	// sensitive than a fixed percentage, since system noise is typically small
	// relative to total power draw.
	threshold := baselineAvg + 3*baselineStdDev
	log.Printf("  latency: baseline avg=%d uW (%.2f W), stddev=%d uW (%.2f W), threshold=%d uW (%.2f W)",
		baselineAvg, float64(baselineAvg)/1e6,
		baselineStdDev, float64(baselineStdDev)/1e6,
		threshold, float64(threshold)/1e6)

	// Sync to an update boundary: poll until we see a value change, so we know
	// we're right at the start of a fresh cycle.
	var lastValue int64
	sample, _ := collector.CollectBattery()
	if sample != nil {
		lastValue = sample.PowerUW
	}
	syncDeadline := time.Now().Add(2 * updateInterval)
	for time.Now().Before(syncDeadline) {
		s, err := collector.CollectBattery()
		if err == nil && s.PowerUW != lastValue {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Now make the step change right after the update boundary.
	changeTime := time.Now()
	if err := SetBrightness(100); err != nil {
		return 0, 0, fmt.Errorf("set brightness 100%%: %w", err)
	}
	log.Printf("  latency: brightness set to 100%% at %v", changeTime.Format("15:04:05.000"))

	// Poll each update cycle. The reading will ramp up as the averaging window
	// mixes in the new power level. Latency = when the stddev of recent readings
	// drops back to baseline stddev level, meaning the averaging window has fully
	// flushed and the reading has settled at the new level.
	maxCycles := 120 // up to ~2 minutes
	pollOffset := updateInterval / 5
	const windowSize = 5
	var readings []PowerReading

	log.Printf("  latency: waiting for readings to settle (baseline stddev=%.2f W, window=%d)",
		float64(baselineStdDev)/1e6, windowSize)

	for cycle := 1; cycle <= maxCycles; cycle++ {
		nextBoundary := changeTime.Add(time.Duration(cycle)*updateInterval + pollOffset)
		sleepFor := time.Until(nextBoundary)
		if sleepFor > 0 {
			time.Sleep(sleepFor)
		}

		s, err := collector.CollectBattery()
		if err != nil {
			log.Printf("  latency: cycle %2d  error: %v", cycle, err)
			continue
		}
		elapsed := time.Since(changeTime)
		readings = append(readings, PowerReading{Timestamp: time.Now(), PowerUW: s.PowerUW})
		delta := float64(s.PowerUW-baselineAvg) / 1e6

		if len(readings) >= windowSize {
			window := readings[len(readings)-windowSize:]
			windowAvg := AvgPower(window)
			windowSD := stdDev(window, windowAvg)
			// Settled = stddev of recent window is within 2x of baseline stddev.
			settled := windowSD <= 2*baselineStdDev

			log.Printf("  latency: cycle %2d  t=+%v  power=%.2f W  delta=%+.2f W  window_avg=%.2f W  window_sd=%.4f W  settled=%v",
				cycle, elapsed.Round(time.Millisecond), float64(s.PowerUW)/1e6, delta,
				float64(windowAvg)/1e6, float64(windowSD)/1e6, settled)

			if settled {
				log.Printf("  latency: fully settled at cycle %d (t=+%v), stddev %.4f W <= 2x baseline %.4f W",
					cycle, elapsed.Round(time.Millisecond), float64(windowSD)/1e6, float64(baselineStdDev)/1e6)
				return elapsed, cycle, nil
			}
		} else {
			log.Printf("  latency: cycle %2d  t=+%v  power=%.2f W  delta=%+.2f W  (collecting window %d/%d)",
				cycle, elapsed.Round(time.Millisecond), float64(s.PowerUW)/1e6, delta, len(readings), windowSize)
		}
	}

	return 0, maxCycles, fmt.Errorf("power reading did not settle within %d cycles", maxCycles)
}

// WaitForStable samples power at the update interval and waits until the
// rate of change has settled to a steady state. Battery voltage naturally
// drifts as the battery discharges, so readings never truly stabilize — but
// after a brightness step change, the controller's averaging window causes
// an extra transient on top of the background drift. We detect when that
// transient is over by splitting the window into quarters and comparing the
// slope (rate of change) of the first half vs second half. When the slopes
// match, the transient has passed and we're left with just background drift.
func WaitForStable(updateInterval time.Duration) ([]PowerReading, error) {
	const windowSize = 20
	const maxWait = 120 * time.Second

	var all []PowerReading
	deadline := time.Now().Add(maxWait)

	for time.Now().Before(deadline) {
		sample, err := collector.CollectBattery()
		if err != nil {
			time.Sleep(updateInterval)
			continue
		}
		all = append(all, PowerReading{Timestamp: time.Now(), PowerUW: sample.PowerUW})

		if len(all) >= windowSize {
			window := all[len(all)-windowSize:]
			avg := AvgPower(window)
			sd := stdDev(window, avg)

			// Compute slope of each half (uW per sample).
			// Slope = (mean of second quarter - mean of first quarter) per half-window.
			q := windowSize / 4
			q1Avg := AvgPower(window[:q])         // oldest quarter
			q2Avg := AvgPower(window[q : 2*q])    // second quarter
			q3Avg := AvgPower(window[2*q : 3*q])  // third quarter
			q4Avg := AvgPower(window[3*q:])        // newest quarter

			olderSlope := float64(q2Avg - q1Avg)   // change across first half
			newerSlope := float64(q4Avg - q3Avg)   // change across second half
			slopeDiff := math.Abs(olderSlope - newerSlope)

			// Normalize slope difference by stddev. If the slopes differ
			// by less than 1 stddev, the transient is over.
			slopeDiffSigmas := float64(0)
			if sd > 0 {
				slopeDiffSigmas = slopeDiff / float64(sd)
			}

			log.Printf("  stabilize: n=%d  avg=%.2f W  stddev=%.2f W  q1=%.2f q2=%.2f q3=%.2f q4=%.2f  older_slope=%+.0f  newer_slope=%+.0f  slope_diff=%.1fσ",
				len(all), float64(avg)/1e6, float64(sd)/1e6,
				float64(q1Avg)/1e6, float64(q2Avg)/1e6, float64(q3Avg)/1e6, float64(q4Avg)/1e6,
				olderSlope/1e3, newerSlope/1e3, slopeDiffSigmas)

			// Stable when slopes match (transient over) and we have low noise.
			if slopeDiffSigmas < 1.0 && float64(sd)/float64(avg) < 0.02 {
				log.Printf("  stabilize: settled after %d samples (slopes match, transient over)", len(all))
				return window, nil
			}
		}
		time.Sleep(updateInterval)
	}
	return nil, fmt.Errorf("readings did not stabilize within %v", maxWait)
}

func stdDev(readings []PowerReading, mean int64) int64 {
	if len(readings) < 2 {
		return 0
	}
	var sumSq float64
	for _, r := range readings {
		d := float64(r.PowerUW - mean)
		sumSq += d * d
	}
	variance := sumSq / float64(len(readings)-1)
	return int64(math.Sqrt(variance))
}

func findBacklightDir() (string, error) {
	matches, err := filepath.Glob("/sys/class/backlight/*")
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no backlight found")
	}
	return matches[0], nil
}

func readSysFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
