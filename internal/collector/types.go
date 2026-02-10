package collector

// BatterySample holds a snapshot of battery state from /sys/class/power_supply/BAT*.
type BatterySample struct {
	Timestamp   int64  `json:"timestamp"`
	VoltageUV   int64  `json:"voltage_uv"`
	CurrentUA   int64  `json:"current_ua"`
	PowerUW     int64  `json:"power_uw"`
	CapacityPct int    `json:"capacity_pct"`
	Status      string `json:"status"`
}

// BacklightSample holds a snapshot of display backlight state.
type BacklightSample struct {
	Timestamp     int64 `json:"timestamp"`
	Brightness    int64 `json:"brightness"`
	MaxBrightness int64 `json:"max_brightness"`
}

// SleepEvent records a sleep/wake cycle.
type SleepEvent struct {
	SleepTime int64  `json:"sleep_time"`
	WakeTime  int64  `json:"wake_time"`
	Type      string `json:"type"` // "suspend", "hibernate", or "unknown"
}
