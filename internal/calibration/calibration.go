package calibration

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// CalibrationResult holds the output of a calibration run.
type CalibrationResult struct {
	BaselinePowerUW int64              `json:"baseline_power_uw"`
	Samples         []BrightnessSample `json:"samples"`
	CPUFrequencyKHz int64              `json:"cpu_frequency_khz"`
	CalibratedAt    string             `json:"calibrated_at"`
}

// BrightnessSample holds power at a given brightness level.
type BrightnessSample struct {
	BrightnessPct int   `json:"brightness_pct"`
	AvgPowerUW    int64 `json:"avg_power_uw"`
}

// DefaultPath returns the default calibration config path for the given home directory.
func DefaultPath(home string) string {
	return filepath.Join(home, ".config", "power-monitor", "calibration.json")
}

// Load reads a CalibrationResult from the given path.
func Load(path string) (*CalibrationResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result CalibrationResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Save writes a CalibrationResult to the given path, creating directories as needed.
func Save(path string, result *CalibrationResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
