package calibration

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cptspacemanspiff/gnome-power-display/internal/collector"
)

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
	BrightnessPct         int   `json:"brightness_pct"`
	AvgPowerUW            int64 `json:"avg_power_uw"`
	AvgPowerErrorUW       int64 `json:"avg_power_error_uw"`
	DeltaChargeUAH        int64 `json:"delta_charge_uah"`
	ChargeQuantizationUAH int64 `json:"charge_quantization_uah"`
}

// BatterySampler provides battery samples for calibration measurements.
type BatterySampler interface {
	Collect() (*collector.BatterySample, error)
}

const defaultChargeQuantizationUAH int64 = 1000

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

// MeasurePowerOverWindow measures average power over the next fixed window.
// It waits for the next quantized charge-step change, then measures energy
// using charge delta across the window and average sampled voltage.
func MeasurePowerOverWindow(bs BatterySampler, window, poll time.Duration) (int64, error) {
	powerUW, _, _, _, err := MeasurePowerOverWindowWithDiagnostics(bs, window, poll, nil)
	return powerUW, err
}

// MeasurePowerOverWindowWithDiagnostics measures average power over the next
// fixed window and emits optional per-sample diagnostics.
func MeasurePowerOverWindowWithDiagnostics(
	bs BatterySampler,
	window, poll time.Duration,
	onSample func(phase string, elapsed, remaining time.Duration, chargeNowUAH, voltageUV int64),
) (powerUW, powerErrorUW, deltaChargeUAH, chargeQuantizationUAH int64, err error) {
	if window <= 0 {
		return 0, 0, 0, 0, fmt.Errorf("window must be > 0")
	}
	if poll <= 0 {
		return 0, 0, 0, 0, fmt.Errorf("poll interval must be > 0")
	}

	initialSample, err := bs.Collect()
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("collect initial sample: %w", err)
	}
	if initialSample.ChargeNowUAH <= 0 {
		return 0, 0, 0, 0, fmt.Errorf("initial charge sample unavailable")
	}

	waitStart := time.Now()
	maxWait := 10 * window
	if maxWait < 500*time.Millisecond {
		maxWait = 500 * time.Millisecond
	}

	chargeBeforeStep := initialSample.ChargeNowUAH
	startSample := initialSample
	observedQuantizationUAH := int64(0)
	for {
		if time.Since(waitStart) > maxWait {
			return 0, 0, 0, 0, fmt.Errorf("timed out waiting for charge-step change")
		}

		time.Sleep(poll)
		sample, err := bs.Collect()
		if err != nil {
			return 0, 0, 0, 0, fmt.Errorf("collect charge-step sample: %w", err)
		}

		if onSample != nil {
			onSample("wait-charge-step", time.Since(waitStart), 0, sample.ChargeNowUAH, sample.VoltageUV)
		}

		if sample.ChargeNowUAH <= 0 {
			continue
		}
		if step := absInt64(sample.ChargeNowUAH - chargeBeforeStep); step > 0 {
			observedQuantizationUAH = minNonZeroInt64(observedQuantizationUAH, step)
		}
		if sample.ChargeNowUAH != chargeBeforeStep {
			startSample = sample
			break
		}
	}

	startTime := time.Now()
	startChargeUAH := startSample.ChargeNowUAH

	voltageSum := int64(0)
	voltageCount := int64(0)
	if startSample.VoltageUV > 0 {
		voltageSum += startSample.VoltageUV
		voltageCount++
	}
	lastChargeUAH := startSample.ChargeNowUAH
	deadline := startTime.Add(window)

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		sleepFor := poll
		if sleepFor > remaining {
			sleepFor = remaining
		}
		time.Sleep(sleepFor)

		sample, err := bs.Collect()
		if err != nil {
			return 0, 0, 0, 0, fmt.Errorf("collect window sample: %w", err)
		}
		now := time.Now()
		if sample.ChargeNowUAH > 0 && lastChargeUAH > 0 {
			if step := absInt64(sample.ChargeNowUAH - lastChargeUAH); step > 0 {
				observedQuantizationUAH = minNonZeroInt64(observedQuantizationUAH, step)
			}
			lastChargeUAH = sample.ChargeNowUAH
		}

		if onSample != nil {
			remaining = deadline.Sub(now)
			if remaining < 0 {
				remaining = 0
			}
			onSample("window", now.Sub(startTime), remaining, sample.ChargeNowUAH, sample.VoltageUV)
		}

		if sample.VoltageUV > 0 {
			voltageSum += sample.VoltageUV
			voltageCount++
		}
	}

	endWaitStart := time.Now()
	endWaitMax := 10 * window
	if endWaitMax < 500*time.Millisecond {
		endWaitMax = 500 * time.Millisecond
	}

	endSample := startSample
	endTime := time.Time{}
	for {
		if time.Since(endWaitStart) > endWaitMax {
			return 0, 0, 0, 0, fmt.Errorf("timed out waiting for end charge-step change")
		}

		time.Sleep(poll)
		sample, err := bs.Collect()
		if err != nil {
			return 0, 0, 0, 0, fmt.Errorf("collect end charge-step sample: %w", err)
		}
		now := time.Now()

		if onSample != nil {
			onSample("wait-end-charge-step", now.Sub(startTime), 0, sample.ChargeNowUAH, sample.VoltageUV)
		}

		if sample.ChargeNowUAH <= 0 {
			continue
		}
		if lastChargeUAH > 0 {
			if step := absInt64(sample.ChargeNowUAH - lastChargeUAH); step > 0 {
				observedQuantizationUAH = minNonZeroInt64(observedQuantizationUAH, step)
			}
		}
		if sample.ChargeNowUAH != lastChargeUAH {
			endSample = sample
			endTime = now
			break
		}
	}

	if onSample != nil {
		onSample("end", endTime.Sub(startTime), 0, endSample.ChargeNowUAH, endSample.VoltageUV)
	}

	elapsed := endTime.Sub(startTime)
	if elapsed <= 0 {
		return 0, 0, 0, 0, fmt.Errorf("measurement window elapsed time is zero")
	}
	if startChargeUAH <= 0 || endSample.ChargeNowUAH <= 0 {
		return 0, 0, 0, 0, fmt.Errorf("charge samples unavailable for measurement")
	}

	deltaChargeUAH = absInt64(startChargeUAH - endSample.ChargeNowUAH)
	if deltaChargeUAH == 0 {
		return 0, 0, 0, 0, fmt.Errorf("charge did not change over measurement window")
	}

	if voltageCount <= 0 {
		return 0, 0, 0, 0, fmt.Errorf("no valid voltage samples for measurement")
	}
	avgVoltageUV := voltageSum / voltageCount
	if avgVoltageUV <= 0 {
		return 0, 0, 0, 0, fmt.Errorf("average voltage is not positive")
	}

	// power_uW = delta_charge_uAh * avg_voltage_uV * 3_600_000 / elapsed_ns
	powerUW = (deltaChargeUAH * avgVoltageUV * 3600000) / elapsed.Nanoseconds()
	if powerUW <= 0 {
		return 0, 0, 0, 0, fmt.Errorf("computed power is not positive")
	}

	chargeQuantizationUAH = observedQuantizationUAH
	if chargeQuantizationUAH <= 0 {
		chargeQuantizationUAH = defaultChargeQuantizationUAH
	}

	// Propagate dominant uncertainty from quantized charge readings.
	// With endpoint quantization +/-q/2 each, delta-charge uncertainty is +/-q.
	powerErrorUW = (chargeQuantizationUAH * avgVoltageUV * 3600000) / elapsed.Nanoseconds()
	if powerErrorUW < 0 {
		powerErrorUW = -powerErrorUW
	}

	return powerUW, powerErrorUW, deltaChargeUAH, chargeQuantizationUAH, nil
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func minNonZeroInt64(a, b int64) int64 {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
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
