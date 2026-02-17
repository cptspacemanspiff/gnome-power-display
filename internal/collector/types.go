package collector

// BatterySample holds a snapshot of battery state from /sys/class/power_supply/BAT*.
type BatterySample struct {
	Timestamp            int64  `json:"timestamp"`
	VoltageUV            int64  `json:"voltage_uv"`
	CurrentUA            int64  `json:"current_ua"`
	PowerUW              int64  `json:"power_uw"`
	PowerFromChargeDelta bool   `json:"power_from_charge_delta"`
	SysfsPowerUW         int64  `json:"sysfs_power_uw"`
	ChargeNowUAH         int64  `json:"charge_now_uah"`
	CapacityPct          int    `json:"capacity_pct"`
	Status               string `json:"status"`
}

// BacklightSample holds a snapshot of display backlight state.
type BacklightSample struct {
	Timestamp     int64 `json:"timestamp"`
	Brightness    int64 `json:"brightness"`
	MaxBrightness int64 `json:"max_brightness"`
}

// PowerStateEvent records a power state transition (suspend, hibernate, shutdown, etc.).
type PowerStateEvent struct {
	StartTime     int64  `json:"start_time"`
	EndTime       int64  `json:"end_time"`
	Type          string `json:"type"`           // "suspend", "hibernate", "suspend-then-hibernate", "shutdown"
	SuspendSecs   int64  `json:"suspend_secs"`   // seconds in suspend phase (0 if pure hibernate/shutdown)
	HibernateSecs int64  `json:"hibernate_secs"` // seconds in hibernate phase (0 if pure suspend/shutdown)
}

// ProcessSample holds a per-process CPU usage snapshot for one sampling interval.
type ProcessSample struct {
	Timestamp     int64  `json:"timestamp"`
	PID           int    `json:"pid"`
	Comm          string `json:"comm"`
	Cmdline       string `json:"cmdline"`
	CPUTicksDelta int64  `json:"cpu_ticks_delta"`
	LastCPU       int    `json:"last_cpu"`
}

// CPUFreqSample holds the frequency of a single CPU core at a point in time.
type CPUFreqSample struct {
	Timestamp int64 `json:"timestamp"`
	CPUID     int   `json:"cpu_id"`
	FreqKHz   int64 `json:"freq_khz"`
	IsPCore   bool  `json:"is_p_core"`
}
