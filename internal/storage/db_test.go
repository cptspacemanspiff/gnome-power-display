package storage

import (
	"path/filepath"
	"testing"

	"github.com/cptspacemanspiff/gnome-power-display/internal/collector"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	return db
}

func TestBatteryRoundTrip(t *testing.T) {
	db := openTestDB(t)

	s1 := collector.BatterySample{Timestamp: 10, VoltageUV: 11000000, CurrentUA: 1000000, PowerUW: 1100000, CapacityPct: 80, Status: "Discharging"}
	s2 := collector.BatterySample{Timestamp: 20, VoltageUV: 12000000, CurrentUA: 1000000, PowerUW: 1200000, CapacityPct: 79, Status: "Discharging"}
	if err := db.InsertBatterySample(s1); err != nil {
		t.Fatalf("InsertBatterySample(s1) error = %v", err)
	}
	if err := db.InsertBatterySample(s2); err != nil {
		t.Fatalf("InsertBatterySample(s2) error = %v", err)
	}

	latest, err := db.LatestBatterySample()
	if err != nil {
		t.Fatalf("LatestBatterySample() error = %v", err)
	}
	if latest == nil || latest.Timestamp != 20 || latest.PowerUW != 1200000 {
		t.Fatalf("LatestBatterySample() = %#v, want timestamp=20 power_uw=1200000", latest)
	}

	ranged, err := db.BatterySamplesInRange(10, 15)
	if err != nil {
		t.Fatalf("BatterySamplesInRange() error = %v", err)
	}
	if len(ranged) != 1 || ranged[0].Timestamp != 10 {
		t.Fatalf("BatterySamplesInRange() = %#v, want one row at ts=10", ranged)
	}
}

func TestBacklightRoundTrip(t *testing.T) {
	db := openTestDB(t)

	s1 := collector.BacklightSample{Timestamp: 11, Brightness: 100, MaxBrightness: 500}
	s2 := collector.BacklightSample{Timestamp: 21, Brightness: 200, MaxBrightness: 500}
	if err := db.InsertBacklightSample(s1); err != nil {
		t.Fatalf("InsertBacklightSample(s1) error = %v", err)
	}
	if err := db.InsertBacklightSample(s2); err != nil {
		t.Fatalf("InsertBacklightSample(s2) error = %v", err)
	}

	latest, err := db.LatestBacklightSample()
	if err != nil {
		t.Fatalf("LatestBacklightSample() error = %v", err)
	}
	if latest == nil || latest.Timestamp != 21 || latest.Brightness != 200 {
		t.Fatalf("LatestBacklightSample() = %#v, want timestamp=21 brightness=200", latest)
	}

	ranged, err := db.BacklightSamplesInRange(11, 11)
	if err != nil {
		t.Fatalf("BacklightSamplesInRange() error = %v", err)
	}
	if len(ranged) != 1 || ranged[0].Timestamp != 11 {
		t.Fatalf("BacklightSamplesInRange() = %#v, want one row at ts=11", ranged)
	}
}

