package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/cptspacemanspiff/gnome-power-display/internal/calibration"
	"github.com/cptspacemanspiff/gnome-power-display/internal/collector"
)

func main() {
	if os.Geteuid() != 0 {
		log.Fatal("power-calibrate must be run as root (needed for CPU frequency and backlight control)")
	}

	fmt.Println("=== Power Monitor Display Calibration ===")
	fmt.Println()
	fmt.Println("This tool measures your display's power consumption at various brightness levels.")
	fmt.Println()
	fmt.Println("Before pressing Enter, please:")
	fmt.Println("  1. Close ALL unnecessary programs (browser, IDE, etc.)")
	fmt.Println("  2. Turn off WiFi and Bluetooth")
	fmt.Println("  3. Unplug all external devices (USB, monitors, etc.)")
	fmt.Println("  4. Ensure the laptop is running on battery (unplug AC adapter)")
	fmt.Println("  5. Wait a few seconds after making these changes")
	fmt.Println()
	fmt.Println("IMPORTANT: Do not touch the laptop or change anything once calibration starts.")
	fmt.Println()
	fmt.Print("Press Enter when ready...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
	fmt.Println()

	// Save original brightness to restore later.
	origCur, origMax, err := calibration.GetBrightness()
	if err != nil {
		log.Fatalf("get brightness: %v", err)
	}
	origPct := int(origCur * 100 / origMax)
	defer func() {
		fmt.Printf("Restoring brightness to %d%%\n", origPct)
		calibration.SetBrightness(origPct)
	}()

	// Pin CPU frequency.
	fmt.Println("[1/3] Locking CPU frequency and disabling turbo boost...")
	restoreCPU, err := calibration.PinCPU()
	if err != nil {
		log.Fatalf("pin CPU: %v", err)
	}
	defer func() {
		fmt.Println("Restoring CPU settings...")
		restoreCPU()
	}()

	cpuFreq, _ := calibration.GetCPUFrequency()
	fmt.Printf("       CPU locked to %d kHz\n", cpuFreq)

	// Set brightness to 0% as the starting point for measurements.
	fmt.Println("       Setting brightness to 0%...")
	if err := calibration.SetBrightness(0); err != nil {
		log.Fatalf("set brightness: %v", err)
	}
	fmt.Println("       Ready.")
	fmt.Println()

	// Create battery collector for calibration measurements.
	// Use a 30-second averaging window for charge-delta power calculation.
	bc := collector.NewBatteryCollector(30)

	// Measure power at each brightness level.
	levels := []int{0, 25, 50, 75, 100}
	var samples []calibration.BrightnessSample
	var baselinePower int64

	// Use a short settling wait after each brightness change.
	settleWait := 5 * time.Second
	// Longer window reduces quantization error from charge-step endpoints.
	sampleDuration := 30 * 2 * 5 * time.Second
	samplePoll := 500 * time.Millisecond

	fmt.Printf("[2/3] Measuring power at %d brightness levels (settle %v + sample %v each)...\n",
		len(levels), settleWait, sampleDuration)
	for i, pct := range levels {
		brightnessWarned := false
		lastReassertSec := -1

		fmt.Printf("       Level %d/%d: brightness %d%%", i+1, len(levels), pct)
		if err := calibration.SetBrightness(pct); err != nil {
			log.Fatalf("set brightness %d%%: %v", pct, err)
		}

		// Keep reasserting brightness to counter desktop idle dimming.
		fmt.Printf(" (settling %v)...", settleWait)
		fmt.Println()
		waitWithProgress("         [settle]", settleWait, 1*time.Second, func() {
			reassertBrightness(pct, &brightnessWarned)
		})

		// Measure power usage over the next fixed sampling window.
		fmt.Printf(" sampling %v\n", sampleDuration)
		avg, avgErr, deltaChargeUAH, chargeQuantUAH, err := calibration.MeasurePowerOverWindowWithDiagnostics(
			bc,
			sampleDuration,
			samplePoll,
			func(phase string, elapsed, remaining time.Duration, chargeNowUAH, voltageUV int64) {
				sec := int(elapsed.Seconds())
				if sec != lastReassertSec {
					reassertBrightness(pct, &brightnessWarned)
					lastReassertSec = sec
				}

				switch phase {
				case "wait-charge-step":
					fmt.Printf("         [diag] waiting charge-step t=%2ds charge=%d uAh voltage=%.3f V\n",
						int(elapsed.Seconds()), chargeNowUAH, float64(voltageUV)/1e6)
				case "window":
					fmt.Printf("         [diag] sample t=%2ds remaining=%2ds charge=%d uAh voltage=%.3f V\n",
						int(elapsed.Seconds()), int(remaining.Seconds()), chargeNowUAH, float64(voltageUV)/1e6)
				case "wait-end-charge-step":
					fmt.Printf("         [diag] waiting end charge-step t=%2ds charge=%d uAh voltage=%.3f V\n",
						int(elapsed.Seconds()), chargeNowUAH, float64(voltageUV)/1e6)
				case "end":
					fmt.Printf("         [diag] end t=%2ds charge=%d uAh voltage=%.3f V\n",
						int(elapsed.Seconds()), chargeNowUAH, float64(voltageUV)/1e6)
				}
			},
		)
		if err != nil {
			log.Fatalf("measure power at %d%%: %v", pct, err)
		}
		fmt.Printf("       -> avg: %.2f W +/- %.3f W (delta charge: %d uAh, q=%d uAh)\n",
			float64(avg)/1e6, float64(avgErr)/1e6, deltaChargeUAH, chargeQuantUAH)

		samples = append(samples, calibration.BrightnessSample{
			BrightnessPct:         pct,
			AvgPowerUW:            avg,
			AvgPowerErrorUW:       avgErr,
			DeltaChargeUAH:        deltaChargeUAH,
			ChargeQuantizationUAH: chargeQuantUAH,
		})
		if pct == 0 {
			baselinePower = avg
		}
	}
	fmt.Println()

	// Write results.
	result := calibration.CalibrationResult{
		UpdateIntervalMs: 0,
		LatencyMs:        0,
		StaleCycles:      0,
		BaselinePowerUW:  baselinePower,
		Samples:          samples,
		CPUFrequencyKHz:  cpuFreq,
		CalibratedAt:     time.Now().UTC().Format(time.RFC3339),
	}

	// Resolve the real user's home directory when running under sudo,
	// so the config file is written to the invoking user's home, not root's.
	home := os.Getenv("HOME")
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		home = filepath.Join("/home", sudoUser)
	}
	configDir := filepath.Join(home, ".config")
	outDir := filepath.Join(configDir, "power-monitor")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		log.Fatalf("create config dir: %v", err)
	}
	outPath := filepath.Join(outDir, "calibration.json")

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		log.Fatalf("marshal result: %v", err)
	}
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		log.Fatalf("write config: %v", err)
	}

	// Fix ownership if running under sudo so the real user can read the file.
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		if u, err := user.Lookup(sudoUser); err == nil {
			uid, _ := strconv.Atoi(u.Uid)
			gid, _ := strconv.Atoi(u.Gid)
			os.Chown(outDir, uid, gid)
			os.Chown(outPath, uid, gid)
		}
	}

	fmt.Printf("[3/3] Calibration complete! Results written to:\n")
	fmt.Printf("       %s\n", outPath)
	fmt.Println()
	fmt.Println("Summary:")
	fmt.Printf("  Baseline power:   %.2f W (display off)\n", float64(baselinePower)/1e6)
	for _, s := range samples {
		displayPower := float64(s.AvgPowerUW-baselinePower) / 1e6
		fmt.Printf("  Brightness %3d%%:  %.2f +/- %.3f W total (%.2f W display)\n",
			s.BrightnessPct, float64(s.AvgPowerUW)/1e6, float64(s.AvgPowerErrorUW)/1e6, displayPower)
	}
}

func waitWithProgress(prefix string, total, tick time.Duration, onTick func()) {
	if total <= 0 {
		return
	}
	if tick <= 0 {
		tick = time.Second
	}

	deadline := time.Now().Add(total)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			fmt.Printf("%s done\n", prefix)
			return
		}

		fmt.Printf("%s remaining: %2ds\n", prefix, int(remaining.Round(time.Second).Seconds()))
		if onTick != nil {
			onTick()
		}
		sleepFor := tick
		if sleepFor > remaining {
			sleepFor = remaining
		}
		time.Sleep(sleepFor)
	}
}

func reassertBrightness(pct int, warned *bool) {
	if err := calibration.SetBrightness(pct); err != nil && !*warned {
		log.Printf("warning: failed to reassert brightness %d%%: %v", pct, err)
		*warned = true
	}
}
