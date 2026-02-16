package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Storage.DBPath != "/var/lib/power-monitor/data.db" {
		t.Fatalf("unexpected DBPath: %q", cfg.Storage.DBPath)
	}
	if cfg.Storage.StateLogPath != "/var/lib/power-monitor/state-log.jsonl" {
		t.Fatalf("unexpected StateLogPath: %q", cfg.Storage.StateLogPath)
	}
	if cfg.Collection.IntervalSeconds != 5 {
		t.Fatalf("unexpected IntervalSeconds: %d", cfg.Collection.IntervalSeconds)
	}
	if cfg.Collection.TopProcesses != 10 {
		t.Fatalf("unexpected TopProcesses: %d", cfg.Collection.TopProcesses)
	}
	if cfg.Collection.WallClockJumpThresholdSeconds != 15 {
		t.Fatalf("unexpected WallClockJumpThresholdSeconds: %d", cfg.Collection.WallClockJumpThresholdSeconds)
	}
	if cfg.Cleanup.RetentionDays != 30 {
		t.Fatalf("unexpected RetentionDays: %d", cfg.Cleanup.RetentionDays)
	}
	if cfg.Cleanup.IntervalHours != 24 {
		t.Fatalf("unexpected IntervalHours: %d", cfg.Cleanup.IntervalHours)
	}
}

func TestLoad_OverridesAndKeepsDefaults(t *testing.T) {
	path := writeTempConfig(t, `
[storage]
db_path = "/tmp/test.db"

[collection]
interval_seconds = 8
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Storage.DBPath != "/tmp/test.db" {
		t.Fatalf("DBPath = %q, want /tmp/test.db", cfg.Storage.DBPath)
	}
	if cfg.Storage.StateLogPath != "/var/lib/power-monitor/state-log.jsonl" {
		t.Fatalf("StateLogPath = %q, want default", cfg.Storage.StateLogPath)
	}
	if cfg.Collection.IntervalSeconds != 8 {
		t.Fatalf("IntervalSeconds = %d, want 8", cfg.Collection.IntervalSeconds)
	}
	if cfg.Collection.TopProcesses != 10 {
		t.Fatalf("TopProcesses = %d, want default 10", cfg.Collection.TopProcesses)
	}
	if cfg.Collection.WallClockJumpThresholdSeconds != 15 {
		t.Fatalf("WallClockJumpThresholdSeconds = %d, want default 15", cfg.Collection.WallClockJumpThresholdSeconds)
	}
	if cfg.Cleanup.RetentionDays != 30 {
		t.Fatalf("RetentionDays = %d, want default 30", cfg.Cleanup.RetentionDays)
	}
	if cfg.Cleanup.IntervalHours != 24 {
		t.Fatalf("IntervalHours = %d, want default 24", cfg.Cleanup.IntervalHours)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	if err == nil {
		t.Fatal("Load() error = nil, want missing file error")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("Load() error = %v, want not-exist error", err)
	}
}

func TestLoad_InvalidTOML(t *testing.T) {
	path := writeTempConfig(t, "not = [valid")
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want TOML parse error")
	}
}

func TestLoad_ValidationErrors(t *testing.T) {
	tests := []struct {
		name       string
		contents   string
		wantErrSub string
	}{
		{
			name: "interval_seconds must be positive",
			contents: `
[collection]
interval_seconds = 0
`,
			wantErrSub: "collection.interval_seconds must be positive",
		},
		{
			name: "top_processes must be positive",
			contents: `
[collection]
top_processes = 0
`,
			wantErrSub: "collection.top_processes must be positive",
		},
		{
			name: "wall_clock_jump_threshold_seconds must be positive",
			contents: `
[collection]
wall_clock_jump_threshold_seconds = 0
`,
			wantErrSub: "collection.wall_clock_jump_threshold_seconds must be positive",
		},
		{
			name: "retention_days must be positive",
			contents: `
[cleanup]
retention_days = 0
`,
			wantErrSub: "cleanup.retention_days must be positive",
		},
		{
			name: "interval_hours must be positive",
			contents: `
[cleanup]
interval_hours = 0
`,
			wantErrSub: "cleanup.interval_hours must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempConfig(t, tt.contents)

			_, err := Load(path)
			if err == nil {
				t.Fatalf("Load() error = nil, want error containing %q", tt.wantErrSub)
			}
			if !strings.Contains(err.Error(), tt.wantErrSub) {
				t.Fatalf("Load() error = %q, want contains %q", err.Error(), tt.wantErrSub)
			}
		})
	}
}
