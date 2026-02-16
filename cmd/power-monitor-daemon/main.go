package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/cptspacemanspiff/gnome-power-display/internal/collector"
	"github.com/cptspacemanspiff/gnome-power-display/internal/config"
	dbussvc "github.com/cptspacemanspiff/gnome-power-display/internal/dbus"
	"github.com/cptspacemanspiff/gnome-power-display/internal/storage"
)

// topicHandler wraps an slog.Handler and filters records by a "topic" attribute.
// Records without a topic attribute always pass through (startup messages, errors).
// Records with a topic only pass if that topic is enabled.
type topicHandler struct {
	inner  slog.Handler
	topics map[string]bool
	topic  string // set when WithAttrs includes a "topic" key
}

func (h *topicHandler) Enabled(_ context.Context, level slog.Level) bool {
	return h.inner.Enabled(context.Background(), level)
}

func (h *topicHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.topics["all"] {
		return h.inner.Handle(ctx, r)
	}
	topic := h.topic
	if topic == "" {
		// Check record-level attrs as fallback.
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "topic" {
				topic = a.Value.String()
				return false
			}
			return true
		})
	}
	if topic != "" && !h.topics[topic] {
		return nil
	}
	return h.inner.Handle(ctx, r)
}

func (h *topicHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	topic := h.topic
	for _, a := range attrs {
		if a.Key == "topic" {
			topic = a.Value.String()
		}
	}
	return &topicHandler{inner: h.inner.WithAttrs(attrs), topics: h.topics, topic: topic}
}

func (h *topicHandler) WithGroup(name string) slog.Handler {
	return &topicHandler{inner: h.inner.WithGroup(name), topics: h.topics, topic: h.topic}
}

