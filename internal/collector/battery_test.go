package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setTestSysfsRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	oldRoot := sysfsRoot
	sysfsRoot = root
	t.Cleanup(func() {
		sysfsRoot = oldRoot
	})

	return root
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func newTestCollector() *BatteryCollector {
	return NewBatteryCollector(30)
}

func TestCollect_ParsesUevent(t *testing.T) {
	root := setTestSysfsRoot(t)
	writeTestFile(t, filepath.Join(root, "class/power_supply/BAT0/uevent"), strings.Join([]string{
		"POWER_SUPPLY_STATUS=Charging",
		"POWER_SUPPLY_VOLTAGE_NOW=12345000",
		"POWER_SUPPLY_CURRENT_NOW=2345000",
		"POWER_SUPPLY_POWER_NOW=3456000",
		"POWER_SUPPLY_CHARGE_NOW=5000000",
		"POWER_SUPPLY_CAPACITY=61",
		"",
	}, "\n"))

	bc := newTestCollector()
	sample, err := bc.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if sample.Timestamp <= 0 {
		t.Fatalf("Timestamp = %d, want > 0", sample.Timestamp)
	}
	if sample.Status != "Charging" {
		t.Fatalf("Status = %q, want Charging", sample.Status)
	}
	if sample.VoltageUV != 12345000 {
		t.Fatalf("VoltageUV = %d, want 12345000", sample.VoltageUV)
	}
	if sample.CurrentUA != 2345000 {
		t.Fatalf("CurrentUA = %d, want 2345000", sample.CurrentUA)
	}
	if sample.SysfsPowerUW != 3456000 {
		t.Fatalf("SysfsPowerUW = %d, want 3456000", sample.SysfsPowerUW)
	}
	// First sample: no history, so PowerUW falls back to sysfs.
	if sample.PowerUW != 3456000 {
		t.Fatalf("PowerUW = %d, want 3456000", sample.PowerUW)
	}
	if sample.ChargeNowUAH != 5000000 {
		t.Fatalf("ChargeNowUAH = %d, want 5000000", sample.ChargeNowUAH)
	}
	if sample.CapacityPct != 61 {
		t.Fatalf("CapacityPct = %d, want 61", sample.CapacityPct)
	}
}

func TestCollect_SysfsPowerFallbackVoltageTimesCurrent(t *testing.T) {
	root := setTestSysfsRoot(t)
	writeTestFile(t, filepath.Join(root, "class/power_supply/BAT0/uevent"), strings.Join([]string{
		"POWER_SUPPLY_STATUS=Discharging",
		"POWER_SUPPLY_VOLTAGE_NOW=12000000",
		"POWER_SUPPLY_CURRENT_NOW=2000000",
		"POWER_SUPPLY_POWER_NOW=0",
		"POWER_SUPPLY_CAPACITY=75",
		"",
	}, "\n"))

	bc := newTestCollector()
	sample, err := bc.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// power_now=0, so sysfs fallback = voltage * current = 12000 * 2000 = 24000000
	if sample.SysfsPowerUW != 24000000 {
		t.Fatalf("SysfsPowerUW = %d, want 24000000", sample.SysfsPowerUW)
	}
	if sample.PowerUW != 24000000 {
		t.Fatalf("PowerUW = %d, want 24000000", sample.PowerUW)
	}
}

func TestCollect_AveragingWindow(t *testing.T) {
	root := setTestSysfsRoot(t)
	writeTestFile(t, filepath.Join(root, "class/power_supply/BAT0/uevent"), strings.Join([]string{
		"POWER_SUPPLY_STATUS=Discharging",
		"POWER_SUPPLY_VOLTAGE_NOW=12000000",
		"POWER_SUPPLY_CURRENT_NOW=1000000",
		"POWER_SUPPLY_POWER_NOW=5000000",
		"POWER_SUPPLY_CHARGE_NOW=5000000",
		"POWER_SUPPLY_CAPACITY=75",
		"",
	}, "\n"))

	bc := NewBatteryCollector(60)

	// Seed history directly to simulate multiple past readings.
	bc.history = []historyEntry{
		{timestamp: 100, chargeUAH: 5010000, voltageUV: 12000000},
		{timestamp: 110, chargeUAH: 5005000, voltageUV: 12000000},
		{timestamp: 120, chargeUAH: 5000000, voltageUV: 12000000},
	}

	// Override Collect to use a controlled timestamp by injecting directly.
	// Instead, let's test the averaging math directly by calling Collect
	// and checking history state. Since time.Now() gives real time, we'll
	// test the struct method behavior with seeded history.
	// Actually, let's just test the core logic by adding one more entry.
	// The real timestamp will be ~now, too far from ts=100-120.
	// So let's test with history entries close to now.

	now := sample(t, root, bc)

	// With only 1 real entry, no averaging possible, falls back to sysfs.
	if now.PowerUW != 5000000 {
		t.Fatalf("PowerUW = %d, want 5000000 (sysfs fallback, history cleared due to gap)", now.PowerUW)
	}

	// Now do a second collection with slightly different charge.
	writeTestFile(t, filepath.Join(root, "class/power_supply/BAT0/uevent"), strings.Join([]string{
		"POWER_SUPPLY_STATUS=Discharging",
		"POWER_SUPPLY_VOLTAGE_NOW=12000000",
		"POWER_SUPPLY_CURRENT_NOW=1000000",
		"POWER_SUPPLY_POWER_NOW=5000000",
		"POWER_SUPPLY_CHARGE_NOW=4990000",
		"POWER_SUPPLY_CAPACITY=74",
		"",
	}, "\n"))

	second, err := bc.Collect()
	if err != nil {
		t.Fatalf("second Collect() error = %v", err)
	}

	deltaTime := second.Timestamp - now.Timestamp
	if deltaTime == 0 {
		// Same second, falls back to sysfs.
		if second.PowerUW != 5000000 {
			t.Fatalf("PowerUW = %d, want 5000000 (same-second fallback)", second.PowerUW)
		}
	} else {
		// deltaCharge = 5000000 - 4990000 = 10000 µAh
		// avgVoltage = 12000000 µV
		// power = 10000 * 12000 * 3600 / (deltaTime * 1000)
		expected := (int64(10000) * 12000 * 3600) / (deltaTime * 1000)
		if second.PowerUW != expected {
			t.Fatalf("PowerUW = %d, want %d", second.PowerUW, expected)
		}
	}
}

