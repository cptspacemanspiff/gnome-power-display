package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var sysfsRoot = "/sys"

// historyEntry records a charge/voltage reading at a point in time.
type historyEntry struct {
	timestamp  int64
	chargeUAH  int64
	voltageUV  int64
}

// BatteryCollector tracks battery readings and computes averaged power from
// charge deltas over a configurable time window.
type BatteryCollector struct {
	windowSec int64
	history   []historyEntry
}

// NewBatteryCollector creates a BatteryCollector that averages charge deltas
// over the given window (in seconds).
func NewBatteryCollector(windowSec int64) *BatteryCollector {
	return &BatteryCollector{windowSec: windowSec}
}

// Collect reads battery info from /sys/class/power_supply/BAT* and computes
// power from charge deltas averaged over the configured window.
func (bc *BatteryCollector) Collect() (*BatterySample, error) {
	matches, err := filepath.Glob(filepath.Join(sysfsRoot, "class/power_supply/BAT*"))
	if err != nil {
		return nil, fmt.Errorf("glob battery: %w", err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no battery found")
	}

	ueventPath := filepath.Join(matches[0], "uevent")
	data, err := os.ReadFile(ueventPath)
	if err != nil {
		return nil, fmt.Errorf("read uevent: %w", err)
	}

	props := parseUevent(string(data))
	s := &BatterySample{
		Timestamp: time.Now().Unix(),
		Status:    props["POWER_SUPPLY_STATUS"],
	}
	s.VoltageUV, _ = strconv.ParseInt(props["POWER_SUPPLY_VOLTAGE_NOW"], 10, 64)
	s.CurrentUA, _ = strconv.ParseInt(props["POWER_SUPPLY_CURRENT_NOW"], 10, 64)
	s.ChargeNowUAH, _ = strconv.ParseInt(props["POWER_SUPPLY_CHARGE_NOW"], 10, 64)
	cap, _ := strconv.ParseInt(props["POWER_SUPPLY_CAPACITY"], 10, 64)
	s.CapacityPct = int(cap)

	// Compute sysfs power: prefer power_now, fall back to voltage × current.
	sysfsPower, _ := strconv.ParseInt(props["POWER_SUPPLY_POWER_NOW"], 10, 64)
	if sysfsPower == 0 && s.VoltageUV > 0 && s.CurrentUA > 0 {
		sysfsPower = (s.VoltageUV / 1000) * (s.CurrentUA / 1000)
	}
	s.SysfsPowerUW = sysfsPower

	// Gap detection: if the last history entry is too old, clear history.
	if len(bc.history) > 0 {
		last := bc.history[len(bc.history)-1]
		if s.Timestamp-last.timestamp > 2*bc.windowSec {
			bc.history = bc.history[:0]
		}
	}

	// Append current reading to history.
	if s.ChargeNowUAH > 0 {
		bc.history = append(bc.history, historyEntry{
			timestamp: s.Timestamp,
			chargeUAH: s.ChargeNowUAH,
			voltageUV: s.VoltageUV,
		})
	}

	// Prune entries older than the window.
	cutoff := s.Timestamp - bc.windowSec
	pruneIdx := 0
	for pruneIdx < len(bc.history) && bc.history[pruneIdx].timestamp < cutoff {
		pruneIdx++
	}
	// Keep at least the oldest entry at or before cutoff for a full window span.
	if pruneIdx > 0 && pruneIdx < len(bc.history) {
		pruneIdx-- // keep one entry before cutoff
	}
	if pruneIdx > 0 {
		bc.history = bc.history[pruneIdx:]
	}

	// Compute averaged power from oldest to newest history entry.
	if len(bc.history) >= 2 {
		oldest := bc.history[0]
		newest := bc.history[len(bc.history)-1]
		deltaTimeSec := newest.timestamp - oldest.timestamp
		if deltaTimeSec > 0 {
			deltaCharge := oldest.chargeUAH - newest.chargeUAH // positive when discharging
			if deltaCharge < 0 {
				deltaCharge = -deltaCharge
			}
			// Average voltage across all entries in window.
			var voltageSum int64
			for _, e := range bc.history {
				voltageSum += e.voltageUV
			}
			avgVoltageUV := voltageSum / int64(len(bc.history))
			if avgVoltageUV > 0 {
				s.PowerUW = (deltaCharge * (avgVoltageUV / 1000) * 3600) / (deltaTimeSec * 1000)
			}
		}
	}

	// Fall back to sysfs power if not enough history for averaging.
	if s.PowerUW == 0 {
		s.PowerUW = s.SysfsPowerUW
	}

	// Some firmware reports "Discharging" at full capacity while on AC power.
	if s.Status == "Discharging" && s.CapacityPct >= 100 && isACOnline() {
		s.Status = "Full"
	}

	return s, nil
}

// isACOnline checks if any AC adapter is online.
func isACOnline() bool {
	matches, err := filepath.Glob(filepath.Join(sysfsRoot, "class/power_supply/AC*/online"))
	if err != nil {
		return false
	}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err == nil && strings.TrimSpace(string(data)) == "1" {
			return true
		}
	}
	return false
}

func parseUevent(data string) map[string]string {
	props := make(map[string]string)
	for _, line := range strings.Split(data, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			props[k] = v
		}
	}
	return props
}