func main() {
	verbose := flag.Bool("verbose", false, "enable all verbose logging (equivalent to -log=all)")
	logFlag := flag.String("log", "", "comma-separated log topics: battery,backlight,process,sleep (or 'all')")
	resetDB := flag.Bool("reset-db", false, "delete the database and start fresh")
	configPath := flag.String("config", "/etc/power-monitor/config.toml", "path to config file")
	flag.Parse()

	topics := make(map[string]bool)
	if *verbose {
		topics["all"] = true
	}
	if *logFlag != "" {
		for _, t := range strings.Split(*logFlag, ",") {
			topics[strings.TrimSpace(t)] = true
		}
	}

	handler := &topicHandler{
		inner:  slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}),
		topics: topics,
	}
	logger := slog.New(handler)

	// Load config.
	cfg, err := config.Load(*configPath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg = config.DefaultConfig()
			logger.Info("config file not found, using defaults", "path", *configPath)
		} else {
			logger.Error("load config", "path", *configPath, "err", err)
			os.Exit(1)
		}
	} else {
		logger.Info("loaded config", "path", *configPath)
	}

	batteryLog := logger.With("topic", "battery")
	backlightLog := logger.With("topic", "backlight")
	processLog := logger.With("topic", "process")
	sleepLog := logger.With("topic", "sleep")

	dbPath := cfg.Storage.DBPath
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		logger.Error("create data dir", "err", err)
		os.Exit(1)
	}

	if *resetDB {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if err := os.Remove(dbPath + suffix); err != nil && !os.IsNotExist(err) {
				logger.Error("delete database", "err", err)
				os.Exit(1)
			}
		}
		logger.Info("database deleted", "path", dbPath)
		return
	}

	store, err := storage.Open(dbPath)
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	// Run cleanup on startup.
	runCleanup(store, cfg.Cleanup.RetentionDays, logger)

	svc := dbussvc.NewService(store)
	conn, err := svc.Export()
	if err != nil {
		logger.Error("export dbus service", "err", err)
		os.Exit(1)
	}
	defer conn.Close()
	logger.Info("D-Bus service registered", "name", dbussvc.BusName)

	// Import any power state events from the systemd hook state log.
	importStateLog(store, sleepLog, cfg.Storage.StateLogPath)

	// Start sleep monitor; its wake channel triggers state log re-reads
	// (catches short sleeps that don't produce a wall-clock jump).
	sleepMon, err := collector.NewSleepMonitor(sleepLog)
	var wakeCh <-chan struct{}
	if err != nil {
		logger.Warn("sleep monitor unavailable", "err", err)
	} else {
		wakeCh = sleepMon.Wake()
		defer sleepMon.Close()
	}

	// Start battery collector with averaging window.
	batteryCollector := collector.NewBatteryCollector(int64(cfg.Collection.PowerAverageSeconds))

	// Start process collector.
	procCollector := collector.NewProcessCollector(cfg.Collection.TopProcesses)

	// Collect battery, backlight, and process data on a ticker.
	collectInterval := time.Duration(cfg.Collection.IntervalSeconds) * time.Second
	ticker := time.NewTicker(collectInterval)
	defer ticker.Stop()

	// Start cleanup ticker.
	cleanupTicker := time.NewTicker(time.Duration(cfg.Cleanup.IntervalHours) * time.Hour)
	defer cleanupTicker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	jumpThreshold := time.Duration(cfg.Collection.WallClockJumpThresholdSeconds) * time.Second
	logger.Info("power-monitor-daemon started", "interval", collectInterval)
	lastTick := time.Now().Round(0) // Strip monotonic so Sub uses wall clock across suspend
	for {
		select {
		case <-ticker.C:
			now := time.Now().Round(0)
			if now.Sub(lastTick) > jumpThreshold {
				logger.Info("wall-clock jump detected, re-reading state log", "gap_secs", int(now.Sub(lastTick).Seconds()))
				importStateLog(store, sleepLog, cfg.Storage.StateLogPath)
			}
			lastTick = now
			if sample, err := batteryCollector.Collect(); err == nil {
				batteryLog.Info("sample",
					"capacity_pct", sample.CapacityPct,
					"status", sample.Status,
					"power_uw", sample.PowerUW)
				if err := store.InsertBatterySample(*sample); err != nil {
					logger.Error("store battery", "err", err)
				}
			} else {
				batteryLog.Debug("collect failed", "err", err)
			}
			if sample, err := collector.CollectBacklight(); err == nil {
				backlightLog.Info("sample",
					"brightness", sample.Brightness,
					"max_brightness", sample.MaxBrightness)
				if err := store.InsertBacklightSample(*sample); err != nil {
					logger.Error("store backlight", "err", err)
				}
			} else {
				backlightLog.Debug("collect failed", "err", err)
			}
			if procSamples, freqSamples, stats, err := procCollector.Collect(); err == nil {
				capturedPct := 0.0
				if stats.TotalTicks > 0 {
					capturedPct = float64(stats.CapturedTicks) / float64(stats.TotalTicks) * 100
				}
				processLog.Info("sample",
					"active", stats.TotalProcs,
					"top_n", len(procSamples),
					"captured_ticks", stats.CapturedTicks,
					"total_ticks", stats.TotalTicks,
					"captured_pct", fmt.Sprintf("%.1f", capturedPct))
				var pProcs, eProcs []collector.ProcessSample
				for _, s := range procSamples {
					if procCollector.IsPCore(s.LastCPU) {
						pProcs = append(pProcs, s)
					} else {
						eProcs = append(eProcs, s)
					}
				}
				for _, s := range pProcs {
					processLog.Debug("p-core process", "pid", s.PID, "comm", s.Comm, "ticks", s.CPUTicksDelta, "cpu", s.LastCPU)
				}
				for _, s := range eProcs {
					processLog.Debug("e-core process", "pid", s.PID, "comm", s.Comm, "ticks", s.CPUTicksDelta, "cpu", s.LastCPU)
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
				processLog.Debug("core ticks",
					"p_ticks", pTicks, "p_cores", strings.Join(pParts, " "),
					"e_ticks", eTicks, "e_cores", strings.Join(eParts, " "))
				if err := store.InsertProcessSamples(procSamples); err != nil {
					logger.Error("store process samples", "err", err)
				}
				if err := store.InsertCPUFreqSamples(freqSamples); err != nil {
					logger.Error("store cpu freq samples", "err", err)
				}
			} else {
				processLog.Debug("collect failed", "err", err)
			}
		case <-wakeCh:
			logger.Info("wake signal received, re-reading state log")
			importStateLog(store, sleepLog, cfg.Storage.StateLogPath)
			lastTick = time.Now().Round(0)
		case <-cleanupTicker.C:
			runCleanup(store, cfg.Cleanup.RetentionDays, logger)
		case <-sigCh:
			logger.Info("shutting down")
			return
		}
	}
}

func runCleanup(store *storage.DB, retentionDays int, logger *slog.Logger) {
	before := time.Now().AddDate(0, 0, -retentionDays).Unix()
	deleted, err := store.DeleteOlderThan(before)
	if err != nil {
		logger.Error("cleanup failed", "err", err)
	} else if deleted > 0 {
		logger.Info("cleanup completed", "deleted_rows", deleted, "retention_days", retentionDays)
	}
}

func importStateLog(store *storage.DB, logger *slog.Logger, stateLogPath string) {
	events := collector.ReadAndConsumeStateLog(logger, time.Now(), stateLogPath)
	if len(events) == 0 {
		logger.Debug("no new power state events in state log")
		return
	}
	for _, evt := range events {
		inserted, err := store.InsertPowerStateEvent(evt)
		if err != nil {
			logger.Error("store power state event", "err", err)
		} else if inserted {
			logger.Info("imported power state event",
				"type", evt.Type,
				"start", evt.StartTime,
				"end", evt.EndTime,
				"suspend_secs", evt.SuspendSecs,
				"hibernate_secs", evt.HibernateSecs)
		} else {
			logger.Debug("duplicate power state event skipped", "start", evt.StartTime)
		}
	}
}
