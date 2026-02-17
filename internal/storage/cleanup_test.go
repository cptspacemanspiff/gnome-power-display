package storage

import (
	"fmt"
	"testing"

	"github.com/cptspacemanspiff/gnome-power-display/internal/collector"
)

func countRows(t *testing.T, db *DB, table string) int {
	t.Helper()

	var n int
	row := db.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table))
	if err := row.Scan(&n); err != nil {
		t.Fatalf("count rows in %s: %v", table, err)
	}
	return n
}

func TestDeleteOlderThan(t *testing.T) {
	db := openTestDB(t)

	const (
		oldTs    int64 = 50
		cutoffTs int64 = 100
		newTs    int64 = 150
	)

	// battery_samples
	for _, ts := range []int64{oldTs, cutoffTs, newTs} {
		err := db.InsertBatterySample(collector.BatterySample{Timestamp: ts, VoltageUV: 11000000, CurrentUA: 1000000, PowerUW: 1100000, CapacityPct: 80, Status: "Discharging"})
		if err != nil {
			t.Fatalf("InsertBatterySample(ts=%d): %v", ts, err)
		}
	}

	// backlight_samples
	for _, ts := range []int64{oldTs, cutoffTs, newTs} {
		err := db.InsertBacklightSample(collector.BacklightSample{Timestamp: ts, Brightness: 100, MaxBrightness: 500})
		if err != nil {
			t.Fatalf("InsertBacklightSample(ts=%d): %v", ts, err)
		}
	}

	// power_state_events
	for _, ts := range []int64{oldTs, cutoffTs, newTs} {
		_, err := db.InsertPowerStateEvent(collector.PowerStateEvent{StartTime: ts, EndTime: ts + 10, Type: "suspend", SuspendSecs: 10})
		if err != nil {
			t.Fatalf("InsertPowerStateEvent(ts=%d): %v", ts, err)
		}
	}

	// process_samples
	err := db.InsertProcessSamples([]collector.ProcessSample{
		{Timestamp: oldTs, PID: 1, Comm: "a", Cmdline: "a", CPUTicksDelta: 1, LastCPU: 0},
		{Timestamp: cutoffTs, PID: 2, Comm: "b", Cmdline: "b", CPUTicksDelta: 1, LastCPU: 1},
		{Timestamp: newTs, PID: 3, Comm: "c", Cmdline: "c", CPUTicksDelta: 1, LastCPU: 2},
	})
	if err != nil {
		t.Fatalf("InsertProcessSamples(): %v", err)
	}

	// cpu_freq_samples
	err = db.InsertCPUFreqSamples([]collector.CPUFreqSample{
		{Timestamp: oldTs, CPUID: 0, FreqKHz: 1000000, IsPCore: true},
		{Timestamp: cutoffTs, CPUID: 1, FreqKHz: 1000000, IsPCore: false},
		{Timestamp: newTs, CPUID: 2, FreqKHz: 1000000, IsPCore: true},
	})
	if err != nil {
		t.Fatalf("InsertCPUFreqSamples(): %v", err)
	}

	deleted, err := db.DeleteOlderThan(cutoffTs)
	if err != nil {
		t.Fatalf("DeleteOlderThan() error = %v", err)
	}
	if deleted != 5 {
		t.Fatalf("DeleteOlderThan() deleted = %d, want 5 (one old row per table)", deleted)
	}

	for _, table := range []string{
		"battery_samples",
		"backlight_samples",
		"power_state_events",
		"process_samples",
		"cpu_freq_samples",
	} {
		if got := countRows(t, db, table); got != 2 {
			t.Fatalf("%s row count after cleanup = %d, want 2 (cutoff+new)", table, got)
		}
	}
}