func sample(t *testing.T, root string, bc *BatteryCollector) *BatterySample {
	t.Helper()
	s, err := bc.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return s
}

func TestCollect_GapClearsHistory(t *testing.T) {
	root := setTestSysfsRoot(t)
	writeTestFile(t, filepath.Join(root, "class/power_supply/BAT0/uevent"), strings.Join([]string{
		"POWER_SUPPLY_STATUS=Discharging",
		"POWER_SUPPLY_VOLTAGE_NOW=12000000",
		"POWER_SUPPLY_CURRENT_NOW=1000000",
		"POWER_SUPPLY_POWER_NOW=7000000",
		"POWER_SUPPLY_CHARGE_NOW=5000000",
		"POWER_SUPPLY_CAPACITY=75",
		"",
	}, "\n"))

	bc := NewBatteryCollector(30)
	// Seed with ancient history entry — gap > 2×window.
	bc.history = []historyEntry{
		{timestamp: 1, chargeUAH: 5100000, voltageUV: 12000000},
	}

	s, err := bc.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// Gap should clear history, so only 1 entry, falls back to sysfs.
	if s.PowerUW != 7000000 {
		t.Fatalf("PowerUW = %d, want 7000000 (sysfs fallback after gap clear)", s.PowerUW)
	}
	if len(bc.history) != 1 {
		t.Fatalf("history len = %d, want 1 (gap cleared old, added current)", len(bc.history))
	}
}

func TestCollect_CorrectsStatusToFullWhenACOnline(t *testing.T) {
	root := setTestSysfsRoot(t)
	writeTestFile(t, filepath.Join(root, "class/power_supply/BAT0/uevent"), strings.Join([]string{
		"POWER_SUPPLY_STATUS=Discharging",
		"POWER_SUPPLY_VOLTAGE_NOW=11000000",
		"POWER_SUPPLY_CURRENT_NOW=1000000",
		"POWER_SUPPLY_POWER_NOW=1100000",
		"POWER_SUPPLY_CAPACITY=100",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "class/power_supply/AC0/online"), "1\n")

	bc := newTestCollector()
	s, err := bc.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if s.Status != "Full" {
		t.Fatalf("Status = %q, want Full", s.Status)
	}
}

func TestCollect_LeavesStatusWhenACOffline(t *testing.T) {
	root := setTestSysfsRoot(t)
	writeTestFile(t, filepath.Join(root, "class/power_supply/BAT0/uevent"), strings.Join([]string{
		"POWER_SUPPLY_STATUS=Discharging",
		"POWER_SUPPLY_VOLTAGE_NOW=11000000",
		"POWER_SUPPLY_CURRENT_NOW=1000000",
		"POWER_SUPPLY_POWER_NOW=1100000",
		"POWER_SUPPLY_CAPACITY=100",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(root, "class/power_supply/AC0/online"), "0\n")

	bc := newTestCollector()
	s, err := bc.Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if s.Status != "Discharging" {
		t.Fatalf("Status = %q, want Discharging", s.Status)
	}
}

func TestCollect_NoBatteryFound(t *testing.T) {
	_ = setTestSysfsRoot(t)

	bc := newTestCollector()
	_, err := bc.Collect()
	if err == nil {
		t.Fatal("Collect() error = nil, want no battery found error")
	}
	if !strings.Contains(err.Error(), "no battery found") {
		t.Fatalf("Collect() error = %q, want contains %q", err.Error(), "no battery found")
	}
}

func TestCollect_UeventReadError(t *testing.T) {
	root := setTestSysfsRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "class/power_supply/BAT0"), 0o755); err != nil {
		t.Fatalf("mkdir BAT0: %v", err)
	}

	bc := newTestCollector()
	_, err := bc.Collect()
	if err == nil {
		t.Fatal("Collect() error = nil, want read uevent error")
	}
	if !strings.Contains(err.Error(), "read uevent") {
		t.Fatalf("Collect() error = %q, want contains %q", err.Error(), "read uevent")
	}
}
