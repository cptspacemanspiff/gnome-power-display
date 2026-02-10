package storage

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"

	"github.com/cptspacemanspiff/gnome-power-display/internal/collector"
)

const schema = `
CREATE TABLE IF NOT EXISTS battery_samples (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp INTEGER NOT NULL,
	voltage_uv INTEGER NOT NULL,
	current_ua INTEGER NOT NULL,
	power_uw INTEGER NOT NULL,
	capacity_pct INTEGER NOT NULL,
	status TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_battery_ts ON battery_samples(timestamp);

CREATE TABLE IF NOT EXISTS backlight_samples (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp INTEGER NOT NULL,
	brightness INTEGER NOT NULL,
	max_brightness INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_backlight_ts ON backlight_samples(timestamp);

CREATE TABLE IF NOT EXISTS sleep_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	sleep_time INTEGER NOT NULL,
	wake_time INTEGER NOT NULL,
	type TEXT NOT NULL DEFAULT 'unknown'
);
CREATE INDEX IF NOT EXISTS idx_sleep_ts ON sleep_events(sleep_time);

`

// DB wraps a SQLite database for power monitor data.
type DB struct {
	db *sql.DB
}

// Open opens or creates the SQLite database at the given path.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &DB{db: db}, nil
}

// Close closes the database.
func (d *DB) Close() error {
	return d.db.Close()
}

// InsertBatterySample inserts a battery sample.
func (d *DB) InsertBatterySample(s collector.BatterySample) error {
	_, err := d.db.Exec(
		"INSERT INTO battery_samples (timestamp, voltage_uv, current_ua, power_uw, capacity_pct, status) VALUES (?, ?, ?, ?, ?, ?)",
		s.Timestamp, s.VoltageUV, s.CurrentUA, s.PowerUW, s.CapacityPct, s.Status,
	)
	return err
}

// InsertBacklightSample inserts a backlight sample.
func (d *DB) InsertBacklightSample(s collector.BacklightSample) error {
	_, err := d.db.Exec(
		"INSERT INTO backlight_samples (timestamp, brightness, max_brightness) VALUES (?, ?, ?)",
		s.Timestamp, s.Brightness, s.MaxBrightness,
	)
	return err
}

// InsertSleepEvent inserts a sleep event.
func (d *DB) InsertSleepEvent(s collector.SleepEvent) error {
	_, err := d.db.Exec(
		"INSERT INTO sleep_events (sleep_time, wake_time, type) VALUES (?, ?, ?)",
		s.SleepTime, s.WakeTime, s.Type,
	)
	return err
}

// LatestBatterySample returns the most recent battery sample.
func (d *DB) LatestBatterySample() (*collector.BatterySample, error) {
	row := d.db.QueryRow("SELECT timestamp, voltage_uv, current_ua, power_uw, capacity_pct, status FROM battery_samples ORDER BY timestamp DESC LIMIT 1")
	var s collector.BatterySample
	err := row.Scan(&s.Timestamp, &s.VoltageUV, &s.CurrentUA, &s.PowerUW, &s.CapacityPct, &s.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// LatestBacklightSample returns the most recent backlight sample.
func (d *DB) LatestBacklightSample() (*collector.BacklightSample, error) {
	row := d.db.QueryRow("SELECT timestamp, brightness, max_brightness FROM backlight_samples ORDER BY timestamp DESC LIMIT 1")
	var s collector.BacklightSample
	err := row.Scan(&s.Timestamp, &s.Brightness, &s.MaxBrightness)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// BatterySamplesInRange returns battery samples within the given time range.
func (d *DB) BatterySamplesInRange(from, to int64) ([]collector.BatterySample, error) {
	rows, err := d.db.Query(
		"SELECT timestamp, voltage_uv, current_ua, power_uw, capacity_pct, status FROM battery_samples WHERE timestamp >= ? AND timestamp <= ? ORDER BY timestamp",
		from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var samples []collector.BatterySample
	for rows.Next() {
		var s collector.BatterySample
		if err := rows.Scan(&s.Timestamp, &s.VoltageUV, &s.CurrentUA, &s.PowerUW, &s.CapacityPct, &s.Status); err != nil {
			return nil, err
		}
		samples = append(samples, s)
	}
	return samples, rows.Err()
}

// BacklightSamplesInRange returns backlight samples within the given time range.
func (d *DB) BacklightSamplesInRange(from, to int64) ([]collector.BacklightSample, error) {
	rows, err := d.db.Query(
		"SELECT timestamp, brightness, max_brightness FROM backlight_samples WHERE timestamp >= ? AND timestamp <= ? ORDER BY timestamp",
		from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var samples []collector.BacklightSample
	for rows.Next() {
		var s collector.BacklightSample
		if err := rows.Scan(&s.Timestamp, &s.Brightness, &s.MaxBrightness); err != nil {
			return nil, err
		}
		samples = append(samples, s)
	}
	return samples, rows.Err()
}

// SleepEventsInRange returns sleep events within the given time range.
func (d *DB) SleepEventsInRange(from, to int64) ([]collector.SleepEvent, error) {
	rows, err := d.db.Query(
		"SELECT sleep_time, wake_time, type FROM sleep_events WHERE sleep_time >= ? AND sleep_time <= ? ORDER BY sleep_time",
		from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []collector.SleepEvent
	for rows.Next() {
		var e collector.SleepEvent
		if err := rows.Scan(&e.SleepTime, &e.WakeTime, &e.Type); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
