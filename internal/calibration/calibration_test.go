package calibration

import (
	"testing"
	"time"

	"github.com/cptspacemanspiff/gnome-power-display/internal/collector"
)

type fakeBatterySampler struct {
	samples []*collector.BatterySample
	idx     int
}

func (f *fakeBatterySampler) Collect() (*collector.BatterySample, error) {
	if len(f.samples) == 0 {
		return &collector.BatterySample{}, nil
	}
	if f.idx >= len(f.samples) {
		return f.samples[len(f.samples)-1], nil
	}
	s := f.samples[f.idx]
	f.idx++
	return s, nil
}

func TestMeasurePowerOverWindow_UsesChargeDelta(t *testing.T) {
	bs := &fakeBatterySampler{samples: []*collector.BatterySample{
		{ChargeNowUAH: 5000000, VoltageUV: 12000000, PowerUW: 0},
		{ChargeNowUAH: 4999900, VoltageUV: 12000000, PowerUW: 0},
	}}

	powerUW, err := MeasurePowerOverWindow(bs, 10*time.Millisecond, 2*time.Millisecond)
	if err != nil {
		t.Fatalf("MeasurePowerOverWindow() error = %v", err)
	}
	if powerUW <= 0 {
		t.Fatalf("MeasurePowerOverWindow() = %d, want > 0", powerUW)
	}
}

func TestMeasurePowerOverWindow_FallsBackToSampledPower(t *testing.T) {
	bs := &fakeBatterySampler{samples: []*collector.BatterySample{
		{ChargeNowUAH: 5000000, VoltageUV: 12000000, PowerUW: 4000000},
		{ChargeNowUAH: 5000000, VoltageUV: 12000000, PowerUW: 6000000},
	}}

	powerUW, err := MeasurePowerOverWindow(bs, 10*time.Millisecond, 2*time.Millisecond)
	if err != nil {
		t.Fatalf("MeasurePowerOverWindow() error = %v", err)
	}
	if powerUW <= 0 {
		t.Fatalf("MeasurePowerOverWindow() = %d, want > 0", powerUW)
	}
	if powerUW < 4000000 || powerUW > 6000000 {
		t.Fatalf("MeasurePowerOverWindow() = %d, want between 4000000 and 6000000", powerUW)
	}
}

func TestMeasurePowerOverWindow_ValidatesArguments(t *testing.T) {
	bs := &fakeBatterySampler{samples: []*collector.BatterySample{{PowerUW: 1000000}}}

	if _, err := MeasurePowerOverWindow(bs, 0, time.Millisecond); err == nil {
		t.Fatal("expected error for zero window")
	}
	if _, err := MeasurePowerOverWindow(bs, time.Second, 0); err == nil {
		t.Fatal("expected error for zero poll interval")
	}
}
