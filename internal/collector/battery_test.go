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

func TestCollectBattery_ParsesUevent(t *testing.T) {
	root := setTestSysfsRoot(t)
	writeTestFile(t, filepath.Join(root, "class/power_supply/BAT0/uevent"), strings.Join([]string{
		"POWER_SUPPLY_STATUS=Charging",
		"POWER_SUPPLY_VOLTAGE_NOW=12345000",
		"POWER_SUPPLY_CURRENT_NOW=2345000",
		"POWER_SUPPLY_POWER_NOW=3456000",
		"POWER_SUPPLY_CAPACITY=61",
		"",
	}, "\n"))

	sample, err := CollectBattery()
	if err != nil {
		t.Fatalf("CollectBattery() error = %v", err)
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
	if sample.PowerUW != 3456000 {
		t.Fatalf("PowerUW = %d, want 3456000", sample.PowerUW)
	}
	if sample.CapacityPct != 61 {
		t.Fatalf("CapacityPct = %d, want 61", sample.CapacityPct)
	}
}

func TestCollectBattery_ComputesFallbackPower(t *testing.T) {
	root := setTestSysfsRoot(t)
	writeTestFile(t, filepath.Join(root, "class/power_supply/BAT0/uevent"), strings.Join([]string{
		"POWER_SUPPLY_STATUS=Discharging",
		"POWER_SUPPLY_VOLTAGE_NOW=12000000",
		"POWER_SUPPLY_CURRENT_NOW=2000000",
		"POWER_SUPPLY_POWER_NOW=0",
		"POWER_SUPPLY_CAPACITY=75",
		"",
	}, "\n"))

	sample, err := CollectBattery()
	if err != nil {
		t.Fatalf("CollectBattery() error = %v", err)
	}

	if sample.PowerUW != 24000000 {
		t.Fatalf("PowerUW = %d, want 24000000", sample.PowerUW)
	}
}

func TestCollectBattery_CorrectsStatusToFullWhenACOnline(t *testing.T) {
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

	sample, err := CollectBattery()
	if err != nil {
		t.Fatalf("CollectBattery() error = %v", err)
	}

	if sample.Status != "Full" {
		t.Fatalf("Status = %q, want Full", sample.Status)
	}
}

func TestCollectBattery_LeavesStatusWhenACOffline(t *testing.T) {
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

	sample, err := CollectBattery()
	if err != nil {
		t.Fatalf("CollectBattery() error = %v", err)
	}

	if sample.Status != "Discharging" {
		t.Fatalf("Status = %q, want Discharging", sample.Status)
	}
}

func TestCollectBattery_NoBatteryFound(t *testing.T) {
	_ = setTestSysfsRoot(t)

	_, err := CollectBattery()
	if err == nil {
		t.Fatal("CollectBattery() error = nil, want no battery found error")
	}
	if !strings.Contains(err.Error(), "no battery found") {
		t.Fatalf("CollectBattery() error = %q, want contains %q", err.Error(), "no battery found")
	}
}

func TestCollectBattery_UeventReadError(t *testing.T) {
	root := setTestSysfsRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "class/power_supply/BAT0"), 0o755); err != nil {
		t.Fatalf("mkdir BAT0: %v", err)
	}

	_, err := CollectBattery()
	if err == nil {
		t.Fatal("CollectBattery() error = nil, want read uevent error")
	}
	if !strings.Contains(err.Error(), "read uevent") {
		t.Fatalf("CollectBattery() error = %q, want contains %q", err.Error(), "read uevent")
	}
}
