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
	fmt.Println("The battery controller has a long internal averaging window, so any system")
	fmt.Println("changes take 1-2 minutes to fully reflect in power readings.")
	fmt.Println()
	fmt.Println("Before pressing Enter, please:")
	fmt.Println("  1. Close ALL unnecessary programs (browser, IDE, etc.)")
	fmt.Println("  2. Turn off WiFi and Bluetooth")
	fmt.Println("  3. Unplug all external devices (USB, monitors, etc.)")
	fmt.Println("  4. Ensure the laptop is running on battery (unplug AC adapter)")
	fmt.Println("  5. Wait ~60 seconds after making these changes for readings to settle")
	fmt.Println()
	fmt.Println("IMPORTANT: Do not touch the laptop or change anything once calibration starts.")
	fmt.Println("           Any change will take 1-2 minutes to flush from the battery averaging.")
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
	fmt.Println("[1/5] Locking CPU frequency and disabling turbo boost...")
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

	// Set brightness to 0% and let the system settle. The CPU frequency change
	// and brightness change both need to flush through the battery controller's
	// averaging window before measurements are meaningful.
	fmt.Println("       Setting brightness to 0%% and waiting for battery averaging to flush...")
	if err := calibration.SetBrightness(0); err != nil {
		log.Fatalf("set brightness: %v", err)
	}
	fmt.Println("       This takes ~90 seconds. Do not touch the laptop.")
	time.Sleep(90 * time.Second)
	fmt.Println("       Initial settling complete.")
	fmt.Println()

	// Create battery collector for calibration measurements.
	// Use a 30-second averaging window for charge-delta power calculation.
	bc := collector.NewBatteryCollector(30)

	// Measure battery update interval.
	fmt.Println("[2/5] Measuring battery firmware update interval...")
	fmt.Println("       Rapidly polling power readings to detect value changes...")
	stats, err := calibration.MeasureUpdateInterval(bc)
	if err != nil {
		fmt.Printf("       Warning: could not measure update interval: %v\n", err)
		fmt.Println("       Using default of 3 seconds")
		stats.Median = 3 * time.Second
		stats.Min = 3 * time.Second
		stats.Max = 3 * time.Second
	} else {
		fmt.Printf("       Update interval: median=%v  min=%v  max=%v  (%d samples)\n",
			stats.Median, stats.Min, stats.Max, len(stats.All))
	}
	fmt.Println()

	// Measure controller latency (how many update cycles before reading reflects change).
	fmt.Println("[3/5] Measuring battery controller latency...")
	fmt.Println("       Setting brightness to 0%%, stabilizing, then stepping to 100%%...")
	latency, staleCycles, err := calibration.MeasureLatency(bc, stats.Median)
	if err != nil {
		fmt.Printf("       Warning: could not measure latency: %v\n", err)
		fmt.Println("       Using default of 2 update cycles")
		staleCycles = 2
		latency = 2 * stats.Median
	} else {
		fmt.Printf("       Latency: %v (%d stale update cycles before reading changed)\n", latency, staleCycles)
	}
	fmt.Println()

	// Measure power at each brightness level.
	levels := []int{0, 25, 50, 75, 100}
	var samples []calibration.BrightnessSample
	var baselinePower int64

	// Use the measured latency as the settling wait, with a minimum of 90 seconds.
	// This is how long the battery controller's averaging window takes to flush.
	settleWait := latency
	if settleWait < 90*time.Second {
		settleWait = 90 * time.Second
	}
	sampleDuration := 30 * time.Second

	fmt.Printf("[4/5] Measuring power at %d brightness levels (settle %v + sample %v each)...\n",
		len(levels), settleWait, sampleDuration)
	for i, pct := range levels {
		fmt.Printf("       Level %d/%d: brightness %d%%", i+1, len(levels), pct)
		if err := calibration.SetBrightness(pct); err != nil {
			log.Fatalf("set brightness %d%%: %v", pct, err)
		}

		// Wait for the battery averaging window to fully flush.
		fmt.Printf(" (settling %v)...", settleWait)
		time.Sleep(settleWait)

		// Measure power usage over the next fixed sampling window.
		fmt.Printf(" sampling %v...", sampleDuration)
		avg, err := calibration.MeasurePowerOverWindow(bc, sampleDuration, 500*time.Millisecond)
		if err != nil {
			log.Fatalf("measure power at %d%%: %v", pct, err)
		}
		fmt.Printf(" avg: %.2f W\n", float64(avg)/1e6)

		samples = append(samples, calibration.BrightnessSample{
			BrightnessPct: pct,
			AvgPowerUW:    avg,
		})
		if pct == 0 {
			baselinePower = avg
		}
	}
	fmt.Println()

	// Write results.
	result := calibration.CalibrationResult{
		UpdateIntervalMs: stats.Median.Milliseconds(),
		LatencyMs:        latency.Milliseconds(),
		StaleCycles:      staleCycles,
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

	fmt.Printf("[5/5] Calibration complete! Results written to:\n")
	fmt.Printf("       %s\n", outPath)
	fmt.Println()
	fmt.Println("Summary:")
	fmt.Printf("  Update interval:  %v (min=%v, max=%v)\n", stats.Median, stats.Min, stats.Max)
	fmt.Printf("  Controller lag:   %v (%d stale cycles)\n", latency, staleCycles)
	fmt.Printf("  Baseline power:   %.2f W (display off)\n", float64(baselinePower)/1e6)
	for _, s := range samples {
		displayPower := float64(s.AvgPowerUW-baselinePower) / 1e6
		fmt.Printf("  Brightness %3d%%:  %.2f W total (%.2f W display)\n",
			s.BrightnessPct, float64(s.AvgPowerUW)/1e6, displayPower)
	}
}
