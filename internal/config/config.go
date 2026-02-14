package config

import (
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Storage    StorageConfig    `toml:"storage"`
	Collection CollectionConfig `toml:"collection"`
	Cleanup    CleanupConfig    `toml:"cleanup"`
}

type StorageConfig struct {
	DBPath       string `toml:"db_path"`
	StateLogPath string `toml:"state_log_path"`
}

type CollectionConfig struct {
	IntervalSeconds               int `toml:"interval_seconds"`
	TopProcesses                  int `toml:"top_processes"`
	WallClockJumpThresholdSeconds int `toml:"wall_clock_jump_threshold_seconds"`
}

type CleanupConfig struct {
	RetentionDays int `toml:"retention_days"`
	IntervalHours int `toml:"interval_hours"`
}

func DefaultConfig() *Config {
	return &Config{
		Storage: StorageConfig{
			DBPath:       "/var/lib/power-monitor/data.db",
			StateLogPath: "/var/lib/power-monitor/state-log.jsonl",
		},
		Collection: CollectionConfig{
			IntervalSeconds:               5,
			TopProcesses:                  10,
			WallClockJumpThresholdSeconds: 15,
		},
		Cleanup: CleanupConfig{
			RetentionDays: 30,
			IntervalHours: 24,
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
