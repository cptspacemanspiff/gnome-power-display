package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/cptspacemanspiff/gnome-power-display/internal/collector"
	dbussvc "github.com/cptspacemanspiff/gnome-power-display/internal/dbus"
	"github.com/cptspacemanspiff/gnome-power-display/internal/storage"
)

func main() {
	verbose := flag.Bool("verbose", false, "enable verbose logging")
	resetDB := flag.Bool("reset-db", false, "delete the database and start fresh")
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

	if *resetDB {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if err := os.Remove(dbPath + suffix); err != nil && !os.IsNotExist(err) {
				log.Fatalf("delete database: %v", err)
			}
		}
		log.Println("database deleted:", dbPath)
		return
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
					var pProcs, eProcs []collector.ProcessSample
					for _, s := range procSamples {
						if procCollector.IsPCore(s.LastCPU) {
							pProcs = append(pProcs, s)
						} else {
							eProcs = append(eProcs, s)
						}
					}
					if len(pProcs) > 0 {
						log.Printf("  P-core processes:")
						for _, s := range pProcs {
							log.Printf("    pid=%d comm=%s ticks=%d cpu=%d", s.PID, s.Comm, s.CPUTicksDelta, s.LastCPU)
						}
					}
					if len(eProcs) > 0 {
						log.Printf("  E-core processes:")
						for _, s := range eProcs {
							log.Printf("    pid=%d comm=%s ticks=%d cpu=%d", s.PID, s.Comm, s.CPUTicksDelta, s.LastCPU)
						}
					}
					var pTicks, eTicks int64
					var pCores, eCores []int
					for cpuID, isPCore := range procCollector.CPUIDs() {
						if isPCore {
							pCores = append(pCores, cpuID)
						} else {
							eCores = append(eCores, cpuID)
						}
					}
					sort.Ints(pCores)
					sort.Ints(eCores)
					var pParts []string
					for _, id := range pCores {
						t := stats.PerCoreTicks[id]
						pTicks += t
						pParts = append(pParts, fmt.Sprintf("[%d]=%d", id, t))
					}
					var eParts []string
					for _, id := range eCores {
						t := stats.PerCoreTicks[id]
						eTicks += t
						eParts = append(eParts, fmt.Sprintf("[%d]=%d", id, t))
					}
					log.Printf("  P-cores (%d ticks): %s", pTicks, strings.Join(pParts, "  "))
					log.Printf("  E-cores (%d ticks): %s", eTicks, strings.Join(eParts, "  "))
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
