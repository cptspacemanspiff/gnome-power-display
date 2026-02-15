package storage

import "fmt"

// DeleteOlderThan deletes rows from all tables where the timestamp is before
// the given unix epoch. Returns the total number of deleted rows.
func (d *DB) DeleteOlderThan(before int64) (int64, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}

	var total int64
	tables := []struct {
		name   string
		column string
	}{
		{"battery_samples", "timestamp"},
		{"backlight_samples", "timestamp"},
		{"sleep_events", "sleep_time"},
		{"power_state_events", "start_time"},
		{"process_samples", "timestamp"},
		{"cpu_freq_samples", "timestamp"},
	}

	// Note: table/column names are from a hardcoded slice, not user input.
	// fmt.Sprintf is used here because SQL placeholders (?) only work for values, not identifiers.
	// This is safe because 'tables' is a compile-time constant slice.
	for _, t := range tables {
		res, err := tx.Exec(
			fmt.Sprintf("DELETE FROM %s WHERE %s < ?", t.name, t.column),
			before,
		)
		if err != nil {
			tx.Rollback()
			return 0, fmt.Errorf("delete from %s: %w", t.name, err)
		}
		n, _ := res.RowsAffected()
		total += n
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return total, nil
}
