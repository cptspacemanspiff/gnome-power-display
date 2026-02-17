package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// CollectBatteryHealth reads battery identity and health info from sysfs.
func CollectBatteryHealth() (*BatteryHealth, error) {
	matches, err := filepath.Glob(filepath.Join(sysfsRoot, "class/power_supply/BAT*"))
	if err != nil {
		return nil, fmt.Errorf("glob battery: %w", err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no battery found")
	}

	data, err := os.ReadFile(filepath.Join(matches[0], "uevent"))
	if err != nil {
		return nil, fmt.Errorf("read uevent: %w", err)
	}

	props := parseUevent(string(data))
	h := &BatteryHealth{
		Manufacturer: props["POWER_SUPPLY_MANUFACTURER"],
		Model:        props["POWER_SUPPLY_MODEL_NAME"],
		Serial:       props["POWER_SUPPLY_SERIAL_NUMBER"],
		Technology:   props["POWER_SUPPLY_TECHNOLOGY"],
	}
	h.CycleCount, _ = strconv.ParseInt(props["POWER_SUPPLY_CYCLE_COUNT"], 10, 64)
	h.ChargeFullDesignUAH, _ = strconv.ParseInt(props["POWER_SUPPLY_CHARGE_FULL_DESIGN"], 10, 64)
	h.ChargeFullUAH, _ = strconv.ParseInt(props["POWER_SUPPLY_CHARGE_FULL"], 10, 64)
	h.VoltageMinDesignUV, _ = strconv.ParseInt(props["POWER_SUPPLY_VOLTAGE_MIN_DESIGN"], 10, 64)

	return h, nil
}