func TestProcessAndCPUFreqRoundTrip(t *testing.T) {
	db := openTestDB(t)

	procSamples := []collector.ProcessSample{
		{Timestamp: 100, PID: 10, Comm: "a", Cmdline: "a --x", CPUTicksDelta: 50, LastCPU: 0},
		{Timestamp: 101, PID: 20, Comm: "b", Cmdline: "b --y", CPUTicksDelta: 70, LastCPU: 1},
	}
	if err := db.InsertProcessSamples(procSamples); err != nil {
		t.Fatalf("InsertProcessSamples() error = %v", err)
	}

	freqSamples := []collector.CPUFreqSample{
		{Timestamp: 100, CPUID: 0, FreqKHz: 2400000, IsPCore: true},
		{Timestamp: 100, CPUID: 1, FreqKHz: 1800000, IsPCore: false},
	}
	if err := db.InsertCPUFreqSamples(freqSamples); err != nil {
		t.Fatalf("InsertCPUFreqSamples() error = %v", err)
	}

	gotProcs, err := db.ProcessSamplesInRange(100, 101)
	if err != nil {
		t.Fatalf("ProcessSamplesInRange() error = %v", err)
	}
	if len(gotProcs) != 2 || gotProcs[0].PID != 10 || gotProcs[1].PID != 20 {
		t.Fatalf("ProcessSamplesInRange() = %#v, want two rows for pids 10,20", gotProcs)
	}

	gotFreqs, err := db.CPUFreqSamplesInRange(100, 100)
	if err != nil {
		t.Fatalf("CPUFreqSamplesInRange() error = %v", err)
	}
	if len(gotFreqs) != 2 {
		t.Fatalf("CPUFreqSamplesInRange() len = %d, want 2", len(gotFreqs))
	}
	if !gotFreqs[0].IsPCore || gotFreqs[1].IsPCore {
		t.Fatalf("CPUFreqSamplesInRange() IsPCore decode mismatch: %#v", gotFreqs)
	}
}

func TestInsertPowerStateEvent_DeduplicatesByStartTime(t *testing.T) {
	db := openTestDB(t)

	e1 := collector.PowerStateEvent{StartTime: 100, EndTime: 120, Type: "suspend", SuspendSecs: 20}
	inserted, err := db.InsertPowerStateEvent(e1)
	if err != nil {
		t.Fatalf("InsertPowerStateEvent(e1) error = %v", err)
	}
	if !inserted {
		t.Fatal("InsertPowerStateEvent(e1) inserted = false, want true")
	}

	e2 := collector.PowerStateEvent{StartTime: 100, EndTime: 999, Type: "hibernate", HibernateSecs: 899}
	inserted, err = db.InsertPowerStateEvent(e2)
	if err != nil {
		t.Fatalf("InsertPowerStateEvent(e2) error = %v", err)
	}
	if inserted {
		t.Fatal("InsertPowerStateEvent(e2) inserted = true, want false for duplicate start_time")
	}

	events, err := db.PowerStateEventsInRange(0, 1000)
	if err != nil {
		t.Fatalf("PowerStateEventsInRange() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("PowerStateEventsInRange() len = %d, want 1", len(events))
	}
	if events[0].Type != "suspend" || events[0].EndTime != 120 {
		t.Fatalf("stored event = %#v, want first event unchanged", events[0])
	}
}

func TestSleepEventsInRange_UnionSemantics(t *testing.T) {
	db := openTestDB(t)

	if err := db.InsertSleepEvent(collector.SleepEvent{SleepTime: 100, WakeTime: 110, Type: "suspend"}); err != nil {
		t.Fatalf("InsertSleepEvent(legacy) error = %v", err)
	}
	if err := db.InsertSleepEvent(collector.SleepEvent{SleepTime: 10, WakeTime: 20, Type: "suspend"}); err != nil {
		t.Fatalf("InsertSleepEvent(outside) error = %v", err)
	}
	if _, err := db.InsertPowerStateEvent(collector.PowerStateEvent{StartTime: 120, EndTime: 130, Type: "hibernate", HibernateSecs: 10}); err != nil {
		t.Fatalf("InsertPowerStateEvent() error = %v", err)
	}
	if _, err := db.InsertPowerStateEvent(collector.PowerStateEvent{StartTime: 300, EndTime: 310, Type: "shutdown"}); err != nil {
		t.Fatalf("InsertPowerStateEvent(outside) error = %v", err)
	}

	events, err := db.SleepEventsInRange(105, 125)
	if err != nil {
		t.Fatalf("SleepEventsInRange() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("SleepEventsInRange() len = %d, want 2", len(events))
	}
	if events[0].SleepTime != 100 || events[0].Type != "suspend" {
		t.Fatalf("events[0] = %#v, want legacy event first", events[0])
	}
	if events[1].SleepTime != 120 || events[1].Type != "hibernate" {
		t.Fatalf("events[1] = %#v, want power_state event second", events[1])
	}
}
