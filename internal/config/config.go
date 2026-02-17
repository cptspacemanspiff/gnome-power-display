package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	minCollectionIntervalSeconds = 1
	maxCollectionIntervalSeconds = 3600
	minTopProcesses              = 1
	maxTopProcesses              = 500
	minWallClockJumpSeconds      = 1
	maxWallClockJumpSeconds      = 3600
	minPowerAverageSeconds       = 1
	maxPowerAverageSeconds       = 3600
	minRetentionDays             = 1
	maxRetentionDays             = 3650
	minCleanupIntervalHours      = 1
	maxCleanupIntervalHours      = 720
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
	PowerAverageSeconds           int `toml:"power_average_seconds"`
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
			PowerAverageSeconds:           30,
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

	return NormalizeAndValidate(cfg)
}

func NormalizeAndValidate(cfg *Config) (*Config, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}

	sanitized := *cfg

	var err error
	sanitized.Storage.DBPath, err = sanitizePath("storage.db_path", sanitized.Storage.DBPath)
	if err != nil {
		return nil, err
	}
	sanitized.Storage.StateLogPath, err = sanitizePath("storage.state_log_path", sanitized.Storage.StateLogPath)
	if err != nil {
		return nil, err
	}

	if err := validateRange("collection.interval_seconds", sanitized.Collection.IntervalSeconds, minCollectionIntervalSeconds, maxCollectionIntervalSeconds); err != nil {
		return nil, err
	}
	if err := validateRange("collection.top_processes", sanitized.Collection.TopProcesses, minTopProcesses, maxTopProcesses); err != nil {
		return nil, err
	}
	if err := validateRange("collection.wall_clock_jump_threshold_seconds", sanitized.Collection.WallClockJumpThresholdSeconds, minWallClockJumpSeconds, maxWallClockJumpSeconds); err != nil {
		return nil, err
	}
	if err := validateRange("collection.power_average_seconds", sanitized.Collection.PowerAverageSeconds, minPowerAverageSeconds, maxPowerAverageSeconds); err != nil {
		return nil, err
	}
	if err := validateRange("cleanup.retention_days", sanitized.Cleanup.RetentionDays, minRetentionDays, maxRetentionDays); err != nil {
		return nil, err
	}
	if err := validateRange("cleanup.interval_hours", sanitized.Cleanup.IntervalHours, minCleanupIntervalHours, maxCleanupIntervalHours); err != nil {
		return nil, err
	}

	return &sanitized, nil
}

func Save(path string, cfg *Config) error {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return fmt.Errorf("config path must not be empty")
	}

	sanitized, err := NormalizeAndValidate(cfg)
	if err != nil {
		return err
	}

	var data bytes.Buffer
	if err := toml.NewEncoder(&data).Encode(sanitized); err != nil {
		return fmt.Errorf("encode config TOML: %w", err)
	}

	dir := filepath.Dir(trimmedPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return fmt.Errorf("create temp config file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data.Bytes()); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp config file: %w", err)
	}
	if err := tmpFile.Chmod(0o644); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("chmod temp config file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp config file: %w", err)
	}
	if err := os.Rename(tmpPath, trimmedPath); err != nil {
		return fmt.Errorf("replace config file: %w", err)
	}
	tmpPath = ""

	return nil
}

func sanitizePath(name, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s must not be empty", name)
	}
	cleaned := filepath.Clean(trimmed)
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("%s must be an absolute path, got %q", name, value)
	}
	return cleaned, nil
}

func validateRange(name string, value, min, max int) error {
	if value < min || value > max {
		return fmt.Errorf("%s must be between %d and %d, got %d", name, min, max, value)
	}

	return nil
}
