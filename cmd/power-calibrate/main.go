package main

import (
	"fmt"
	"log"
	"os"

	"github.com/cptspacemanspiff/gnome-power-display/internal/calibration"
)

func main() {
	if os.Geteuid() != 0 {
		log.Fatal("power-calibrate must be run as root (needed for CPU frequency and backlight control)")
	}

	home := os.Getenv("HOME")
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		home = "/home/" + sudoUser
	}
	outPath := calibration.DefaultPath(home)

	// TODO: implement calibration

	fmt.Printf("Calibration complete. Results written to: %s\n", outPath)
}
