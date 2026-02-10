package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cptspacemanspiff/gnome-power-display/internal/collector"
	dbussvc "github.com/cptspacemanspiff/gnome-power-display/internal/dbus"
	"github.com/cptspacemanspiff/gnome-power-display/internal/storage"
)

func main() {
	verbose := flag.Bool("verbose", false, "enable verbose logging")
	flag.Parse()

	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".local", "share")
	}
	dbPath := filepath.Join(dataDir, "power-monitor", "data.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	store, err := storage.Open(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer store.Close()

	svc := dbussvc.NewService(store)
	conn, err := svc.Export()
	if err != nil {
		log.Fatalf("export dbus service: %v", err)
	}
	defer conn.Close()
	log.Println("D-Bus service registered as org.gnome.PowerMonitor")

	// Start sleep monitor.
	sleepMon, err := collector.NewSleepMonitor()
	if err != nil {
		log.Printf("sleep monitor unavailable: %v", err)
	} else {
		defer sleepMon.Close()
		go func() {
			for evt := range sleepMon.Events() {
				if err := store.InsertSleepEvent(evt); err != nil {
					log.Printf("store sleep event: %v", err)
				}
			}
		}()
	}

	// Start process collector.
	procCollector := collector.NewProcessCollector(10)

	// Collect battery, backlight, and process data on a ticker.
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Println("power-monitor-daemon started, collecting every 5s")
	for {
		select {
		case <-ticker.C:
			if sample, err := collector.CollectBattery(); err == nil {
				if *verbose {
					log.Printf("battery: %d%% %s, %d uW", sample.CapacityPct, sample.Status, sample.PowerUW)
				}
				if err := store.InsertBatterySample(*sample); err != nil {
					log.Printf("store battery: %v", err)
				}
			} else if *verbose {
				log.Printf("collect battery: %v", err)
			}
			if sample, err := collector.CollectBacklight(); err == nil {
				if *verbose {
					log.Printf("backlight: %d/%d", sample.Brightness, sample.MaxBrightness)
				}
				if err := store.InsertBacklightSample(*sample); err != nil {
					log.Printf("store backlight: %v", err)
				}
			} else if *verbose {
				log.Printf("collect backlight: %v", err)
			}
			if procSamples, freqSamples, stats, err := procCollector.Collect(); err == nil {
				if *verbose {
					capturedPct := 0.0
					if stats.TotalTicks > 0 {
						capturedPct = float64(stats.CapturedTicks) / float64(stats.TotalTicks) * 100
					}
					log.Printf("processes: %d active, top %d captured %d/%d ticks (%.1f%%)",
						stats.TotalProcs, len(procSamples), stats.CapturedTicks, stats.TotalTicks, capturedPct)
					for _, s := range procSamples {
						log.Printf("  pid=%d comm=%s ticks=%d cpu=%d", s.PID, s.Comm, s.CPUTicksDelta, s.LastCPU)
					}
					for cpuID, ticks := range stats.PerCoreTicks {
						coreType := "E"
						if procCollector.IsPCore(cpuID) {
							coreType = "P"
						}
						log.Printf("  core %d (%s): %d ticks", cpuID, coreType, ticks)
					}
				}
				if err := store.InsertProcessSamples(procSamples); err != nil {
					log.Printf("store process samples: %v", err)
				}
				if err := store.InsertCPUFreqSamples(freqSamples); err != nil {
					log.Printf("store cpu freq samples: %v", err)
				}
			} else if *verbose {
				log.Printf("collect processes: %v", err)
			}
		case <-sigCh:
			log.Println("shutting down")
			return
		}
	}
}
