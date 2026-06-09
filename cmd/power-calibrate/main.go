package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cptspacemanspiff/gnome-power-display/internal/calibration"
)

func main() {
	// if os.Geteuid() != 0 {
	// 	log.Fatal("power-calibrate must be run as root (needed for CPU frequency and backlight control)")
	// }

	home := os.Getenv("HOME")
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		home = "/home/" + sudoUser
	}
	outPath := calibration.DefaultPath(home)

	calibrateChargeSensor()

	// TODO: implement remaining calibration steps

	fmt.Printf("Calibration complete. Results written to: %s\n", outPath)
}

// calibrateChargeSensor samples charge_now edges for 3 minutes and prints
// the derived update period, quantization step, and per-sample timing uncertainty.
func calibrateChargeSensor() {
	const duration = 3 * time.Minute
	const pollInterval = 10 * time.Millisecond

	fmt.Println("=== Charge Sensor Calibration ===")
	fmt.Printf("Sampling charge_now for %v (poll interval %v)...\n", duration, pollInterval)

	sample, err := calibration.SampleChargeEdges(duration, pollInterval)
	if err != nil {
		log.Fatalf("sample charge edges: %v", err)
	}

	fmt.Printf("Observed %d charge updates.\n", len(sample.Edges))

	stats, dbg := calibration.ComputeEdgeStats(sample)
	if stats == nil {
		log.Fatal("not enough edges to compute stats (need at least 3)")
	}

	fmt.Printf("  Update period:      %d ms\n", stats.UpdateIntervalMs)
	fmt.Printf("  Quantization step:  %d uAh\n", stats.QuantizationUAH)
	fmt.Printf("  Timing stddev:      %.1f ms (per-sample uncertainty)\n", stats.TimingStdDevMs)

	debugPath := "/tmp/power-calibrate-sensor-debug.json"
	if err := calibration.WriteDebug(debugPath, dbg); err != nil {
		log.Printf("warning: failed to write debug data: %v", err)
	} else {
		fmt.Printf("  Debug data:         %s\n", debugPath)
	}
}
