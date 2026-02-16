package dbus

import (
	"encoding/json"
	"path/filepath"
	"testing"

	godbus "github.com/godbus/dbus/v5"

	"github.com/cptspacemanspiff/gnome-power-display/internal/collector"
	"github.com/cptspacemanspiff/gnome-power-display/internal/storage"
)

func newTestService(t *testing.T) (*Service, *storage.DB) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatalf("storage.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("db.Close() error = %v", err)
		}
	})

	return NewService(db), db
}

func TestService_InvalidTimeRanges(t *testing.T) {
	svc, _ := newTestService(t)

	tests := []struct {
		name string
		call func() *godbus.Error
	}{
		{
			name: "GetHistory negative from",
			call: func() *godbus.Error {
				_, err := svc.GetHistory(-1, 0)
				return err
			},
		},
		{
			name: "GetHistory to before from",
			call: func() *godbus.Error {
				_, err := svc.GetHistory(10, 9)
				return err
			},
		},
		{
			name: "GetHistory range too large",
			call: func() *godbus.Error {
				_, err := svc.GetHistory(0, 86400*366)
				return err
			},
		},
		{
			name: "GetSleepEvents negative from",
			call: func() *godbus.Error {
				_, err := svc.GetSleepEvents(-1, 0)
				return err
			},
		},
		{
			name: "GetSleepEvents to before from",
			call: func() *godbus.Error {
				_, err := svc.GetSleepEvents(10, 9)
				return err
			},
		},
		{
			name: "GetSleepEvents range too large",
			call: func() *godbus.Error {
				_, err := svc.GetSleepEvents(0, 86400*366)
				return err
			},
		},
		{
			name: "GetProcessHistory negative from",
			call: func() *godbus.Error {
				_, err := svc.GetProcessHistory(-1, 0)
				return err
			},
		},
		{
			name: "GetProcessHistory to before from",
			call: func() *godbus.Error {
				_, err := svc.GetProcessHistory(10, 9)
				return err
			},
		},
		{
			name: "GetProcessHistory range too large",
			call: func() *godbus.Error {
				_, err := svc.GetProcessHistory(0, 86400*366)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err == nil {
				t.Fatal("expected D-Bus error, got nil")
			}
		})
	}
}

func TestService_SuccessJSONShapes(t *testing.T) {
	svc, db := newTestService(t)

	if err := db.InsertBatterySample(collector.BatterySample{Timestamp: 100, VoltageUV: 11000000, CurrentUA: 1000000, PowerUW: 1100000, CapacityPct: 80, Status: "Discharging"}); err != nil {
		t.Fatalf("InsertBatterySample() error = %v", err)
	}
	if err := db.InsertBacklightSample(collector.BacklightSample{Timestamp: 100, Brightness: 200, MaxBrightness: 500}); err != nil {
		t.Fatalf("InsertBacklightSample() error = %v", err)
	}
	if err := db.InsertSleepEvent(collector.SleepEvent{SleepTime: 90, WakeTime: 95, Type: "suspend"}); err != nil {
		t.Fatalf("InsertSleepEvent() error = %v", err)
	}
	if err := db.InsertProcessSamples([]collector.ProcessSample{{Timestamp: 100, PID: 1, Comm: "a", Cmdline: "a", CPUTicksDelta: 10, LastCPU: 0}}); err != nil {
		t.Fatalf("InsertProcessSamples() error = %v", err)
	}
	if err := db.InsertCPUFreqSamples([]collector.CPUFreqSample{{Timestamp: 100, CPUID: 0, FreqKHz: 2400000, IsPCore: true}}); err != nil {
		t.Fatalf("InsertCPUFreqSamples() error = %v", err)
	}

	currentJSON, dbusErr := svc.GetCurrentStats()
	if dbusErr != nil {
		t.Fatalf("GetCurrentStats() error = %v", dbusErr)
	}
	var current map[string]json.RawMessage
	if err := json.Unmarshal([]byte(currentJSON), &current); err != nil {
		t.Fatalf("unmarshal current JSON: %v", err)
	}
	if _, ok := current["battery"]; !ok {
		t.Fatalf("current JSON missing key %q: %s", "battery", currentJSON)
	}
	if _, ok := current["backlight"]; !ok {
		t.Fatalf("current JSON missing key %q: %s", "backlight", currentJSON)
	}

	historyJSON, dbusErr := svc.GetHistory(0, 200)
	if dbusErr != nil {
		t.Fatalf("GetHistory() error = %v", dbusErr)
	}
	var history map[string]json.RawMessage
	if err := json.Unmarshal([]byte(historyJSON), &history); err != nil {
		t.Fatalf("unmarshal history JSON: %v", err)
	}
	if _, ok := history["battery"]; !ok {
		t.Fatalf("history JSON missing key %q: %s", "battery", historyJSON)
	}
	if _, ok := history["backlight"]; !ok {
		t.Fatalf("history JSON missing key %q: %s", "backlight", historyJSON)
	}

	sleepJSON, dbusErr := svc.GetSleepEvents(0, 200)
	if dbusErr != nil {
		t.Fatalf("GetSleepEvents() error = %v", dbusErr)
	}
	var sleepArr []map[string]any
	if err := json.Unmarshal([]byte(sleepJSON), &sleepArr); err != nil {
		t.Fatalf("unmarshal sleep JSON array: %v", err)
	}

	procJSON, dbusErr := svc.GetProcessHistory(0, 200)
	if dbusErr != nil {
		t.Fatalf("GetProcessHistory() error = %v", dbusErr)
	}
	var proc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(procJSON), &proc); err != nil {
		t.Fatalf("unmarshal process JSON: %v", err)
	}
	if _, ok := proc["processes"]; !ok {
		t.Fatalf("process JSON missing key %q: %s", "processes", procJSON)
	}
	if _, ok := proc["cpu_freq"]; !ok {
		t.Fatalf("process JSON missing key %q: %s", "cpu_freq", procJSON)
	}
}
