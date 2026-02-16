package collector

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectBacklight_ParsesValues(t *testing.T) {
	root := setTestSysfsRoot(t)
	dir := filepath.Join(root, "class/backlight/intel_backlight")
	writeTestFile(t, filepath.Join(dir, "brightness"), "123\n")
	writeTestFile(t, filepath.Join(dir, "max_brightness"), "456\n")

	sample, err := CollectBacklight()
	if err != nil {
		t.Fatalf("CollectBacklight() error = %v", err)
	}

	if sample.Timestamp <= 0 {
		t.Fatalf("Timestamp = %d, want > 0", sample.Timestamp)
	}
	if sample.Brightness != 123 {
		t.Fatalf("Brightness = %d, want 123", sample.Brightness)
	}
	if sample.MaxBrightness != 456 {
		t.Fatalf("MaxBrightness = %d, want 456", sample.MaxBrightness)
	}
}

func TestCollectBacklight_NoBacklightFound(t *testing.T) {
	_ = setTestSysfsRoot(t)

	_, err := CollectBacklight()
	if err == nil {
		t.Fatal("CollectBacklight() error = nil, want no backlight found error")
	}
	if !strings.Contains(err.Error(), "no backlight found") {
		t.Fatalf("CollectBacklight() error = %q, want contains %q", err.Error(), "no backlight found")
	}
}

func TestCollectBacklight_BrightnessReadError(t *testing.T) {
	root := setTestSysfsRoot(t)
	dir := filepath.Join(root, "class/backlight/intel_backlight")
	writeTestFile(t, filepath.Join(dir, "max_brightness"), "456\n")

	_, err := CollectBacklight()
	if err == nil {
		t.Fatal("CollectBacklight() error = nil, want read brightness error")
	}
	if !strings.Contains(err.Error(), "read brightness") {
		t.Fatalf("CollectBacklight() error = %q, want contains %q", err.Error(), "read brightness")
	}
}

func TestCollectBacklight_MaxBrightnessReadError(t *testing.T) {
	root := setTestSysfsRoot(t)
	dir := filepath.Join(root, "class/backlight/intel_backlight")
	writeTestFile(t, filepath.Join(dir, "brightness"), "123\n")

	_, err := CollectBacklight()
	if err == nil {
		t.Fatal("CollectBacklight() error = nil, want read max_brightness error")
	}
	if !strings.Contains(err.Error(), "read max_brightness") {
		t.Fatalf("CollectBacklight() error = %q, want contains %q", err.Error(), "read max_brightness")
	}
}

func TestCollectBacklight_InvalidBrightnessValue(t *testing.T) {
	root := setTestSysfsRoot(t)
	dir := filepath.Join(root, "class/backlight/intel_backlight")
	writeTestFile(t, filepath.Join(dir, "brightness"), "not-a-number\n")
	writeTestFile(t, filepath.Join(dir, "max_brightness"), "456\n")

	_, err := CollectBacklight()
	if err == nil {
		t.Fatal("CollectBacklight() error = nil, want parse error")
	}
	if !strings.Contains(err.Error(), "read brightness") {
		t.Fatalf("CollectBacklight() error = %q, want contains %q", err.Error(), "read brightness")
	}
}
