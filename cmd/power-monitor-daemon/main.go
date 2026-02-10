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

	// Collect battery and backlight on a ticker.
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
		case <-sigCh:
			log.Println("shutting down")
			return
		}
	}
}
