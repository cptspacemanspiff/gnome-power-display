package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// CollectBattery reads battery info from /sys/class/power_supply/BAT*.
func CollectBattery() (*BatterySample, error) {
	matches, err := filepath.Glob("/sys/class/power_supply/BAT*")
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
	s.PowerUW, _ = strconv.ParseInt(props["POWER_SUPPLY_POWER_NOW"], 10, 64)
	cap, _ := strconv.ParseInt(props["POWER_SUPPLY_CAPACITY"], 10, 64)
	s.CapacityPct = int(cap)

	// If power_now isn't reported, compute from voltage * current.
	if s.PowerUW == 0 && s.VoltageUV > 0 && s.CurrentUA > 0 {
		s.PowerUW = (s.VoltageUV / 1000) * (s.CurrentUA / 1000)
	}

	// Some firmware reports "Discharging" at full capacity while on AC power.
	// Detect this and correct to "Full".
	if s.Status == "Discharging" && s.CapacityPct >= 100 && isACOnline() {
		s.Status = "Full"
	}

	return s, nil
}

// isACOnline checks if any AC adapter is online.
func isACOnline() bool {
	matches, err := filepath.Glob("/sys/class/power_supply/AC*/online")
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
